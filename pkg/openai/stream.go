package openai

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/wweir/warden/pkg/sse"
)

// ChatStreamParser parses SSE events from OpenAI Chat Completions streaming responses.
type ChatStreamParser struct{}

func (p *ChatStreamParser) Parse(events []sse.Event, injectedTools []string) ([]sse.ToolCallInfo, bool, error) {
	type deltaToolCall struct {
		Index    int    `json:"index"`
		ID       string `json:"id,omitempty"`
		Type     string `json:"type,omitempty"`
		Function struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		} `json:"function"`
	}
	type chunk struct {
		Choices []struct {
			Delta struct {
				ToolCalls []deltaToolCall `json:"tool_calls,omitempty"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason,omitempty"`
		} `json:"choices"`
	}

	toolCallMap := make(map[int]*ToolCall)
	var finishReason string

	for _, evt := range events {
		if evt.Data == "" || evt.Data == "[DONE]" {
			continue
		}

		var c chunk
		if err := json.Unmarshal([]byte(evt.Data), &c); err != nil {
			continue
		}
		if len(c.Choices) == 0 {
			continue
		}

		if c.Choices[0].FinishReason != "" {
			finishReason = c.Choices[0].FinishReason
		}

		for _, dtc := range c.Choices[0].Delta.ToolCalls {
			existing, ok := toolCallMap[dtc.Index]
			if !ok {
				toolCallMap[dtc.Index] = &ToolCall{
					ID:   dtc.ID,
					Type: dtc.Type,
					Function: FunctionCall{
						Name:      dtc.Function.Name,
						Arguments: dtc.Function.Arguments,
					},
				}
			} else {
				existing.Function.Arguments += dtc.Function.Arguments
			}
		}
	}

	if finishReason != "tool_calls" || len(toolCallMap) == 0 {
		return nil, false, nil
	}

	var infos []sse.ToolCallInfo
	hasInjected := false
	for i := range len(toolCallMap) {
		tc, ok := toolCallMap[i]
		if !ok {
			continue
		}
		infos = append(infos, sse.ToolCallInfo{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
		if slices.Contains(injectedTools, tc.Function.Name) {
			hasInjected = true
		}
	}

	return infos, hasInjected, nil
}

func (p *ChatStreamParser) Filter(events []sse.Event, _ []string) []sse.Event {
	// For OpenAI Chat Completions, the final round has no injected tool calls,
	// so no filtering is needed — the buffered events can be replayed as-is.
	return events
}

// ResponsesStreamParser parses SSE events from OpenAI Responses API streaming responses.
type ResponsesStreamParser struct{}

func (p *ResponsesStreamParser) Parse(events []sse.Event, injectedTools []string) ([]sse.ToolCallInfo, bool, error) {
	// find the response.completed event and extract function_call items
	for _, evt := range events {
		if evt.EventType != "response.completed" {
			continue
		}

		var wrapper struct {
			Response ResponsesResponse `json:"response"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &wrapper); err != nil {
			return nil, false, err
		}

		var infos []sse.ToolCallInfo
		hasInjected := false
		for _, raw := range wrapper.Response.Output {
			var typeCheck struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(raw, &typeCheck); err != nil || typeCheck.Type != "function_call" {
				continue
			}

			var fc FunctionCallItem
			if err := json.Unmarshal(raw, &fc); err != nil {
				continue
			}

			infos = append(infos, sse.ToolCallInfo{
				ID:        fc.CallID,
				Name:      fc.Name,
				Arguments: fc.Arguments,
			})
			if slices.Contains(injectedTools, fc.Name) {
				hasInjected = true
			}
		}

		return infos, hasInjected, nil
	}

	return nil, false, nil
}

func (p *ResponsesStreamParser) Filter(events []sse.Event, injectedTools []string) []sse.Event {
	var filtered []sse.Event
	for _, evt := range events {
		switch evt.EventType {
		case "response.output_item.added", "response.output_item.done":
			var item struct {
				Item struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"item"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &item); err == nil {
				if item.Item.Type == "function_call" && slices.Contains(injectedTools, item.Item.Name) {
					continue
				}
			}
			filtered = append(filtered, evt)

		case "response.function_call_arguments.delta", "response.function_call_arguments.done":
			filtered = append(filtered, evt)

		case "response.completed":
			var wrapper struct {
				Response json.RawMessage `json:"response"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &wrapper); err != nil {
				filtered = append(filtered, evt)
				continue
			}

			var resp ResponsesResponse
			if err := json.Unmarshal(wrapper.Response, &resp); err != nil {
				filtered = append(filtered, evt)
				continue
			}

			resp.Output = FilterResponsesOutput(resp.Output, injectedTools)

			respBytes, err := json.Marshal(resp)
			if err != nil {
				filtered = append(filtered, evt)
				continue
			}

			newData, err := json.Marshal(map[string]json.RawMessage{"response": respBytes})
			if err != nil {
				filtered = append(filtered, evt)
				continue
			}

			filtered = append(filtered, sse.Event{
				EventType: evt.EventType,
				Data:      string(newData),
				Raw:       "event: response.completed\ndata: " + string(newData) + "\n",
			})

		default:
			filtered = append(filtered, evt)
		}
	}
	return filtered
}

// FilterResponsesOutput removes injected function_call items from the final output.
func FilterResponsesOutput(output []json.RawMessage, injectedTools []string) []json.RawMessage {
	var filtered []json.RawMessage
	for _, raw := range output {
		var typeCheck struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &typeCheck); err != nil {
			filtered = append(filtered, raw)
			continue
		}

		if typeCheck.Type != "function_call" || !slices.Contains(injectedTools, typeCheck.Name) {
			filtered = append(filtered, raw)
		}
	}
	return filtered
}

