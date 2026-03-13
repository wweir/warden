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
	MockToolReasoning    = "__responses_reasoning__"
	MockToolPrefix       = "__responses_tool_"
	MockToolCallIDPrefix = "__responses_"
)

// ChatRequestToResponsesRequest converts a Chat Completions request to a Responses API request.
// Maps messages→input, tools→tools (flattened format), and preserves extra fields.
func ChatRequestToResponsesRequest(chatReq ChatCompletionRequest) (ResponsesRequest, error) {
	respReq := ResponsesRequest{
		Model:  chatReq.Model,
		Stream: chatReq.Stream,
		Extra:  make(map[string]json.RawMessage),
	}

	// Convert messages to input items
	inputItems := make([]json.RawMessage, 0, len(chatReq.Messages))
	for _, msg := range chatReq.Messages {
		items := convertMessageToInputItems(msg)
		inputItems = append(inputItems, items...)
	}
	inputBytes, err := json.Marshal(inputItems)
	if err != nil {
		return respReq, fmt.Errorf("marshal input items: %w", err)
	}
	respReq.Input = inputBytes

	// Convert tools to flat format
	for _, tool := range chatReq.Tools {
		// Check if this is a mock tool that needs to be converted back to original format
		if strings.HasPrefix(tool.Function.Name, MockToolPrefix) {
			// Extract original tool data from Parameters
			if params, ok := tool.Function.Parameters.(json.RawMessage); ok && len(params) > 0 {
				respReq.Tools = append(respReq.Tools, params)
				continue
			}
		}

		flatTool := ResponsesFunctionTool{
			Type:        tool.Type,
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
		}
		if tool.Function.Parameters != nil {
			params, _ := json.Marshal(tool.Function.Parameters)
			flatTool.Parameters = params
		}
		toolBytes, _ := json.Marshal(flatTool)
		respReq.Tools = append(respReq.Tools, toolBytes)
	}

	// Known fields that are handled explicitly - don't pass through
	knownFields := map[string]bool{
		"model":    true,
		"messages": true,
		"tools":    true,
		"stream":   true,
	}

	// Pass through all other fields from Extra
	for k, v := range chatReq.Extra {
		if !knownFields[k] {
			respReq.Extra[k] = v
		}
	}

	return respReq, nil
}

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

// convertMessageToInputItems converts a Chat message to Responses API input items.
// Handles role mapping: system→developer, user→user, assistant→assistant or function_call items, tool→function_call_output.
func convertMessageToInputItems(msg Message) []json.RawMessage {
	var items []json.RawMessage

	// Handle tool role: function_call_output
	if msg.Role == "tool" {
		output := FunctionCallOutputItem{
			Type:   "function_call_output",
			CallID: msg.ToolCallID,
			Output: stringifyContent(msg.Content),
		}
		bytes, _ := json.Marshal(output)
		return append(items, bytes)
	}

	// Handle assistant message
	if msg.Role == "assistant" {
		// Add reasoning item first if present
		if msg.ReasoningContent != "" {
			reasoningItem := map[string]any{
				"type":    "reasoning",
				"summary": msg.ReasoningContent,
			}
			bytes, _ := json.Marshal(reasoningItem)
			items = append(items, bytes)
		}

		// Add assistant message with content if present
		if msg.Content != nil {
			assistantItem := map[string]any{
				"type":    "message",
				"role":    "assistant",
				"content": msg.Content,
			}
			bytes, _ := json.Marshal(assistantItem)
			items = append(items, bytes)
		}

		// Add function_call items for each tool call
		for _, tc := range msg.ToolCalls {
			fcItem := map[string]any{
				"type":      "function_call",
				"call_id":   tc.ID,
				"name":      tc.Function.Name,
				"arguments": tc.Function.Arguments,
			}
			bytes, _ := json.Marshal(fcItem)
			items = append(items, bytes)
		}

		return items
	}

	// Standard message mapping
	item := map[string]any{
		"type": "message",
		"role": msg.Role,
	}
	if msg.Role == "system" {
		item["role"] = "developer" // Responses API uses "developer" for system prompts
	}
	if msg.Content != nil {
		item["content"] = msg.Content
	}
	// Preserve extra fields from message
	for k, v := range msg.Extra {
		item[k] = v
	}

	bytes, _ := json.Marshal(item)
	return append(items, bytes)
}

// stringifyContent converts message content to a string.
func stringifyContent(content any) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []any:
		// Content blocks: extract text
		var texts []string
		for _, block := range v {
			if bm, ok := block.(map[string]any); ok {
				if t, ok := bm["text"].(string); ok {
					texts = append(texts, t)
				}
			}
		}
		return strings.Join(texts, "\n")
	default:
		bytes, _ := json.Marshal(content)
		return string(bytes)
	}
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

