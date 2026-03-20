package openai

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/wweir/warden/pkg/protocol"
)

var responsesToChatAllowedExtraFields = map[string]struct{}{
	"audio":                 {},
	"frequency_penalty":     {},
	"logit_bias":            {},
	"logprobs":              {},
	"max_completion_tokens": {},
	"max_tokens":            {},
	"metadata":              {},
	"modalities":            {},
	"n":                     {},
	"parallel_tool_calls":   {},
	"presence_penalty":      {},
	"response_format":       {},
	"seed":                  {},
	"service_tier":          {},
	"stop":                  {},
	"store":                 {},
	"stream_options":        {},
	"temperature":           {},
	"tool_choice":           {},
	"top_logprobs":          {},
	"top_p":                 {},
	"user":                  {},
}

// ResponsesRequestToChatRequest converts a Responses API request to a Chat Completions request.
// Only the Chat-compatible subset is supported: string/array input and function tools.
func ResponsesRequestToChatRequest(respReq ResponsesRequest) (ChatCompletionRequest, error) {
	chatReq := ChatCompletionRequest{
		Model:  respReq.Model,
		Stream: respReq.Stream,
		Extra:  make(map[string]json.RawMessage),
	}

	messages, err := convertResponsesInputToMessages(respReq.Input)
	if err != nil {
		return chatReq, fmt.Errorf("convert input: %w", err)
	}

	messages, extra, err := convertResponsesRequestExtras(messages, respReq.Extra)
	if err != nil {
		return chatReq, err
	}
	chatReq.Messages = messages
	if len(extra) > 0 {
		maps.Copy(chatReq.Extra, extra)
	}

	for _, rawTool := range respReq.Tools {
		tool, err := convertResponsesToolToChatTool(rawTool)
		if err != nil {
			return chatReq, fmt.Errorf("convert tool: %w", err)
		}
		chatReq.Tools = append(chatReq.Tools, tool)
	}

	return chatReq, nil
}

func convertResponsesRequestExtras(messages []Message, extra map[string]json.RawMessage) ([]Message, map[string]json.RawMessage, error) {
	if len(extra) == 0 {
		return messages, nil, nil
	}

	chatExtra := make(map[string]json.RawMessage, len(extra))
	for key, raw := range extra {
		switch key {
		case "instructions":
			var instructions string
			if err := json.Unmarshal(raw, &instructions); err != nil {
				return nil, nil, fmt.Errorf("convert instructions: %w", err)
			}
			if strings.TrimSpace(instructions) == "" {
				continue
			}
			messages = append([]Message{{
				Role:    "developer",
				Content: instructions,
			}}, messages...)
		case "previous_response_id":
			return nil, nil, fmt.Errorf("previous_response_id is not supported in responses_to_chat mode")
		case "n":
			n, err := decodePositiveInt(raw)
			if err != nil {
				return nil, nil, fmt.Errorf("convert n: %w", err)
			}
			if n > 1 {
				return nil, nil, fmt.Errorf("n > 1 is not supported in responses_to_chat mode")
			}
			chatExtra[key] = raw
		default:
			if _, ok := responsesToChatAllowedExtraFields[key]; !ok {
				return nil, nil, fmt.Errorf("responses field %q is not supported in responses_to_chat mode", key)
			}
			chatExtra[key] = raw
		}
	}

	return messages, chatExtra, nil
}

func decodePositiveInt(raw json.RawMessage) (int64, error) {
	var i int64
	if err := json.Unmarshal(raw, &i); err == nil {
		return i, nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		i, convErr := strconv.ParseInt(s, 10, 64)
		if convErr != nil {
			return 0, convErr
		}
		return i, nil
	}

	return 0, fmt.Errorf("expected integer")
}

