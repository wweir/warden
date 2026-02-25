package gateway

import (
	"encoding/json"

	"github.com/wweir/warden/config"
)

// injectSystemPromptRaw prepends a system message into raw JSON request body
// for Chat Completion requests. It's a no-op if no prompt is configured for the model,
// or if the messages already start with the same system prompt.
func injectSystemPromptRaw(rawBody []byte, route *config.RouteConfig, model string) []byte {
	prompt, ok := route.SystemPrompts[model]
	if !ok || prompt == "" {
		return rawBody
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return rawBody
	}

	msgsRaw, ok := body["messages"]
	if !ok {
		return rawBody
	}

	var msgs []json.RawMessage
	if err := json.Unmarshal(msgsRaw, &msgs); err != nil {
		return rawBody
	}

	// check if first message is already our system prompt
	if len(msgs) > 0 {
		var first struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal(msgs[0], &first) == nil && first.Role == "system" && first.Content == prompt {
			return rawBody
		}
	}

	sysMsg, _ := json.Marshal(map[string]string{"role": "system", "content": prompt})
	msgs = append([]json.RawMessage{sysMsg}, msgs...)
	body["messages"], _ = json.Marshal(msgs)

	result, err := json.Marshal(body)
	if err != nil {
		return rawBody
	}
	return result
}

// injectSystemPromptResponses prepends a system message into a Responses API input.
// It's a no-op if no prompt is configured for the model,
// or if the input already starts with the same system message.
func injectSystemPromptResponses(input json.RawMessage, route *config.RouteConfig, model string) json.RawMessage {
	prompt, ok := route.SystemPrompts[model]
	if !ok || prompt == "" {
		return input
	}

	sysItem := map[string]any{
		"role":    "developer",
		"content": prompt,
	}

	// string input: convert to array [sysItem, userMessage]
	if len(input) > 0 && input[0] == '"' {
		var s string
		if err := json.Unmarshal(input, &s); err != nil {
			return input
		}
		userItem := map[string]any{
			"role":    "user",
			"content": s,
		}
		result, err := json.Marshal([]any{sysItem, userItem})
		if err != nil {
			return input
		}
		return result
	}

	// array input: prepend system item
	if len(input) > 0 && input[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(input, &items); err != nil {
			return input
		}

		// check if first item is already our system prompt
		if len(items) > 0 {
			var first struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			if json.Unmarshal(items[0], &first) == nil && first.Role == "developer" && first.Content == prompt {
				return input
			}
		}

		sysRaw, _ := json.Marshal(sysItem)
		items = append([]json.RawMessage{sysRaw}, items...)
		result, err := json.Marshal(items)
		if err != nil {
			return input
		}
		return result
	}

	return input
}

// injectSystemPromptResponsesRaw prepends a developer message into raw JSON request body
// for Responses API requests. It's a no-op if no prompt is configured for the model.
func injectSystemPromptResponsesRaw(rawBody []byte, route *config.RouteConfig, model string) []byte {
	prompt, ok := route.SystemPrompts[model]
	if !ok || prompt == "" {
		return rawBody
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return rawBody
	}

	inputRaw, ok := body["input"]
	if !ok {
		return rawBody
	}

	body["input"] = injectSystemPromptResponses(inputRaw, route, model)

	result, err := json.Marshal(body)
	if err != nil {
		return rawBody
	}
	return result
}
