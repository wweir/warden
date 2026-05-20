package openai

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/wweir/warden/pkg/protocol"
)

const (
	responsesReasoningOutputIndex = 0
	responsesMessageOutputIndex   = 0
	responsesMessageContentIdx    = 0
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
	var dsmlCalls []dsmlToolCall
	content := extractThinkFromContent(msg.Content, &msg.ReasoningContent)

	if msg.ReasoningContent != "" {
		item := map[string]any{
			"type":    "reasoning",
			"id":      makeReasoningItemID(chatResp.ID),
			"summary": makeReasoningSummary(msg.ReasoningContent),
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return resp, fmt.Errorf("marshal reasoning item: %w", err)
		}
		resp.Output = append(resp.Output, raw)
	}

	if text, ok := content.(string); ok && text != "" {
		remaining, calls, found := parseDSMLToolCalls(text)
		if found {
			content = remaining
			dsmlCalls = append(dsmlCalls, calls...)
		}
	} else if blocks, ok := content.([]any); ok {
		var newBlocks []any
		for _, block := range blocks {
			bm, ok := block.(map[string]any)
			if !ok {
				newBlocks = append(newBlocks, block)
				continue
			}
			if typ, _ := bm["type"].(string); typ == "text" {
				if text, _ := bm["text"].(string); text != "" {
					remaining, calls, found := parseDSMLToolCalls(text)
					if found {
						dsmlCalls = append(dsmlCalls, calls...)
						if remaining != "" {
							cloned := make(map[string]any, len(bm))
							for k, v := range bm {
								cloned[k] = v
							}
							cloned["text"] = remaining
							newBlocks = append(newBlocks, cloned)
						}
						continue
					}
				}
			}
			newBlocks = append(newBlocks, block)
		}
		content = newBlocks
	}

	if content != nil && !isEmptyContent(content) {
		item := map[string]any{
			"type":    "message",
			"role":    messageRoleOrDefault(msg.Role),
			"content": normalizeChatContentForResponses(content),
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return resp, fmt.Errorf("marshal message item: %w", err)
		}
		resp.Output = append(resp.Output, raw)
	}

	for _, tc := range msg.ToolCalls {
		name, arguments := normalizeToolCallArguments(tc.Function.Name, tc.Function.Arguments)
		fc := FunctionCallItem{
			Type:      "function_call",
			CallID:    tc.ID,
			Name:      name,
			Arguments: arguments,
			Status:    "completed",
		}
		raw, err := json.Marshal(fc)
		if err != nil {
			return resp, fmt.Errorf("marshal function_call item: %w", err)
		}
		resp.Output = append(resp.Output, raw)
	}

	for i, tc := range dsmlCalls {
		callID := tc.ID
		if callID == "" {
			callID = generateDSMLCallID(i)
		}
		fc := FunctionCallItem{
			Type:      "function_call",
			CallID:    callID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
			Status:    "completed",
		}
		raw, err := json.Marshal(fc)
		if err != nil {
			return resp, fmt.Errorf("marshal dsml function_call item: %w", err)
		}
		resp.Output = append(resp.Output, raw)
	}

	applyFinishReasonToResponsesResponse(&resp, choice.FinishReason)
	return resp, nil
}

func isEmptyContent(content any) bool {
	if content == nil {
		return true
	}
	if text, ok := content.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	if blocks, ok := content.([]any); ok {
		return len(blocks) == 0
	}
	return false
}