// ResponsesResponseToChatResponse converts a Responses API response to a Chat Completions response.
func ResponsesResponseToChatResponse(resp ResponsesResponse, model string) (ChatCompletionResponse, error) {
	chatResp := ChatCompletionResponse{
		ID:     resp.ID,
		Model:  model,
		Object: "chat.completion",
		Extra:  make(map[string]json.RawMessage),
	}

	// Extract choice from output items
	choice := Choice{
		Index:   0,
		Message: Message{Role: "assistant"},
	}

	var toolCalls []ToolCall
	var contentParts []string

	for _, raw := range resp.Output {
		var typeCheck struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &typeCheck); err != nil {
			continue
		}

		switch typeCheck.Type {
		case "message":
			var msgItem struct {
				Content any `json:"content"`
			}
			if err := json.Unmarshal(raw, &msgItem); err == nil {
				if msgItem.Content != nil {
					switch c := msgItem.Content.(type) {
					case string:
						contentParts = append(contentParts, c)
					case []any:
						for _, block := range c {
							if bm, ok := block.(map[string]any); ok {
								if t, ok := bm["text"].(string); ok {
									contentParts = append(contentParts, t)
								}
							}
						}
					}
				}
			}

		case "function_call":
			var fc FunctionCallItem
			if err := json.Unmarshal(raw, &fc); err != nil {
				continue
			}
			tc := ToolCall{
				ID:   fc.CallID,
				Type: "function",
				Function: FunctionCall{
					Name:      fc.Name,
					Arguments: fc.Arguments,
				},
			}
			toolCalls = append(toolCalls, tc)
		}
	}

	if len(contentParts) > 0 {
		choice.Message.Content = strings.Join(contentParts, "")
	}
	if len(toolCalls) > 0 {
		choice.Message.ToolCalls = toolCalls
		choice.FinishReason = "tool_calls"
	} else {
		choice.FinishReason = "stop"
	}

	chatResp.Choices = []Choice{choice}

	// Extract usage from Extra if present
	if usageRaw, ok := resp.Extra["usage"]; ok {
		var usage Usage
		if err := json.Unmarshal(usageRaw, &usage); err == nil {
			chatResp.Usage = usage
		}
	}

	// Pass through other extra fields
	for k, v := range resp.Extra {
		if k != "usage" && k != "output" && k != "id" {
			chatResp.Extra[k] = v
		}
	}

	return chatResp, nil
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

// ResponsesSSEToChatSSE converts Responses API SSE events to Chat Completions SSE format.
func ResponsesSSEToChatSSE(rawSSE []byte) []byte {
	events := protocol.ParseEvents(rawSSE)
	var chatChunks []string

	// First chunk with role
	chatChunks = append(chatChunks, `data: {"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`+"\n")

	var finishReason string = "stop"
	var toolCallIndex int
	var usage Usage
	var hasFunctionCall bool

	for _, evt := range events {
		switch evt.EventType {
		case "response.output_text.delta":
			var deltaEvt struct {
				Delta string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &deltaEvt); err != nil {
				continue
			}
			escaped, _ := json.Marshal(deltaEvt.Delta)
			chatChunks = append(chatChunks, fmt.Sprintf(`data: {"choices":[{"index":0,"delta":{"content":%s},"finish_reason":null}]}`+"\n", string(escaped)))

		case "response.output_item.added":
			var addedEvt struct {
				Item struct {
					Type   string `json:"type"`
					CallID string `json:"call_id"`
					Name   string `json:"name"`
				} `json:"item"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &addedEvt); err != nil {
				continue
			}
			if addedEvt.Item.Type == "function_call" {
				hasFunctionCall = true
				tcJSON, _ := json.Marshal(map[string]any{
					"index": toolCallIndex,
					"id":    addedEvt.Item.CallID,
					"type":  "function",
					"function": map[string]string{
						"name":      addedEvt.Item.Name,
						"arguments": "",
					},
				})
				chatChunks = append(chatChunks, fmt.Sprintf(`data: {"choices":[{"index":0,"delta":{"tool_calls":[%s]},"finish_reason":null}]}`+"\n", string(tcJSON)))
				toolCallIndex++
			}

		case "response.function_call_arguments.delta":
			var deltaEvt struct {
				Delta string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &deltaEvt); err != nil {
				continue
			}
			// Find the current tool call index (last one)
			idx := toolCallIndex - 1
			if idx < 0 {
				idx = 0
			}
			escaped, _ := json.Marshal(deltaEvt.Delta)
			tcJSON, _ := json.Marshal(map[string]any{
				"index": idx,
				"function": map[string]string{
					"arguments": string(escaped),
				},
			})
			chatChunks = append(chatChunks, fmt.Sprintf(`data: {"choices":[{"index":0,"delta":{"tool_calls":[%s]},"finish_reason":null}]}`+"\n", string(tcJSON)))

		case "response.completed":
			// Extract usage from response.completed
			var completedEvt struct {
				Response ResponsesResponse `json:"response"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &completedEvt); err == nil {
				if usageRaw, ok := completedEvt.Response.Extra["usage"]; ok {
					json.Unmarshal(usageRaw, &usage)
				}
			}
			// Set finish_reason
			if hasFunctionCall {
				finishReason = "tool_calls"
			}
		}
	}

	// Final chunk with finish_reason
	finalChunk := map[string]any{
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": finishReason,
			},
		},
	}
	if usage != (Usage{}) {
		finalChunk["usage"] = usage
	}
	finalBytes, _ := json.Marshal(finalChunk)
	chatChunks = append(chatChunks, "data: "+string(finalBytes)+"\n")

	// Add [DONE]
	chatChunks = append(chatChunks, "data: [DONE]\n")

	return []byte(strings.Join(chatChunks, ""))
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
