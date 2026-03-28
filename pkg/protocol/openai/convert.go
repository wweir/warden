package openai

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/wweir/warden/pkg/protocol"
)

const (
	responsesMessageOutputIndex = 0
	responsesMessageContentIdx  = 0
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

	tools, toolNames, err := convertResponsesTools(respReq.Tools)
	if err != nil {
		return chatReq, fmt.Errorf("convert tool: %w", err)
	}
	chatReq.Tools = tools

	messages, err := convertResponsesInputToMessages(respReq.Input)
	if err != nil {
		return chatReq, fmt.Errorf("convert input: %w", err)
	}

	messages, extra, err := convertResponsesRequestExtras(messages, respReq.Extra, toolNames)
	if err != nil {
		return chatReq, err
	}
	chatReq.Messages = messages
	if len(extra) > 0 {
		maps.Copy(chatReq.Extra, extra)
	}

	return chatReq, nil
}

// DowngradeDeveloperMessages converts developer-role messages into system-role messages.
// Some OpenAI-compatible providers reject developer even though the rest of the chat schema matches.
func DowngradeDeveloperMessages(messages []Message) ([]Message, bool) {
	if len(messages) == 0 {
		return nil, false
	}

	cloned := make([]Message, len(messages))
	changed := false
	for i, msg := range messages {
		cloned[i] = msg
		if msg.Role == "developer" {
			cloned[i].Role = "system"
			changed = true
		}
	}

	if !changed {
		return messages, false
	}
	return cloned, true
}

func convertResponsesRequestExtras(messages []Message, extra map[string]json.RawMessage, toolNames map[string]struct{}) ([]Message, map[string]json.RawMessage, error) {
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
		case "max_output_tokens":
			mapped, include, err := convertResponsesMaxOutputTokens(raw, extra)
			if err != nil {
				return nil, nil, fmt.Errorf("convert max_output_tokens: %w", err)
			}
			if include {
				chatExtra["max_completion_tokens"] = mapped
			}
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
		case "tool_choice":
			toolChoice, err := normalizeResponsesToolChoice(raw, toolNames)
			if err != nil {
				return nil, nil, fmt.Errorf("convert tool_choice: %w", err)
			}
			chatExtra[key] = toolChoice
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
			content, err := normalizeFunctionCallOutputContent(out.Output)
			if err != nil {
				return nil, fmt.Errorf("normalize function_call_output.output: %w", err)
			}
			messages = append(messages, Message{Role: "tool", ToolCallID: out.CallID, Content: content})

		case "reasoning":
			return nil, fmt.Errorf("unsupported input item type %q", itemType.Type)

		default:
			return nil, fmt.Errorf("unsupported input item type %q", itemType.Type)
		}
	}

	return messages, nil
}

func normalizeFunctionCallOutputContent(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return "", nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}

	var compact json.RawMessage
	if err := json.Unmarshal(raw, &compact); err != nil {
		return nil, err
	}
	return string(compact), nil
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

func convertResponsesTools(rawTools []json.RawMessage) ([]Tool, map[string]struct{}, error) {
	tools := make([]Tool, 0, len(rawTools))
	toolNames := make(map[string]struct{}, len(rawTools))
	for _, rawTool := range rawTools {
		tool, err := convertResponsesToolToChatTool(rawTool)
		if err != nil {
			return nil, nil, err
		}
		tools = append(tools, tool)
		if name := strings.TrimSpace(tool.Function.Name); name != "" {
			toolNames[name] = struct{}{}
		}
	}
	return tools, toolNames, nil
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
			Strict:      flatTool.Strict,
		},
	}, nil
}

func convertResponsesMaxOutputTokens(raw json.RawMessage, extra map[string]json.RawMessage) (json.RawMessage, bool, error) {
	limit, err := decodeTokenLimit(raw)
	if err != nil {
		return nil, false, err
	}
	for _, key := range []string{"max_completion_tokens", "max_tokens"} {
		otherRaw, ok := extra[key]
		if !ok {
			continue
		}
		otherLimit, err := decodeTokenLimit(otherRaw)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", key, err)
		}
		if !limit.equals(otherLimit) {
			return nil, false, fmt.Errorf("conflicts with %s", key)
		}
		return nil, false, nil
	}
	if limit.infinite {
		return nil, false, nil
	}
	mapped, err := json.Marshal(limit.value)
	if err != nil {
		return nil, false, err
	}
	return mapped, true, nil
}

func normalizeResponsesToolChoice(raw json.RawMessage, toolNames map[string]struct{}) (json.RawMessage, error) {
	var toolChoiceType string
	if err := json.Unmarshal(raw, &toolChoiceType); err == nil {
		switch toolChoiceType {
		case "auto", "none":
			return json.RawMessage(strconv.Quote(toolChoiceType)), nil
		case "required":
			if len(toolNames) == 0 {
				return nil, fmt.Errorf("requires at least one function tool")
			}
			return json.RawMessage(`"required"`), nil
		default:
			return nil, fmt.Errorf("unsupported string value %q", toolChoiceType)
		}
	}

	var choice struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &choice); err != nil {
		return nil, fmt.Errorf("unsupported shape")
	}

	switch choice.Type {
	case "auto", "none", "required":
		return normalizeResponsesToolChoice(json.RawMessage(strconv.Quote(choice.Type)), toolNames)
	case "function":
		name := strings.TrimSpace(choice.Name)
		if nestedName := strings.TrimSpace(choice.Function.Name); nestedName != "" {
			if name != "" && name != nestedName {
				return nil, fmt.Errorf("function name mismatch between name and function.name")
			}
			name = nestedName
		}
		if name == "" {
			return nil, fmt.Errorf("function name is required")
		}
		if len(toolNames) == 0 {
			return nil, fmt.Errorf("references function %q but no function tools are configured", name)
		}
		if _, ok := toolNames[name]; !ok {
			return nil, fmt.Errorf("references unknown function %q", name)
		}
		return json.Marshal(map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": name,
			},
		})
	default:
		return nil, fmt.Errorf("unsupported type %q", choice.Type)
	}
}

type tokenLimit struct {
	infinite bool
	value    int64
}

type responsesToolStreamState struct {
	callID    string
	name      string
	arguments string
}

func (l tokenLimit) equals(other tokenLimit) bool {
	if l.infinite || other.infinite {
		return l.infinite == other.infinite
	}
	return l.value == other.value
}

func decodeTokenLimit(raw json.RawMessage) (tokenLimit, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if strings.EqualFold(strings.TrimSpace(s), "inf") {
			return tokenLimit{infinite: true}, nil
		}
	}

	value, err := decodePositiveInt(raw)
	if err != nil {
		return tokenLimit{}, err
	}
	if value <= 0 {
		return tokenLimit{}, fmt.Errorf("expected positive integer or \"inf\"")
	}
	return tokenLimit{value: value}, nil
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
	if id, ok := chunk["id"].(string); ok && id != "" {
		s.responseID = id
	}
	if model, ok := chunk["model"].(string); ok && model != "" {
		s.model = model
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

	return []byte(strings.Join(responseChunks, ""))
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