func convertResponsesInputToMessages(input json.RawMessage) ([]Message, error) {
	if len(input) == 0 {
		return nil, nil
	}

	if input[0] == '"' {
		var s string
		if err := json.Unmarshal(input, &s); err != nil {
			return nil, fmt.Errorf("unmarshal string input: %w", err)
		}
		return []Message{{Role: "user", Content: s}}, nil
	}

	if input[0] != '[' {
		return nil, fmt.Errorf("unsupported input shape")
	}

	var items []json.RawMessage
	if err := json.Unmarshal(input, &items); err != nil {
		return nil, fmt.Errorf("unmarshal input items: %w", err)
	}

	messages := make([]Message, 0, len(items))
	for _, raw := range items {
		var itemType struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &itemType); err != nil {
			return nil, fmt.Errorf("unmarshal input item type: %w", err)
		}

		switch itemType.Type {
		case "", "message":
			msg, err := convertResponsesMessageItem(raw)
			if err != nil {
				return nil, err
			}
			messages = append(messages, msg)

		case "function_call":
			var fc FunctionCallItem
			if err := json.Unmarshal(raw, &fc); err != nil {
				return nil, fmt.Errorf("unmarshal function_call: %w", err)
			}
			tc := ToolCall{
				ID:   fc.CallID,
				Type: "function",
				Function: FunctionCall{
					Name:      fc.Name,
					Arguments: fc.Arguments,
				},
			}
			if len(messages) > 0 && messages[len(messages)-1].Role == "assistant" && messages[len(messages)-1].ToolCallID == "" {
				messages[len(messages)-1].ToolCalls = append(messages[len(messages)-1].ToolCalls, tc)
			} else {
				messages = append(messages, Message{Role: "assistant", ToolCalls: []ToolCall{tc}})
			}

		case "function_call_output":
			var out FunctionCallOutputItem
			if err := json.Unmarshal(raw, &out); err != nil {
				return nil, fmt.Errorf("unmarshal function_call_output: %w", err)
			}
			messages = append(messages, Message{Role: "tool", ToolCallID: out.CallID, Content: out.Output})

		case "reasoning":
			return nil, fmt.Errorf("unsupported input item type %q", itemType.Type)

		default:
			return nil, fmt.Errorf("unsupported input item type %q", itemType.Type)
		}
	}

	return messages, nil
}

func convertResponsesMessageItem(raw json.RawMessage) (Message, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Message{}, fmt.Errorf("unmarshal message item: %w", err)
	}

	msg := Message{}
	if v, ok := fields["role"]; ok {
		if err := json.Unmarshal(v, &msg.Role); err != nil {
			return Message{}, fmt.Errorf("unmarshal message role: %w", err)
		}
		delete(fields, "role")
	}
	switch msg.Role {
	case "assistant", "developer", "system", "tool", "user":
	default:
		return Message{}, fmt.Errorf("unsupported message role %q", msg.Role)
	}
	if v, ok := fields["content"]; ok {
		content, err := convertResponsesContentToChatContent(v)
		if err != nil {
			return Message{}, err
		}
		msg.Content = content
		delete(fields, "content")
	}
	if _, ok := fields["reasoning_content"]; ok {
		return Message{}, fmt.Errorf("reasoning_content is not supported in responses_to_chat mode")
	}
	if v, ok := fields["name"]; ok {
		if err := json.Unmarshal(v, &msg.Name); err != nil {
			return Message{}, fmt.Errorf("unmarshal message name: %w", err)
		}
		delete(fields, "name")
	}
	delete(fields, "type")
	if len(fields) > 0 {
		msg.Extra = fields
	}

	return msg, nil
}

func convertResponsesContentToChatContent(raw json.RawMessage) (any, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}

	var arr []any
	if err := json.Unmarshal(raw, &arr); err == nil {
		return normalizeResponsesBlocksToChat(arr), nil
	}

	var v any
	if err := json.Unmarshal(raw, &v); err == nil {
		return v, nil
	}

	return nil, fmt.Errorf("unmarshal content")
}

func normalizeResponsesBlocksToChat(blocks []any) []any {
	normalized := make([]any, 0, len(blocks))
	for _, block := range blocks {
		bm, ok := block.(map[string]any)
		if !ok {
			normalized = append(normalized, block)
			continue
		}

		cloned := make(map[string]any, len(bm))
		for k, v := range bm {
			cloned[k] = v
		}
		if typ, ok := cloned["type"].(string); ok {
			switch typ {
			case "input_text", "output_text":
				cloned["type"] = "text"
			}
		}
		normalized = append(normalized, cloned)
	}
	return normalized
}

