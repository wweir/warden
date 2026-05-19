package openai

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/wweir/warden/pkg/protocol"
)

const (
	responsesMessageOutputIndex = 0
	responsesMessageContentIdx  = 0
)

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
	if !chatResp.Usage.IsZero() {
		resp.Extra["usage"] = mustMarshalRaw(normalizeChatUsageForResponses(chatResp.Usage))
	}

	if len(chatResp.Choices) == 0 {
		return resp, nil
	}

	choice := chatResp.Choices[0]
	msg := choice.Message
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

	applyFinishReasonToResponsesResponse(&resp, choice.FinishReason)
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

func normalizeChatUsageForResponses(usage Usage) map[string]any {
	normalized := make(map[string]any)
	if usage.PromptTokens != 0 {
		normalized["input_tokens"] = usage.PromptTokens
	}
	if usage.CompletionTokens != 0 {
		normalized["output_tokens"] = usage.CompletionTokens
	}
	if usage.TotalTokens != 0 {
		normalized["total_tokens"] = usage.TotalTokens
	}
	for key, value := range usage.Extra {
		switch key {
		case "prompt_tokens_details":
			normalized["input_tokens_details"] = rawMessageToInterface(value)
		case "completion_tokens_details":
			normalized["output_tokens_details"] = rawMessageToInterface(value)
		default:
			normalized[key] = rawMessageToInterface(value)
		}
	}
	return normalized
}

func applyFinishReasonToResponsesResponse(resp *ResponsesResponse, finishReason string) {
	if resp == nil {
		return
	}

	switch finishReason {
	case "", "stop", "tool_calls", "function_call":
		return
	case "length":
		resp.Status = "incomplete"
		resp.Extra["incomplete_details"] = mustMarshalRaw(map[string]any{
			"reason": "max_output_tokens",
		})
	case "content_filter":
		resp.Status = "incomplete"
		resp.Extra["incomplete_details"] = mustMarshalRaw(map[string]any{
			"reason": "content_filter",
		})
	default:
		resp.Status = "incomplete"
		resp.Extra["incomplete_details"] = mustMarshalRaw(map[string]any{
			"reason":        "finish_reason",
			"finish_reason": finishReason,
		})
	}
}

func rawMessageToInterface(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}

func mustMarshalRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

type responsesToolStreamState struct {
	callID    string
	name      string
	arguments string
}

type ChatResponsesStreamState struct {
	responseID     string
	model          string
	createdSent    bool
	inProgressSent bool
	messageAdded   bool
	messageDone    bool
	messageItemID  string
	messageContent string
	toolAdded      map[int]bool
	toolDone       map[int]bool
	toolItemID     map[int]string
	tools          map[int]*responsesToolStreamState
}

func NewChatResponsesStreamState() *ChatResponsesStreamState {
	return &ChatResponsesStreamState{
		toolAdded:  make(map[int]bool),
		toolDone:   make(map[int]bool),
		toolItemID: make(map[int]string),
		tools:      make(map[int]*responsesToolStreamState),
	}
}

func (s *ChatResponsesStreamState) ConvertEvent(evt protocol.Event) ([]byte, error) {
	if evt.Data == "" || evt.Data == "[DONE]" {
		return nil, nil
	}

	var chunk map[string]any
	if err := json.Unmarshal([]byte(evt.Data), &chunk); err != nil {
		return nil, fmt.Errorf("unmarshal chat stream chunk: %w", err)
	}

	choices, _ := asArray(chunk["choices"])
	if len(choices) == 0 {
		return nil, nil
	}
	if id, ok := chunk["id"].(string); ok && id != "" {
		s.responseID = id
	}
	if model, ok := chunk["model"].(string); ok && model != "" {
		s.model = model
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return nil, nil
	}
	delta, _ := choice["delta"].(map[string]any)
	if delta == nil {
		return nil, nil
	}

	var responseChunks []string
	responseChunks = append(responseChunks, s.ensureLifecycleEvents()...)
	if content, ok := delta["content"].(string); ok && content != "" {
		if !s.messageAdded {
			s.messageItemID = s.makeMessageItemID()
			responseChunks = append(responseChunks, formatResponsesEvent("response.output_item.added", map[string]any{
				"output_index": responsesMessageOutputIndex,
				"item": map[string]any{
					"id":      s.messageItemID,
					"type":    "message",
					"role":    "assistant",
					"content": []any{},
				},
			}))
			s.messageAdded = true
		}
		s.messageContent += content
		responseChunks = append(responseChunks, formatResponsesEvent("response.output_text.delta", map[string]any{
			"output_index":  responsesMessageOutputIndex,
			"item_id":       s.messageItemID,
			"content_index": responsesMessageContentIdx,
			"delta":         content,
		}))
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
			itemID := s.makeToolItemID(idx, dtc)
			s.toolItemID[idx] = itemID
			item := map[string]any{"id": itemID, "type": "function_call"}
			if id, ok := dtc["id"].(string); ok && id != "" {
				item["call_id"] = id
			}
			if fn != nil {
				if name, ok := fn["name"].(string); ok && name != "" {
					item["name"] = name
				}
			}
			responseChunks = append(responseChunks, formatResponsesEvent("response.output_item.added", map[string]any{
				"output_index": idx + 1,
				"item":         item,
			}))
			s.toolAdded[idx] = true
		}
		toolState := s.ensureToolState(idx)
		if id, ok := dtc["id"].(string); ok && id != "" {
			toolState.callID = id
		}
		if fn != nil {
			if name, ok := fn["name"].(string); ok && name != "" {
				toolState.name = name
			}
		}

		if fn != nil {
			if args, ok := fn["arguments"].(string); ok && args != "" {
				toolState.arguments += args
				payload := map[string]any{
					"output_index": idx + 1,
					"item_id":      s.toolItemID[idx],
					"delta":        args,
				}
				if id, ok := dtc["id"].(string); ok && id != "" {
					payload["call_id"] = id
				}
				responseChunks = append(responseChunks, formatResponsesEvent("response.function_call_arguments.delta", payload))
			}
		}
	}

	if finishReason, _ := choice["finish_reason"].(string); finishReason != "" {
		responseChunks = append(responseChunks, s.finalizeOutputEvents()...)
	}

	return []byte(strings.Join(responseChunks, "")), nil
}

