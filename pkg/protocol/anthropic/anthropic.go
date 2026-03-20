package anthropic

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/wweir/warden/pkg/protocol/openai"
)

// Endpoint is the Anthropic Messages API endpoint.
const Endpoint = "/messages"

// MarshalRequest converts an OpenAI ChatCompletionRequest to Anthropic Messages API format.
//
// Key conversions:
//   - system messages extracted to top-level "system" field
//   - tool_calls in assistant messages → content blocks with type "tool_use"
//   - role "tool" messages → role "user" with "tool_result" content blocks (merged if consecutive)
//   - tools: function.parameters → input_schema, flattened structure
//   - max_tokens: required by Anthropic, defaults to 4096
//   - OpenAI "stop" → Anthropic "stop_sequences"
func MarshalRequest(req openai.ChatCompletionRequest) ([]byte, error) {
	result := make(map[string]any)
	result["model"] = req.Model

	// extract system messages to top-level field
	var systemParts []string
	var nonSystemMsgs []openai.Message
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			if s, ok := msg.Content.(string); ok {
				systemParts = append(systemParts, s)
			}
		} else {
			nonSystemMsgs = append(nonSystemMsgs, msg)
		}
	}
	if len(systemParts) > 0 {
		result["system"] = strings.Join(systemParts, "\n\n")
	}

	result["messages"] = convertMessages(nonSystemMsgs)

	// convert tools: OpenAI function wrapper → Anthropic flat format
	if len(req.Tools) > 0 {
		var anthTools []map[string]any
		for _, tool := range req.Tools {
			t := map[string]any{"name": tool.Function.Name}
			if tool.Function.Description != "" {
				t["description"] = tool.Function.Description
			}
			if tool.Function.Parameters != nil {
				t["input_schema"] = tool.Function.Parameters
			}
			anthTools = append(anthTools, t)
		}
		result["tools"] = anthTools
	}

	// max_tokens: required by Anthropic, check Extra or default to 4096
	maxTokensSet := false
	if req.Extra != nil {
		if v, ok := req.Extra["max_tokens"]; ok {
			var mt int
			if json.Unmarshal(v, &mt) == nil && mt > 0 {
				result["max_tokens"] = mt
				maxTokensSet = true
			}
		}
	}
	if !maxTokensSet {
		result["max_tokens"] = 4096
	}

	if req.Stream {
		result["stream"] = true
	}

	// passthrough compatible Extra fields
	for k, v := range req.Extra {
		switch k {
		case "max_tokens", "stream", "model", "messages", "tools":
			// already handled
		case "temperature", "top_p", "top_k", "metadata":
			var val any
			if json.Unmarshal(v, &val) == nil {
				result[k] = val
			}
		case "stop":
			// OpenAI "stop" (string or []string) → Anthropic "stop_sequences" ([]string)
			var sequences []string
			if json.Unmarshal(v, &sequences) == nil {
				result["stop_sequences"] = sequences
			} else {
				var single string
				if json.Unmarshal(v, &single) == nil {
					result["stop_sequences"] = []string{single}
				}
			}
		}
	}

	return json.Marshal(result)
}

// UnmarshalResponse converts an Anthropic Messages API response to OpenAI format.
func UnmarshalResponse(body []byte) (openai.ChatCompletionResponse, error) {
	var anthResp struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text,omitempty"`
			ID    string          `json:"id,omitempty"`
			Name  string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &anthResp); err != nil {
		return openai.ChatCompletionResponse{}, err
	}

	var textParts []string
	var toolCalls []openai.ToolCall
	for _, block := range anthResp.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			args := string(block.Input)
			if args == "" || args == "null" {
				args = "{}"
			}
			toolCalls = append(toolCalls, openai.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: openai.FunctionCall{
					Name:      block.Name,
					Arguments: args,
				},
			})
		}
	}

	msg := openai.Message{Role: "assistant"}
	if len(textParts) > 0 {
		msg.Content = strings.Join(textParts, "")
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	return openai.ChatCompletionResponse{
		ID:      anthResp.ID,
		Object:  "chat.completion",
		Model:   anthResp.Model,
		Created: time.Now().Unix(),
		Choices: []openai.Choice{
			{Index: 0, Message: msg, FinishReason: MapStopReason(anthResp.StopReason)},
		},
		Usage: openai.Usage{
			PromptTokens:     anthResp.Usage.InputTokens,
			CompletionTokens: anthResp.Usage.OutputTokens,
			TotalTokens:      anthResp.Usage.InputTokens + anthResp.Usage.OutputTokens,
		},
	}, nil
}

// MapStopReason converts Anthropic stop_reason to OpenAI finish_reason.
func MapStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	default:
		return reason
	}
}

// convertMessages converts OpenAI messages to Anthropic message format.
// Consecutive tool messages are merged into a single user message with tool_result blocks.
func convertMessages(msgs []openai.Message) []map[string]any {
	var result []map[string]any
	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]
		switch msg.Role {
		case "user":
			result = append(result, map[string]any{
				"role":    "user",
				"content": msg.Content,
			})

		case "assistant":
			if len(msg.ToolCalls) == 0 {
				result = append(result, map[string]any{
					"role":    "assistant",
					"content": msg.Content,
				})
			} else {
				var blocks []map[string]any
				if s, ok := msg.Content.(string); ok && s != "" {
					blocks = append(blocks, map[string]any{
						"type": "text",
						"text": s,
					})
				}
				for _, tc := range msg.ToolCalls {
					var input any
					if json.Unmarshal([]byte(tc.Function.Arguments), &input) != nil {
						input = map[string]any{}
					}
					blocks = append(blocks, map[string]any{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Function.Name,
						"input": input,
					})
				}
				result = append(result, map[string]any{
					"role":    "assistant",
					"content": blocks,
				})
			}

		case "tool":
			// Anthropic requires tool_result blocks in a single "user" message.
			// Merge consecutive tool messages.
			var toolResults []map[string]any
			for ; i < len(msgs) && msgs[i].Role == "tool"; i++ {
				toolResults = append(toolResults, map[string]any{
					"type":        "tool_result",
					"tool_use_id": msgs[i].ToolCallID,
					"content":     msgs[i].Content,
				})
			}
			i-- // adjust for outer loop increment
			result = append(result, map[string]any{
				"role":    "user",
				"content": toolResults,
			})
		}
	}
	return result
}