// AssembleChatStream merges streaming SSE chunks into a single JSON object
// for logging. It uses map[string]any to avoid coupling to any specific API
// schema, maximizing compatibility with non-standard LLM providers.
//
// Strategy: take the last chunk as the base object (it typically carries
// top-level fields like id, model, usage, etc.), then merge accumulated
// delta content and tool_calls into choices[0].message.
func AssembleChatStream(rawSSE []byte) ([]byte, error) {
	events := sse.ParseEvents(rawSSE)

	var (
		base         map[string]any                 // last parsed chunk as base
		contentParts []string                       // accumulated delta.content
		toolCalls    = make(map[int]map[string]any) // index -> merged tool_call
		role         string
		finishReason string
	)

	for _, evt := range events {
		if evt.Data == "" || evt.Data == "[DONE]" {
			continue
		}

		var chunk map[string]any
		if err := json.Unmarshal([]byte(evt.Data), &chunk); err != nil {
			continue
		}
		base = chunk

		choices, _ := asArray(chunk["choices"])
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]any)
		if choice == nil {
			continue
		}

		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			finishReason = fr
		}

		delta, _ := choice["delta"].(map[string]any)
		if delta == nil {
			continue
		}

		if r, ok := delta["role"].(string); ok && r != "" {
			role = r
		}
		if c, ok := delta["content"].(string); ok && c != "" {
			contentParts = append(contentParts, c)
		}

		// merge streamed tool_calls by index
		deltaTCs, _ := asArray(delta["tool_calls"])
		for _, raw := range deltaTCs {
			dtc, _ := raw.(map[string]any)
			if dtc == nil {
				continue
			}
			idx := int(toFloat64(dtc["index"]))
			existing, ok := toolCalls[idx]
			if !ok {
				// first chunk for this index: clone the object
				existing = make(map[string]any)
				for k, v := range dtc {
					if k != "index" {
						existing[k] = v
					}
				}
				toolCalls[idx] = existing
				continue
			}
			// subsequent chunks: append function.arguments
			fn, _ := dtc["function"].(map[string]any)
			if fn == nil {
				continue
			}
			existFn, _ := existing["function"].(map[string]any)
			if existFn == nil {
				continue
			}
			if args, ok := fn["arguments"].(string); ok {
				prev, _ := existFn["arguments"].(string)
				existFn["arguments"] = prev + args
			}
		}
	}

	if base == nil {
		return rawSSE, nil
	}

	// build the assembled message
	msg := make(map[string]any)
	if role != "" {
		msg["role"] = role
	}
	if len(contentParts) > 0 {
		msg["content"] = strings.Join(contentParts, "")
	}
	if len(toolCalls) > 0 {
		sorted := make([]any, 0, len(toolCalls))
		for i := range len(toolCalls) {
			if tc, ok := toolCalls[i]; ok {
				sorted = append(sorted, tc)
			}
		}
		msg["tool_calls"] = sorted
	}

	// build the single choice with "message" instead of "delta"
	choice := map[string]any{
		"index":   0,
		"message": msg,
	}
	if finishReason != "" {
		choice["finish_reason"] = finishReason
	}

	// reuse base for top-level fields, replace choices
	base["choices"] = []any{choice}
	// remove streaming-specific object type
	if obj, ok := base["object"].(string); ok && obj == "chat.completion.chunk" {
		base["object"] = "chat.completion"
	}

	return json.Marshal(base)
}

func asArray(v any) ([]any, bool) {
	arr, ok := v.([]any)
	return arr, ok
}

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}

// ExtractCompletedResponse finds the response.completed event and returns the response.
func ExtractCompletedResponse(events []sse.Event) *ResponsesResponse {
	for _, evt := range events {
		if evt.EventType != "response.completed" {
			continue
		}
		var wrapper struct {
			Response ResponsesResponse `json:"response"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &wrapper); err != nil {
			return nil
		}
		return &wrapper.Response
	}
	return nil
}
