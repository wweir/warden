package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wweir/warden/pkg/protocol"
)

// ChatStreamParser parses SSE events from OpenAI Chat Completions streaming responses.
type ChatStreamParser struct{}

func (p *ChatStreamParser) Parse(events []protocol.Event) ([]protocol.ToolCallInfo, error) {
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
		return nil, nil
	}

	var infos []protocol.ToolCallInfo
	for i := range len(toolCallMap) {
		tc, ok := toolCallMap[i]
		if !ok {
			continue
		}
		infos = append(infos, protocol.ToolCallInfo{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	return infos, nil
}

// ResponsesStreamParser parses SSE events from OpenAI Responses API streaming responses.
type ResponsesStreamParser struct{}

func (p *ResponsesStreamParser) Parse(events []protocol.Event) ([]protocol.ToolCallInfo, error) {
	// find the response.completed event and extract function_call items
	for _, evt := range events {
		if responsesEventType(evt) != "response.completed" {
			continue
		}

		var wrapper struct {
			Response ResponsesResponse `json:"response"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &wrapper); err != nil {
			return nil, err
		}

		var infos []protocol.ToolCallInfo
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

			infos = append(infos, protocol.ToolCallInfo{
				ID:        fc.CallID,
				Name:      fc.Name,
				Arguments: fc.Arguments,
			})
		}

		return infos, nil
	}

	return nil, nil
}

// AssembleResponsesStream extracts the completed Responses API object from an SSE stream
// so streaming logs keep the same shape as non-streaming responses.
func AssembleResponsesStream(rawSSE []byte) ([]byte, error) {
	events := protocol.ParseEvents(rawSSE)
	resp := ExtractCompletedResponse(events)
	if resp == nil {
		return nil, fmt.Errorf("response.completed event not found")
	}
	return json.Marshal(resp)
}

// AssembleChatStream merges streaming SSE chunks into a single JSON object
// for logging. It uses map[string]any to avoid coupling to any specific API
// schema, maximizing compatibility with non-standard LLM providers.
//
// Strategy: take the last chunk as the base object (it typically carries
// top-level fields like id, model, usage, etc.), then merge accumulated
// delta content and tool_calls into choices[0].message.
func AssembleChatStream(rawSSE []byte) ([]byte, error) {
	events := protocol.ParseEvents(rawSSE)

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
func ExtractCompletedResponse(events []protocol.Event) *ResponsesResponse {
	for _, evt := range events {
		if responsesEventType(evt) != "response.completed" {
			continue
		}
		if resp := completedResponseFromData(evt.Data); resp != nil {
			return resp
		}
	}
	return nil
}

func responsesEventType(evt protocol.Event) string {
	if evt.EventType != "" {
		return evt.EventType
	}

	var typeCheck struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(evt.Data), &typeCheck); err != nil {
		return ""
	}
	return typeCheck.Type
}

func completedResponseFromData(data string) *ResponsesResponse {
	var wrapper struct {
		Response *ResponsesResponse `json:"response"`
	}
	if err := json.Unmarshal([]byte(data), &wrapper); err == nil && wrapper.Response != nil {
		return wrapper.Response
	}

	var resp ResponsesResponse
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		return nil
	}
	if resp.ID == "" && resp.Status == "" && resp.Output == nil && len(resp.Extra) == 0 {
		return nil
	}
	return &resp
}
