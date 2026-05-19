package openai

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"
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
		case "reasoning":
			effort, err := convertResponsesReasoning(raw)
			if err != nil {
				return nil, nil, fmt.Errorf("convert reasoning: %w", err)
			}
			if effort != "" {
				chatExtra["reasoning_effort"] = json.RawMessage(strconv.Quote(effort))
			} else {
				// preserve original reasoning object for upstream providers that understand it
				chatExtra["reasoning"] = raw
			}
		case "include", "truncation":
			// ignored: Responses-only features not available in Chat Completions
		default:
			if _, ok := responsesToChatAllowedExtraFields[key]; ok {
				chatExtra[key] = raw
			}
			// unknown fields are silently ignored to avoid breaking clients that send
			// Responses-only features (e.g. prompt_cache_key, text, include)
		}
	}

	return messages, chatExtra, nil
}

func convertResponsesReasoning(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}

	var obj struct {
		Effort string `json:"effort"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", fmt.Errorf("unmarshal reasoning: %w", err)
	}
	return obj.Effort, nil
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
			var reasoningItem map[string]any
			if err := json.Unmarshal(raw, &reasoningItem); err != nil {
				return nil, fmt.Errorf("unmarshal reasoning item: %w", err)
			}
			reasoningJSON, _ := json.Marshal(reasoningItem)
			msg := Message{
				Role:             "assistant",
				ReasoningContent: string(reasoningJSON),
			}
			messages = append(messages, msg)

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
	if v, ok := fields["reasoning_content"]; ok {
		var rc string
		if err := json.Unmarshal(v, &rc); err != nil {
			return Message{}, fmt.Errorf("unmarshal message reasoning_content: %w", err)
		}
		msg.ReasoningContent = rc
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

func convertResponsesTools(rawTools []json.RawMessage) ([]Tool, map[string]struct{}, error) {
	tools := make([]Tool, 0, len(rawTools))
	toolNames := make(map[string]struct{}, len(rawTools))
	for _, rawTool := range rawTools {
		tool, ok, err := convertResponsesToolToChatTool(rawTool)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue
		}
		tools = append(tools, tool)
		if name := strings.TrimSpace(tool.Function.Name); name != "" {
			toolNames[name] = struct{}{}
		}
	}
	return tools, toolNames, nil
}

func convertResponsesToolToChatTool(raw json.RawMessage) (Tool, bool, error) {
	var typeCheck struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &typeCheck); err != nil {
		return Tool{}, false, fmt.Errorf("unmarshal tool type: %w", err)
	}
	if typeCheck.Type != "function" && typeCheck.Type != "custom" {
		return Tool{}, false, nil
	}

	var flatTool ResponsesFunctionTool
	if err := json.Unmarshal(raw, &flatTool); err != nil {
		return Tool{}, false, fmt.Errorf("unmarshal function tool: %w", err)
	}

	var params any
	if len(flatTool.Parameters) > 0 {
		if err := json.Unmarshal(flatTool.Parameters, &params); err != nil {
			return Tool{}, false, fmt.Errorf("unmarshal tool parameters: %w", err)
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
	}, true, nil
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