func convertResponsesToolToChatTool(raw json.RawMessage) (Tool, error) {
	var typeCheck struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &typeCheck); err != nil {
		return Tool{}, fmt.Errorf("unmarshal tool type: %w", err)
	}
	if typeCheck.Type != "function" {
		return Tool{}, fmt.Errorf("unsupported tool type %q", typeCheck.Type)
	}

	var flatTool ResponsesFunctionTool
	if err := json.Unmarshal(raw, &flatTool); err != nil {
		return Tool{}, fmt.Errorf("unmarshal function tool: %w", err)
	}

	var params any
	if len(flatTool.Parameters) > 0 {
		if err := json.Unmarshal(flatTool.Parameters, &params); err != nil {
			return Tool{}, fmt.Errorf("unmarshal tool parameters: %w", err)
		}
	}

	return Tool{
		Type: "function",
		Function: Function{
			Name:        flatTool.Name,
			Description: flatTool.Description,
			Parameters:  params,
		},
	}, nil
}

// ChatResponseToResponsesResponse converts a Chat Completions response to a Responses API response.
func ChatResponseToResponsesResponse(chatResp ChatCompletionResponse, model string) (ResponsesResponse, error) {
	resp := ResponsesResponse{
		ID:     chatResp.ID,
		Status: "completed",
		Extra:  make(map[string]json.RawMessage),
	}

	for k, v := range chatResp.Extra {
		if k != "choices" && k != "usage" && k != "id" {
			resp.Extra[k] = v
		}
	}
	resp.Extra["object"], _ = json.Marshal("response")
	if chatResp.Model != "" {
		resp.Extra["model"], _ = json.Marshal(chatResp.Model)
	} else if model != "" {
		resp.Extra["model"], _ = json.Marshal(model)
	}
	if chatResp.Created != 0 {
		resp.Extra["created"] = mustMarshalRaw(chatResp.Created)
	}
	if chatResp.Usage != (Usage{}) {
		resp.Extra["usage"] = mustMarshalRaw(chatResp.Usage)
	}

	if len(chatResp.Choices) == 0 {
		return resp, nil
	}

	msg := chatResp.Choices[0].Message
	if msg.Content != nil {
		item := map[string]any{
			"type":    "message",
			"role":    messageRoleOrDefault(msg.Role),
			"content": normalizeChatContentForResponses(msg.Content),
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return resp, fmt.Errorf("marshal message item: %w", err)
		}
		resp.Output = append(resp.Output, raw)
	}

	for _, tc := range msg.ToolCalls {
		fc := FunctionCallItem{
			Type:      "function_call",
			CallID:    tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
			Status:    "completed",
		}
		raw, err := json.Marshal(fc)
		if err != nil {
			return resp, fmt.Errorf("marshal function_call item: %w", err)
		}
		resp.Output = append(resp.Output, raw)
	}

	return resp, nil
}

func normalizeChatContentForResponses(content any) any {
	if text, ok := content.(string); ok {
		return []any{map[string]any{
			"type": "output_text",
			"text": text,
		}}
	}

	blocks, ok := content.([]any)
	if !ok {
		return content
	}

	normalized := make([]any, 0, len(blocks))
	for _, block := range blocks {
		bm, ok := block.(map[string]any)
		if !ok {
			normalized = append(normalized, block)
			continue
		}

		cloned := make(map[string]any, len(bm))
		for k, v := range bm {
			cloned[k] = v
		}
		if typ, ok := cloned["type"].(string); ok && typ == "text" {
			cloned["type"] = "output_text"
		}
		normalized = append(normalized, cloned)
	}

	return normalized
}

func messageRoleOrDefault(role string) string {
	if role == "" {
		return "assistant"
	}
	return role
}

func mustMarshalRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

type ChatResponsesStreamState struct {
	messageAdded bool
	toolAdded    map[int]bool
}

func NewChatResponsesStreamState() *ChatResponsesStreamState {
	return &ChatResponsesStreamState{
		toolAdded: make(map[int]bool),
	}
}

