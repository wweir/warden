package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wweir/warden/pkg/protocol"
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

	// Handle assistant with tool_calls: generate function_call items
	if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
		// First, add the assistant message with content (if any)
		if msg.Content != nil {
			assistantItem := map[string]any{
				"type":    "message",
				"role":    "assistant",
				"content": msg.Content,
			}
			bytes, _ := json.Marshal(assistantItem)
			items = append(items, bytes)
		}
		// Then add function_call items for each tool call
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