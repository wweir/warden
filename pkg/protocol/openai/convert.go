package openai

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/wweir/warden/pkg/protocol"
)

// Mock tool names for Responses API features that cannot be mapped to Chat Completions.
const (
	MockToolPrefix       = "__responses_tool_"
	MockToolCallIDPrefix = "__responses_"
)

// ResponsesRequestToChatRequest converts a Responses API request to a Chat Completions request.
// Only the Chat-compatible subset is supported: string/array input and function tools.
func ResponsesRequestToChatRequest(respReq ResponsesRequest) (ChatCompletionRequest, error) {
	chatReq := ChatCompletionRequest{
		Model:  respReq.Model,
		Stream: respReq.Stream,
		Extra:  make(map[string]json.RawMessage),
	}

	if len(respReq.Extra) > 0 {
		maps.Copy(chatReq.Extra, respReq.Extra)
	}
	if _, ok := chatReq.Extra["previous_response_id"]; ok {
		return chatReq, fmt.Errorf("previous_response_id is not supported in responses_to_chat mode")
	}

	messages, err := convertResponsesInputToMessages(respReq.Input)
	if err != nil {
		return chatReq, fmt.Errorf("convert input: %w", err)
	}
	chatReq.Messages = messages

	for _, rawTool := range respReq.Tools {
		tool, err := convertResponsesToolToChatTool(rawTool)
		if err != nil {
			// Convert unsupported tool types to mock function tools for passthrough
			// This preserves custom/web_search/file_search tools when converting to Chat Completions
			tool = createMockToolFromRaw(rawTool)
		}
		chatReq.Tools = append(chatReq.Tools, tool)
	}

	return chatReq, nil
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
	var pendingReasoning string // Reasoning content to attach to next assistant message

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
			// If this is an assistant message and we have pending reasoning, attach it
			if msg.Role == "assistant" && pendingReasoning != "" {
				msg.ReasoningContent = pendingReasoning
				pendingReasoning = ""
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
				msg := Message{Role: "assistant", ToolCalls: []ToolCall{tc}}
				if pendingReasoning != "" {
					msg.ReasoningContent = pendingReasoning
					pendingReasoning = ""
				}
				messages = append(messages, msg)
			}

		case "function_call_output":
			var out FunctionCallOutputItem
			if err := json.Unmarshal(raw, &out); err != nil {
				return nil, fmt.Errorf("unmarshal function_call_output: %w", err)
			}
			messages = append(messages, Message{Role: "tool", ToolCallID: out.CallID, Content: out.Output})

		case "reasoning":
			// Reasoning items contain extended thinking content
			// Store for next assistant message
			var reasoningItem struct {
				Summary string `json:"summary"`
			}
			if err := json.Unmarshal(raw, &reasoningItem); err != nil {
				// If parsing fails, use raw JSON as fallback
				reasoningItem.Summary = string(raw)
			}
			pendingReasoning = reasoningItem.Summary

		default:
			// Convert unknown input item types to mock tool call for passthrough
			mockCallID := generateMockCallID(itemType.Type)
			tc := ToolCall{
				ID:   mockCallID,
				Type: "function",
				Function: FunctionCall{
					Name:      MockToolPrefix + itemType.Type,
					Arguments: string(raw),
				},
			}
			if len(messages) > 0 && messages[len(messages)-1].Role == "assistant" && messages[len(messages)-1].ToolCallID == "" {
				messages[len(messages)-1].ToolCalls = append(messages[len(messages)-1].ToolCalls, tc)
			} else {
				msg := Message{Role: "assistant", ToolCalls: []ToolCall{tc}}
				if pendingReasoning != "" {
					msg.ReasoningContent = pendingReasoning
					pendingReasoning = ""
				}
				messages = append(messages, msg)
			}
		}
	}

	// If there's pending reasoning without a following assistant message, create one
	if pendingReasoning != "" {
		messages = append(messages, Message{Role: "assistant", ReasoningContent: pendingReasoning})
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
	if msg.Role == "developer" {
		msg.Role = "system"
	}
	if v, ok := fields["content"]; ok {
		content, err := convertResponsesContentToChatContent(v)
		if err != nil {
			return Message{}, err
		}
		msg.Content = content
		delete(fields, "content")
	}
	if v, ok := fields["reasoning_content"]; ok {
		if err := json.Unmarshal(v, &msg.ReasoningContent); err != nil {
			return Message{}, fmt.Errorf("unmarshal reasoning_content: %w", err)
		}
		delete(fields, "reasoning_content")
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
		return Tool{}, fmt.Errorf("unsupported responses tool type %q", typeCheck.Type)
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
		ID:    chatResp.ID,
		Extra: make(map[string]json.RawMessage),
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
	resp.Extra["status"], _ = json.Marshal("completed")

	if len(chatResp.Choices) == 0 {
		return resp, nil
	}

	msg := chatResp.Choices[0].Message
	if msg.Content != nil {
		item := map[string]any{
			"type":    "message",
			"role":    "assistant",
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

func mustMarshalRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// ChatSSEToResponsesSSE converts Chat Completions SSE chunks to Responses API SSE events.
// If the stream is incomplete (missing [DONE] marker), the response.completed event will
// have status "incomplete" with an error message.
func ChatSSEToResponsesSSE(rawSSE []byte, model string) []byte {
	events := protocol.ParseEvents(rawSSE)
	var responseChunks []string
	messageAdded := false
	toolAdded := make(map[int]bool)
	streamComplete := false

	for _, evt := range events {
		if evt.Data == "[DONE]" {
			streamComplete = true
			continue
		}
		if evt.Data == "" {
			continue
		}

		var chunk map[string]any
		if err := json.Unmarshal([]byte(evt.Data), &chunk); err != nil {
			continue
		}

		choices, _ := asArray(chunk["choices"])
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]any)
		if choice == nil {
			continue
		}
		delta, _ := choice["delta"].(map[string]any)
		if delta == nil {
			continue
		}

		if content, ok := delta["content"].(string); ok && content != "" {
			if !messageAdded {
				responseChunks = append(responseChunks, formatResponsesEvent("response.output_item.added", map[string]any{
					"item": map[string]any{"type": "message", "role": "assistant", "content": []any{}},
				}))
				messageAdded = true
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

			if !toolAdded[idx] {
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
				toolAdded[idx] = true
			}

			if fn != nil {
				if args, ok := fn["arguments"].(string); ok && args != "" {
					responseChunks = append(responseChunks, formatResponsesEvent("response.function_call_arguments.delta", map[string]any{"delta": args}))
				}
			}
		}
	}

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

	// Set status based on stream completeness
	if !streamComplete {
		completed.Status = "incomplete"
		if completed.Extra == nil {
			completed.Extra = make(map[string]json.RawMessage)
		}
		completed.Extra["error"] = mustMarshalRaw(map[string]any{
			"type":    "stream_error",
			"message": "stream disconnected before completion",
		})
	} else {
		completed.Status = "completed"
	}
	responseChunks = append(responseChunks, formatResponsesEvent("response.completed", map[string]any{"response": completed}))

	return []byte(strings.Join(responseChunks, ""))
}

func formatResponsesEvent(eventType string, payload any) string {
	data, _ := json.Marshal(payload)
	return "event: " + eventType + "\ndata: " + string(data) + "\n\n"
}

// generateMockCallID creates a unique ID for mock tool calls.
func generateMockCallID(toolType string) string {
	return MockToolCallIDPrefix + toolType + "_" + fmt.Sprintf("%d", len(toolType))
}

// createMockToolFromRaw creates a mock function tool from an unsupported Responses tool type.
// The original tool data is preserved in the Parameters field.
func createMockToolFromRaw(raw json.RawMessage) Tool {
	var typeCheck struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	json.Unmarshal(raw, &typeCheck)

	return Tool{
		Type: "function",
		Function: Function{
			Name:        MockToolPrefix + typeCheck.Type + "_" + typeCheck.Name,
			Description: "Mock tool for Responses API " + typeCheck.Type + " tool (passthrough)",
			Parameters:  raw,
		},
	}
}