func (s *ChatResponsesStreamState) ConvertEvent(evt protocol.Event) []byte {
	if evt.Data == "" || evt.Data == "[DONE]" {
		return nil
	}

	var chunk map[string]any
	if err := json.Unmarshal([]byte(evt.Data), &chunk); err != nil {
		return nil
	}

	choices, _ := asArray(chunk["choices"])
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return nil
	}
	delta, _ := choice["delta"].(map[string]any)
	if delta == nil {
		return nil
	}

	var responseChunks []string
	if content, ok := delta["content"].(string); ok && content != "" {
		if !s.messageAdded {
			responseChunks = append(responseChunks, formatResponsesEvent("response.output_item.added", map[string]any{
				"item": map[string]any{"type": "message", "role": "assistant", "content": []any{}},
			}))
			s.messageAdded = true
		}
		responseChunks = append(responseChunks, formatResponsesEvent("response.output_text.delta", map[string]any{"delta": content}))
	}

	deltaToolCalls, _ := asArray(delta["tool_calls"])
	for _, rawToolCall := range deltaToolCalls {
		dtc, _ := rawToolCall.(map[string]any)
		if dtc == nil {
			continue
		}
		idx := int(toFloat64(dtc["index"]))
		fn, _ := dtc["function"].(map[string]any)

		if !s.toolAdded[idx] {
			item := map[string]any{"type": "function_call"}
			if id, ok := dtc["id"].(string); ok && id != "" {
				item["call_id"] = id
			}
			if fn != nil {
				if name, ok := fn["name"].(string); ok && name != "" {
					item["name"] = name
				}
			}
			responseChunks = append(responseChunks, formatResponsesEvent("response.output_item.added", map[string]any{"item": item}))
			s.toolAdded[idx] = true
		}

		if fn != nil {
			if args, ok := fn["arguments"].(string); ok && args != "" {
				responseChunks = append(responseChunks, formatResponsesEvent("response.function_call_arguments.delta", map[string]any{"delta": args}))
			}
		}
	}

	return []byte(strings.Join(responseChunks, ""))
}

func BuildChatResponsesCompletedEvent(rawSSE []byte, model string, streamComplete bool) []byte {
	completed := ResponsesResponse{}
	if assembled, err := AssembleChatStream(rawSSE); err == nil {
		var chatResp ChatCompletionResponse
		if err := json.Unmarshal(assembled, &chatResp); err == nil {
			if chatResp.Model == "" {
				chatResp.Model = model
			}
			if converted, err := ChatResponseToResponsesResponse(chatResp, model); err == nil {
				completed = converted
			}
		}
	}
	if len(completed.Extra) == 0 && model != "" {
		completed.Extra = map[string]json.RawMessage{"model": mustMarshalRaw(model), "object": mustMarshalRaw("response")}
	}

	if !streamComplete {
		completed.Status = "incomplete"
		if completed.Extra == nil {
			completed.Extra = make(map[string]json.RawMessage)
		}
		completed.Extra["error"] = mustMarshalRaw(map[string]any{
			"type":    "stream_error",
			"message": "stream disconnected before completion",
		})
	}

	return []byte(formatResponsesEvent("response.completed", map[string]any{"response": completed}))
}

// ChatSSEToResponsesSSE converts Chat Completions SSE chunks to Responses API SSE events.
// If the stream is incomplete (missing [DONE] marker), the response.completed event will
// have status "incomplete" with an error message.
func ChatSSEToResponsesSSE(rawSSE []byte, model string) []byte {
	events := protocol.ParseEvents(rawSSE)
	var responseChunks []string
	state := NewChatResponsesStreamState()
	streamComplete := false

	for _, evt := range events {
		if evt.Data == "[DONE]" {
			streamComplete = true
			continue
		}
		if chunk := state.ConvertEvent(evt); len(chunk) > 0 {
			responseChunks = append(responseChunks, string(chunk))
		}
	}

	responseChunks = append(responseChunks, string(BuildChatResponsesCompletedEvent(rawSSE, model, streamComplete)))

	return []byte(strings.Join(responseChunks, ""))
}

func formatResponsesEvent(eventType string, payload any) string {
	data, _ := json.Marshal(payload)
	return "event: " + eventType + "\ndata: " + string(data) + "\n\n"
}