func normalizeChatContentForResponses(content any) any {
	if text, ok := content.(string); ok {
		return []any{map[string]any{
			"type": "output_text",
			"text": sanitizeAssistantText(text),
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

func extractThinkTags(text string) (reasoning string, remaining string) {
	var reasoningParts []string
	remaining = text

	for {
		startIdx := strings.Index(remaining, "<think>")
		if startIdx == -1 {
			break
		}
		afterOpen := remaining[startIdx+len("<think>"):]
		endIdx := strings.Index(afterOpen, "</think>")
		if endIdx == -1 {
			break
		}
		part := strings.TrimSpace(afterOpen[:endIdx])
		if part != "" {
			reasoningParts = append(reasoningParts, part)
		}
		remaining = remaining[:startIdx] + afterOpen[endIdx+len("</think>"):]
	}

	if len(reasoningParts) == 0 && remaining == text {
		return "", text
	}
	remaining = strings.TrimSpace(remaining)
	return strings.Join(reasoningParts, "\n\n"), remaining
}

// extractThinkFromContent extracts <think> tags from string or text-block array
// content and appends the reasoning to reasoningContent. Returns the content
// with think tags stripped.
func extractThinkFromContent(content any, reasoningContent *string) any {
	if text, ok := content.(string); ok && text != "" {
		reasoning, remaining := extractThinkTags(text)
		if reasoning != "" {
			if *reasoningContent != "" {
				*reasoningContent += "\n\n" + reasoning
			} else {
				*reasoningContent = reasoning
			}
		}
		if reasoning != "" || remaining != text {
			return remaining
		}
		return content
	}

	blocks, ok := content.([]any)
	if !ok {
		return content
	}

	var newBlocks []any
	var reasoningParts []string
	for _, block := range blocks {
		bm, ok := block.(map[string]any)
		if !ok {
			newBlocks = append(newBlocks, block)
			continue
		}
		if typ, _ := bm["type"].(string); typ == "text" {
			if text, _ := bm["text"].(string); text != "" {
				reasoning, remaining := extractThinkTags(text)
				if reasoning != "" {
					reasoningParts = append(reasoningParts, reasoning)
				}
				if remaining != "" {
					cloned := make(map[string]any, len(bm))
					for k, v := range bm {
						cloned[k] = v
					}
					cloned["text"] = remaining
					newBlocks = append(newBlocks, cloned)
					continue
				}
				// text was entirely think tags: drop this block
				continue
			}
		}
		newBlocks = append(newBlocks, block)
	}
	if len(reasoningParts) > 0 {
		joined := strings.Join(reasoningParts, "\n\n")
		if *reasoningContent != "" {
			*reasoningContent += "\n\n" + joined
		} else {
			*reasoningContent = joined
		}
	}
	return newBlocks
}

func mustMarshalRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func makeReasoningSummary(content string) []any {
	if content == "" {
		return []any{}
	}
	return []any{map[string]any{
		"type": "summary_text",
		"text": content,
	}}
}

func makeReasoningItemID(responseID string) string {
	if responseID != "" {
		return responseID + "_rs_0"
	}
	return "rs_0"
}

type responsesToolStreamState struct {
	callID    string
	name      string
	arguments string
}

type ChatResponsesStreamState struct {
	responseID        string
	model             string
	createdSent       bool
	inProgressSent    bool
	reasoningAdded    bool
	reasoningDone     bool
	reasoningItemID   string
	reasoningContent  string
	messageAdded      bool
	messageDone       bool
	messageItemID     string
	messageContent    string
	toolAdded         map[int]bool
	toolDone          map[int]bool
	toolItemID        map[int]string
	tools             map[int]*responsesToolStreamState
	pendingContent    string
	nextDSMLToolIndex int
	// thinking tracks whether we are inside a <think>...</think> block.
	thinking       bool
	thinkingBuffer string
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
	if reasoningContent, ok := delta["reasoning_content"].(string); ok && reasoningContent != "" {
		s.reasoningContent += reasoningContent
		if !s.reasoningAdded {
			s.reasoningItemID = s.makeReasoningItemID()
			responseChunks = append(responseChunks, formatResponsesEvent("response.output_item.added", map[string]any{
				"output_index": responsesReasoningOutputIndex,
				"item": map[string]any{
					"id":      s.reasoningItemID,
					"type":    "reasoning",
					"summary": makeReasoningSummary(s.reasoningContent),
				},
			}))
			s.reasoningAdded = true
		}
		s.emitReasoningDelta(reasoningContent, &responseChunks)
	}
	if content, ok := delta["content"].(string); ok && content != "" {
		clean := sanitizeAssistantText(content)
		if clean != "" {
			clean = s.processThinkTags(clean, &responseChunks)
			if clean != "" {
				s.pendingContent += clean

				for {
					loc := dsmlBlockPattern.FindStringIndex(s.pendingContent)
					if loc == nil {
						break
					}
					if loc[0] > 0 {
						prefix := strings.TrimRight(s.pendingContent[:loc[0]], " \t\n\r")
						if prefix != "" {
							s.emitTextDelta(prefix, &responseChunks)
						}
					}
					block := s.pendingContent[loc[0]:loc[1]]
					submatch := dsmlBlockPattern.FindStringSubmatch(block)
					if len(submatch) >= 2 {
						calls := parseDSMLInvokes(submatch[1])
						for _, tc := range calls {
							s.emitDSMLToolCall(tc, &responseChunks)
						}
					}
					s.pendingContent = s.pendingContent[loc[1]:]
					s.pendingContent = strings.TrimLeft(s.pendingContent, " \t\n\r")
				}

				if !hasIncompleteDSML(s.pendingContent) {
					if s.pendingContent != "" {
						s.emitTextDelta(s.pendingContent, &responseChunks)
						s.pendingContent = ""
					}
				}
			}
		}
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
				"output_index": s.toolOutputIndex(idx),
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
					"output_index": s.toolOutputIndex(idx),
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

	// Flush any dangling <think> buffer as reasoning.
	if s.thinking {
		reasoning := strings.TrimSpace(s.thinkingBuffer)
		s.thinkingBuffer = ""
		s.thinking = false
		if reasoning != "" {
			// Update content first before sending events
			s.reasoningContent += reasoning

			if !s.reasoningAdded {
				s.reasoningItemID = s.makeReasoningItemID()
				events = append(events, formatResponsesEvent("response.output_item.added", map[string]any{
					"output_index": responsesReasoningOutputIndex,
					"item": map[string]any{
						"id":      s.reasoningItemID,
						"type":    "reasoning",
						"summary": makeReasoningSummary(s.reasoningContent),
					},
				}))
				s.reasoningAdded = true
			}
			s.emitReasoningDelta(reasoning, &events)
		}
	} else if s.thinkingBuffer != "" {
		// Defensive: buffer has content but thinking flag is false.
		// Treat remaining buffer as pending content to avoid data loss.
		s.pendingContent += s.thinkingBuffer
		s.thinkingBuffer = ""
	}

	// Flush any buffered content with final DSML parse attempt.
	if s.pendingContent != "" {
		for {
			loc := dsmlBlockPattern.FindStringIndex(s.pendingContent)
			if loc == nil {
				break
			}
			if loc[0] > 0 {
				prefix := strings.TrimRight(s.pendingContent[:loc[0]], " \t\n\r")
				if prefix != "" {
					s.emitTextDelta(prefix, &events)
				}
			}
			block := s.pendingContent[loc[0]:loc[1]]
			submatch := dsmlBlockPattern.FindStringSubmatch(block)
			if len(submatch) >= 2 {
				calls := parseDSMLInvokes(submatch[1])
				for _, tc := range calls {
					s.emitDSMLToolCall(tc, &events)
				}
			}
			s.pendingContent = s.pendingContent[loc[1]:]
			s.pendingContent = strings.TrimLeft(s.pendingContent, " \t\n\r")
		}
		if s.pendingContent != "" {
			s.emitTextDelta(s.pendingContent, &events)
			s.pendingContent = ""
		}
	}

	if s.reasoningAdded && !s.reasoningDone {
		events = append(events, formatResponsesEvent("response.output_item.done", map[string]any{
			"output_index": responsesReasoningOutputIndex,
			"item": map[string]any{
				"id":      s.reasoningItemID,
				"type":    "reasoning",
				"summary": makeReasoningSummary(s.reasoningContent),
			},
		}))
		s.reasoningDone = true
	}
	if s.messageAdded && !s.messageDone {
		events = append(events, formatResponsesEvent("response.output_text.done", map[string]any{
			"output_index":  s.messageOutputIndex(),
			"item_id":       s.messageItemID,
			"content_index": responsesMessageContentIdx,
			"text":          s.messageContent,
		}))
		events = append(events, formatResponsesEvent("response.output_item.done", map[string]any{
			"output_index": s.messageOutputIndex(),
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
			"id":     s.toolItemID[idx],
			"type":   "function_call",
			"status": "completed",
		}
		name, arguments := normalizeToolCallArguments(toolState.name, toolState.arguments)
		item["arguments"] = arguments
		if toolState.callID != "" {
			item["call_id"] = toolState.callID
		}
		if name != "" {
			item["name"] = name
		}
		events = append(events, formatResponsesEvent("response.function_call_arguments.done", map[string]any{
			"output_index": s.toolOutputIndex(idx),
			"item_id":      s.toolItemID[idx],
			"arguments":    arguments,
		}))
		events = append(events, formatResponsesEvent("response.output_item.done", map[string]any{
			"output_index": s.toolOutputIndex(idx),
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

func (s *ChatResponsesStreamState) makeReasoningItemID() string {
	if s.responseID != "" {
		return s.responseID + "_rs_0"
	}
	return "rs_0"
}

func (s *ChatResponsesStreamState) messageOutputIndex() int {
	if s.reasoningAdded {
		return 1
	}
	return responsesMessageOutputIndex
}

func (s *ChatResponsesStreamState) toolOutputIndex(idx int) int {
	if s.reasoningAdded {
		return idx + 2
	}
	return idx + 1
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

func (s *ChatResponsesStreamState) emitReasoningDelta(delta string, responseChunks *[]string) {
	if delta == "" || s.reasoningItemID == "" {
		return
	}
	*responseChunks = append(*responseChunks, formatResponsesEvent("response.reasoning.delta", map[string]any{
		"output_index": responsesReasoningOutputIndex,
		"item_id":      s.reasoningItemID,
		"delta":        delta,
	}))
}

// processThinkTags extracts reasoning content from <think>...</think> tags that
// some models (e.g. DeepSeek R1) emit inside assistant content. It returns the
// remaining text after stripping think tags and emits reasoning delta events.
// hasIncompleteThinkTag reports whether text ends with a prefix of <think> or
// </think> that could be completed by a future chunk.
func hasIncompleteThinkTag(text string) bool {
	for _, tag := range []string{"<think>", "</think>"} {
		for i := 1; i <= len(tag); i++ {
			if strings.HasSuffix(text, tag[:i]) {
				return true
			}
		}
	}
	return false
}

func (s *ChatResponsesStreamState) processThinkTags(text string, responseChunks *[]string) string {
	if !s.thinking {
		// Fast path: no pending buffer and no '<' that could start a think tag.
		if s.thinkingBuffer == "" && !strings.Contains(text, "<") {
			return text
		}
	}

	s.thinkingBuffer += text

	for {
		if !s.thinking {
			idx := strings.Index(s.thinkingBuffer, "<think>")
			if idx == -1 {
				// No complete <think> found. If buffer doesn't end with an
				// incomplete think-tag prefix, flush the entire buffer.
				if !hasIncompleteThinkTag(s.thinkingBuffer) {
					result := s.thinkingBuffer
					s.thinkingBuffer = ""
					return result
				}
				// Buffer may contain an incomplete <think> tag: keep buffering.
				return ""
			}
			// Emit any text before <think> and enter thinking state.
			prefix := strings.TrimRight(s.thinkingBuffer[:idx], " \t\n\r")
			s.thinkingBuffer = s.thinkingBuffer[idx+len("<think>"):]
			s.thinking = true
			if prefix != "" {
				return prefix
			}
		}

		// Inside <think>: look for closing tag.
		idx := strings.Index(s.thinkingBuffer, "</think>")
		if idx == -1 {
			// Still inside think block: flush as reasoning.
			reasoning := s.thinkingBuffer
			s.thinkingBuffer = ""
			if reasoning != "" {
				// Update content first before sending events
				s.reasoningContent += reasoning

				if !s.reasoningAdded {
					s.reasoningItemID = s.makeReasoningItemID()
					*responseChunks = append(*responseChunks, formatResponsesEvent("response.output_item.added", map[string]any{
						"output_index": responsesReasoningOutputIndex,
						"item": map[string]any{
							"id":      s.reasoningItemID,
							"type":    "reasoning",
							"summary": makeReasoningSummary(s.reasoningContent),
						},
					}))
					s.reasoningAdded = true
				}
				s.emitReasoningDelta(reasoning, responseChunks)
			}
			return ""
		}

		reasoning := strings.TrimSpace(s.thinkingBuffer[:idx])
		s.thinkingBuffer = s.thinkingBuffer[idx+len("</think>"):]
		s.thinking = false
		if reasoning != "" {
			// Update content first before sending events
			s.reasoningContent += reasoning

			if !s.reasoningAdded {
				s.reasoningItemID = s.makeReasoningItemID()
				*responseChunks = append(*responseChunks, formatResponsesEvent("response.output_item.added", map[string]any{
					"output_index": responsesReasoningOutputIndex,
					"item": map[string]any{
						"id":      s.reasoningItemID,
						"type":    "reasoning",
						"summary": makeReasoningSummary(s.reasoningContent),
					},
				}))
				s.reasoningAdded = true
			}
			s.emitReasoningDelta(reasoning, responseChunks)
		}
		// Continue loop to process any text after </think>.
	}
}

func (s *ChatResponsesStreamState) emitTextDelta(text string, responseChunks *[]string) {
	if text == "" {
		return
	}
	if !s.messageAdded {
		s.messageItemID = s.makeMessageItemID()
		*responseChunks = append(*responseChunks, formatResponsesEvent("response.output_item.added", map[string]any{
			"output_index": s.messageOutputIndex(),
			"item": map[string]any{
				"id":      s.messageItemID,
				"type":    "message",
				"role":    "assistant",
				"content": []any{},
			},
		}))
		s.messageAdded = true
	}
	s.messageContent += text
	*responseChunks = append(*responseChunks, formatResponsesEvent("response.output_text.delta", map[string]any{
		"output_index":  s.messageOutputIndex(),
		"item_id":       s.messageItemID,
		"content_index": responsesMessageContentIdx,
		"delta":         text,
	}))
}

func (s *ChatResponsesStreamState) emitDSMLToolCall(tc dsmlToolCall, responseChunks *[]string) {
	// Find a slot that doesn't conflict with native tool calls.
	idx := s.nextDSMLToolIndex
	for s.toolAdded[idx] {
		idx++
	}
	s.nextDSMLToolIndex = idx + 1
	itemID := s.makeToolItemID(idx, nil)
	callID := tc.ID
	if callID == "" {
		callID = generateDSMLCallID(idx)
	}
	s.toolItemID[idx] = itemID
	s.toolAdded[idx] = true

	toolState := s.ensureToolState(idx)
	toolState.callID = callID
	toolState.name = tc.Name
	toolState.arguments = tc.Arguments

	// output_item.added
	item := map[string]any{"id": itemID, "type": "function_call"}
	if callID != "" {
		item["call_id"] = callID
	}
	if tc.Name != "" {
		item["name"] = tc.Name
	}
	*responseChunks = append(*responseChunks, formatResponsesEvent("response.output_item.added", map[string]any{
		"output_index": s.toolOutputIndex(idx),
		"item":         item,
	}))

	// function_call_arguments.done
	*responseChunks = append(*responseChunks, formatResponsesEvent("response.function_call_arguments.done", map[string]any{
		"output_index": s.toolOutputIndex(idx),
		"item_id":      itemID,
		"arguments":    tc.Arguments,
	}))

	// output_item.done
	doneItem := map[string]any{
		"id":        itemID,
		"type":      "function_call",
		"status":    "completed",
		"arguments": tc.Arguments,
	}
	if callID != "" {
		doneItem["call_id"] = callID
	}
	if tc.Name != "" {
		doneItem["name"] = tc.Name
	}
	*responseChunks = append(*responseChunks, formatResponsesEvent("response.output_item.done", map[string]any{
		"output_index": s.toolOutputIndex(idx),
		"item":         doneItem,
	}))
	s.toolDone[idx] = true
}

func normalizeToolCallArguments(name string, arguments string) (string, string) {
	_, calls, found := parseDSMLToolCalls(arguments)
	if !found || len(calls) == 0 {
		return name, arguments
	}

	call := calls[0]
	if call.Name != "" {
		name = call.Name
	}
	return name, call.Arguments
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
