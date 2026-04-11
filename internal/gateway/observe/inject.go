package observe

import (
	"encoding/json"

	"github.com/wweir/warden/pkg/toolhook"
)

// InjectChatBlockVerdicts removes rejected tool calls from a Chat Completion response.
// It modifies each choice.message.tool_calls by filtering out entries whose function.name
// and id match a rejected verdict.
func InjectChatBlockVerdicts(respBody []byte, verdicts []toolhook.HookVerdict) []byte {
	rejected := buildRejectedSet(verdicts)
	if len(rejected) == 0 {
		return respBody
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return respBody
	}

	choicesRaw, ok := raw["choices"]
	if !ok {
		return respBody
	}
	var choices []json.RawMessage
	if err := json.Unmarshal(choicesRaw, &choices); err != nil || len(choices) == 0 {
		return respBody
	}

	changed := false
	for i, choiceRaw := range choices {
		var choice map[string]json.RawMessage
		if err := json.Unmarshal(choiceRaw, &choice); err != nil {
			continue
		}
		msgRaw, ok := choice["message"]
		if !ok {
			continue
		}
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(msgRaw, &msg); err != nil {
			continue
		}
		toolCallsRaw, ok := msg["tool_calls"]
		if !ok {
			continue
		}
		var toolCalls []json.RawMessage
		if err := json.Unmarshal(toolCallsRaw, &toolCalls); err != nil {
			continue
		}

		filtered := filterToolCalls(toolCalls, rejected)
		if len(filtered) == len(toolCalls) {
			continue
		}
		changed = true

		b, err := json.Marshal(filtered)
		if err != nil {
			return respBody
		}
		if len(filtered) == 0 {
			delete(msg, "tool_calls")
		} else {
			msg["tool_calls"] = b
		}
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			return respBody
		}
		choice["message"] = msgBytes

		if len(filtered) == 0 {
			if fr, ok := choice["finish_reason"]; ok {
				var reason string
				if json.Unmarshal(fr, &reason) == nil && reason == "tool_calls" {
					b, _ := json.Marshal("stop")
					choice["finish_reason"] = b
				}
			}
		}

		choiceBytes, err := json.Marshal(choice)
		if err != nil {
			return respBody
		}
		choices[i] = choiceBytes
	}
	if !changed {
		return respBody
	}

	choicesBytes, _ := json.Marshal(choices)
	raw["choices"] = choicesBytes
	result, err := json.Marshal(raw)
	if err != nil {
		return respBody
	}
	return result
}

// InjectResponsesBlockVerdicts removes rejected function_call items from a Responses API response.
// It modifies output[] by filtering out entries with type "function_call" whose call_id or name
// match a rejected verdict.
func InjectResponsesBlockVerdicts(respBody []byte, verdicts []toolhook.HookVerdict) []byte {
	rejected := buildRejectedSet(verdicts)
	if len(rejected) == 0 {
		return respBody
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return respBody
	}

	outputRaw, ok := raw["output"]
	if !ok {
		return respBody
	}
	var output []json.RawMessage
	if err := json.Unmarshal(outputRaw, &output); err != nil {
		return respBody
	}

	filtered := filterResponsesOutput(output, rejected)
	if len(filtered) == len(output) {
		return respBody
	}

	b, err := json.Marshal(filtered)
	if err != nil {
		return respBody
	}
	raw["output"] = b
	result, err := json.Marshal(raw)
	if err != nil {
		return respBody
	}
	return result
}

// InjectAnthropicBlockVerdicts removes rejected tool_use blocks from an Anthropic response.
// It modifies content[] by filtering out entries with type "tool_use" whose id matches
// a rejected verdict's CallID.
func InjectAnthropicBlockVerdicts(respBody []byte, verdicts []toolhook.HookVerdict) []byte {
	rejected := buildRejectedSet(verdicts)
	if len(rejected) == 0 {
		return respBody
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return respBody
	}

	contentRaw, ok := raw["content"]
	if !ok {
		return respBody
	}
	var content []json.RawMessage
	if err := json.Unmarshal(contentRaw, &content); err != nil {
		return respBody
	}

	var filtered []json.RawMessage
	for _, item := range content {
		var entry struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(item, &entry) == nil && entry.Type == "tool_use" {
			key := entry.ID
			if key == "" {
				key = entry.Name
			}
			if rejected[key] {
				continue
			}
		}
		filtered = append(filtered, item)
	}

	if len(filtered) == len(content) {
		return respBody
	}

	b, err := json.Marshal(filtered)
	if err != nil {
		return respBody
	}
	raw["content"] = b

	// Update stop_reason if it was "tool_use" and all tool_use blocks were removed
	hasToolUse := false
	for _, item := range filtered {
		var entry struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(item, &entry) == nil && entry.Type == "tool_use" {
			hasToolUse = true
			break
		}
	}
	if !hasToolUse {
		if sr, ok := raw["stop_reason"]; ok {
			var reason string
			if json.Unmarshal(sr, &reason) == nil && reason == "tool_use" {
				b, _ := json.Marshal("end_turn")
				raw["stop_reason"] = b
			}
		}
	}

	result, err := json.Marshal(raw)
	if err != nil {
		return respBody
	}
	return result
}

// buildRejectedSet builds a set of call IDs (or tool names as fallback) for rejected verdicts.
func buildRejectedSet(verdicts []toolhook.HookVerdict) map[string]bool {
	m := make(map[string]bool)
	for _, v := range verdicts {
		if v.Rejected {
			key := v.CallID
			if key == "" {
				key = v.ToolName
			}
			m[key] = true
		}
	}
	return m
}

// filterToolCalls filters OpenAI Chat tool_calls by checking id against rejected set.
func filterToolCalls(toolCalls []json.RawMessage, rejected map[string]bool) []json.RawMessage {
	var filtered []json.RawMessage
	for _, tc := range toolCalls {
		var entry struct {
			ID       string `json:"id"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if json.Unmarshal(tc, &entry) == nil {
			key := entry.ID
			if key == "" {
				key = entry.Function.Name
			}
			if rejected[key] {
				continue
			}
		}
		filtered = append(filtered, tc)
	}
	return filtered
}

// filterResponsesOutput filters Responses API output items, removing rejected function_call entries.
func filterResponsesOutput(output []json.RawMessage, rejected map[string]bool) []json.RawMessage {
	var filtered []json.RawMessage
	for _, item := range output {
		var entry struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Name   string `json:"name"`
		}
		if json.Unmarshal(item, &entry) == nil && entry.Type == "function_call" {
			key := entry.CallID
			if key == "" {
				key = entry.Name
			}
			if rejected[key] {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}
