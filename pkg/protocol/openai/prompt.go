package openai

import "encoding/json"

// InjectSystemPromptRaw prepends a system message into raw JSON request body
// for Chat Completion requests. It's a no-op if prompt is empty,
// or if the messages already start with the same system prompt.
func InjectSystemPromptRaw(rawBody []byte, prompt string) []byte {
	if prompt == "" {
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

// InjectSystemPromptResponses prepends a system message into a Responses API input.
// It's a no-op if prompt is empty, or if the input already starts with the same system message.
func InjectSystemPromptResponses(input json.RawMessage, prompt string) json.RawMessage {
	if prompt == "" {
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

// InjectSystemPromptResponsesRaw prepends a developer message into raw JSON request body
// for Responses API requests. It's a no-op if prompt is empty.
func InjectSystemPromptResponsesRaw(rawBody []byte, prompt string) []byte {
	if prompt == "" {
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

	body["input"] = InjectSystemPromptResponses(inputRaw, prompt)

	result, err := json.Marshal(body)
	if err != nil {
		return rawBody
	}
	return result
}