func (s *ChatResponsesStreamState) ensureLifecycleEvents() []string {
	var events []string
	response := s.responseObject("in_progress")
	if !s.createdSent {
		events = append(events, formatResponsesEvent("response.created", map[string]any{"response": response}))
		s.createdSent = true
	}
	if !s.inProgressSent {
		events = append(events, formatResponsesEvent("response.in_progress", map[string]any{"response": response}))
		s.inProgressSent = true
	}
	return events
}

func (s *ChatResponsesStreamState) finalizeOutputEvents() []string {
	var events []string
	if s.messageAdded && !s.messageDone {
		events = append(events, formatResponsesEvent("response.output_text.done", map[string]any{
			"output_index":  responsesMessageOutputIndex,
			"item_id":       s.messageItemID,
			"content_index": responsesMessageContentIdx,
			"text":          s.messageContent,
		}))
		events = append(events, formatResponsesEvent("response.output_item.done", map[string]any{
			"output_index": responsesMessageOutputIndex,
			"item": map[string]any{
				"id":   s.messageItemID,
				"type": "message",
				"role": "assistant",
				"content": []any{map[string]any{
					"type": "output_text",
					"text": s.messageContent,
				}},
			},
		}))
		s.messageDone = true
	}

	toolIndexes := make([]int, 0, len(s.toolAdded))
	for idx := range s.toolAdded {
		toolIndexes = append(toolIndexes, idx)
	}
	slices.Sort(toolIndexes)
	for _, idx := range toolIndexes {
		if s.toolDone[idx] {
			continue
		}
		toolState := s.ensureToolState(idx)
		item := map[string]any{
			"id":        s.toolItemID[idx],
			"type":      "function_call",
			"status":    "completed",
			"arguments": toolState.arguments,
		}
		if toolState.callID != "" {
			item["call_id"] = toolState.callID
		}
		if toolState.name != "" {
			item["name"] = toolState.name
		}
		events = append(events, formatResponsesEvent("response.function_call_arguments.done", map[string]any{
			"output_index": idx + 1,
			"item_id":      s.toolItemID[idx],
			"arguments":    toolState.arguments,
		}))
		events = append(events, formatResponsesEvent("response.output_item.done", map[string]any{
			"output_index": idx + 1,
			"item":         item,
		}))
		s.toolDone[idx] = true
	}
	return events
}

func (s *ChatResponsesStreamState) responseObject(status string) map[string]any {
	response := map[string]any{
		"object": "response",
		"status": status,
	}
	if s.responseID != "" {
		response["id"] = s.responseID
	}
	if s.model != "" {
		response["model"] = s.model
	}
	return response
}

func (s *ChatResponsesStreamState) makeMessageItemID() string {
	if s.responseID != "" {
		return s.responseID + "_msg_0"
	}
	return "msg_0"
}

func (s *ChatResponsesStreamState) makeToolItemID(idx int, toolCall map[string]any) string {
	if id, ok := toolCall["id"].(string); ok && id != "" {
		return id
	}
	if s.responseID != "" {
		return fmt.Sprintf("%s_fc_%d", s.responseID, idx)
	}
	return fmt.Sprintf("fc_%d", idx)
}

func (s *ChatResponsesStreamState) ensureToolState(idx int) *responsesToolStreamState {
	if toolState, ok := s.tools[idx]; ok {
		return toolState
	}
	toolState := &responsesToolStreamState{}
	s.tools[idx] = toolState
	return toolState
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
func ChatSSEToResponsesSSE(rawSSE []byte, model string) ([]byte, error) {
	events := protocol.ParseEvents(rawSSE)
	var responseChunks []string
	state := NewChatResponsesStreamState()
	streamComplete := false

	for _, evt := range events {
		if evt.Data == "[DONE]" {
			streamComplete = true
			continue
		}
		chunk, err := state.ConvertEvent(evt)
		if err != nil {
			return nil, err
		}
		if len(chunk) > 0 {
			responseChunks = append(responseChunks, string(chunk))
		}
	}

	responseChunks = append(responseChunks, string(BuildChatResponsesCompletedEvent(rawSSE, model, streamComplete)))

	return []byte(strings.Join(responseChunks, "")), nil
}

func formatResponsesEvent(eventType string, payload any) string {
	dataMap, ok := payload.(map[string]any)
	if !ok {
		dataMap = map[string]any{"payload": payload}
	}
	if _, exists := dataMap["type"]; !exists {
		dataMap["type"] = eventType
	}
	data, _ := json.Marshal(dataMap)
	return "event: " + eventType + "\ndata: " + string(data) + "\n\n"
}
