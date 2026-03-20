package anthropic

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/protocol/openai"
)

var anthropicToChatAllowedExtraFields = map[string]struct{}{
	"max_tokens":     {},
	"metadata":       {},
	"stop_sequences": {},
	"temperature":    {},
	"tool_choice":    {},
	"top_p":          {},
}

type MessagesRequest struct {
	Model    string                     `json:"model"`
	Messages []messageParam             `json:"messages"`
	Tools    []toolParam                `json:"tools,omitempty"`
	Stream   bool                       `json:"stream,omitempty"`
	System   json.RawMessage            `json:"system,omitempty"`
	Extra    map[string]json.RawMessage `json:"-"`
}

type messageParam struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type toolParam struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

func (r *MessagesRequest) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["model"]; ok {
		if err := json.Unmarshal(v, &r.Model); err != nil {
			return err
		}
		delete(raw, "model")
	}
	if v, ok := raw["messages"]; ok {
		if err := json.Unmarshal(v, &r.Messages); err != nil {
			return err
		}
		delete(raw, "messages")
	}
	if v, ok := raw["tools"]; ok {
		if err := json.Unmarshal(v, &r.Tools); err != nil {
			return err
		}
		delete(raw, "tools")
	}
	if v, ok := raw["stream"]; ok {
		if err := json.Unmarshal(v, &r.Stream); err != nil {
			return err
		}
		delete(raw, "stream")
	}
	if v, ok := raw["system"]; ok {
		r.System = append(r.System[:0], v...)
		delete(raw, "system")
	}
	if len(raw) > 0 {
		r.Extra = raw
	}
	return nil
}

// MessagesRequestToChatRequest converts an Anthropic Messages request into an OpenAI Chat request.
// Only a controlled text + function-tools subset is supported.
func MessagesRequestToChatRequest(rawBody []byte) (openai.ChatCompletionRequest, error) {
	var req MessagesRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		return openai.ChatCompletionRequest{}, err
	}
	if strings.TrimSpace(req.Model) == "" {
		return openai.ChatCompletionRequest{}, fmt.Errorf("model is required")
	}
	if len(req.Messages) == 0 {
		return openai.ChatCompletionRequest{}, fmt.Errorf("messages is required")
	}

	chatReq := openai.ChatCompletionRequest{
		Model:  req.Model,
		Stream: req.Stream,
		Extra:  make(map[string]json.RawMessage),
	}

	systemPrompt, err := systemPromptFromRaw(req.System)
	if err != nil {
		return openai.ChatCompletionRequest{}, err
	}
	if systemPrompt != "" {
		chatReq.Messages = append(chatReq.Messages, openai.Message{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	for idx, msg := range req.Messages {
		converted, err := convertAnthropicMessageToChatMessages(idx, msg)
		if err != nil {
			return openai.ChatCompletionRequest{}, err
		}
		chatReq.Messages = append(chatReq.Messages, converted...)
	}

	for idx, tool := range req.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return openai.ChatCompletionRequest{}, fmt.Errorf("tools[%d].name is required", idx)
		}
		chatTool := openai.Tool{
			Type: "function",
			Function: openai.Function{
				Name:        tool.Name,
				Description: tool.Description,
			},
		}
		if len(tool.InputSchema) > 0 {
			var inputSchema any
			if err := json.Unmarshal(tool.InputSchema, &inputSchema); err != nil {
				return openai.ChatCompletionRequest{}, fmt.Errorf("tools[%d].input_schema: %w", idx, err)
			}
			chatTool.Function.Parameters = inputSchema
		}
		chatReq.Tools = append(chatReq.Tools, chatTool)
	}

	for key, raw := range req.Extra {
		if _, ok := anthropicToChatAllowedExtraFields[key]; !ok {
			return openai.ChatCompletionRequest{}, fmt.Errorf("messages field %q is not supported in anthropic_to_chat mode", key)
		}
		switch key {
		case "stop_sequences":
			if _, err := decodeStringArray(raw); err != nil {
				return openai.ChatCompletionRequest{}, fmt.Errorf("stop_sequences: %w", err)
			}
			chatReq.Extra["stop"] = raw
		case "tool_choice":
			toolChoice, err := convertAnthropicToolChoice(raw)
			if err != nil {
				return openai.ChatCompletionRequest{}, fmt.Errorf("tool_choice: %w", err)
			}
			chatReq.Extra["tool_choice"] = toolChoice
		default:
			chatReq.Extra[key] = raw
		}
	}

	return chatReq, nil
}

func systemPromptFromRaw(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	return contentBlocksToText(raw, "system")
}

func convertAnthropicMessageToChatMessages(idx int, msg messageParam) ([]openai.Message, error) {
	switch msg.Role {
	case "user":
		return convertAnthropicUserMessage(idx, msg.Content)
	case "assistant":
		return convertAnthropicAssistantMessage(idx, msg.Content)
	default:
		return nil, fmt.Errorf("messages[%d].role %q is not supported in anthropic_to_chat mode", idx, msg.Role)
	}
}

func convertAnthropicUserMessage(idx int, raw json.RawMessage) ([]openai.Message, error) {
	text, err := decodeOptionalString(raw)
	if err == nil {
		return []openai.Message{{Role: "user", Content: text}}, nil
	}

	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("messages[%d].content: unsupported user content shape", idx)
	}

	var textParts []string
	var toolResults []openai.Message
	for blockIdx, rawBlock := range blocks {
		blockType, err := contentBlockType(rawBlock)
		if err != nil {
			return nil, fmt.Errorf("messages[%d].content[%d]: %w", idx, blockIdx, err)
		}
		switch blockType {
		case "text":
			if len(toolResults) > 0 {
				return nil, fmt.Errorf("messages[%d].content mixes text and tool_result blocks, which is not supported in anthropic_to_chat mode", idx)
			}
			text, err := textFromBlock(rawBlock)
			if err != nil {
				return nil, fmt.Errorf("messages[%d].content[%d]: %w", idx, blockIdx, err)
			}
			textParts = append(textParts, text)
		case "tool_result":
			if len(textParts) > 0 {
				return nil, fmt.Errorf("messages[%d].content mixes text and tool_result blocks, which is not supported in anthropic_to_chat mode", idx)
			}
			toolMsg, err := toolResultToChatMessage(rawBlock)
			if err != nil {
				return nil, fmt.Errorf("messages[%d].content[%d]: %w", idx, blockIdx, err)
			}
			toolResults = append(toolResults, toolMsg)
		default:
			return nil, fmt.Errorf("messages[%d].content[%d].type %q is not supported in anthropic_to_chat mode", idx, blockIdx, blockType)
		}
	}

	if len(toolResults) > 0 {
		return toolResults, nil
	}
	return []openai.Message{{Role: "user", Content: strings.Join(textParts, "")}}, nil
}

func convertAnthropicAssistantMessage(idx int, raw json.RawMessage) ([]openai.Message, error) {
	text, err := decodeOptionalString(raw)
	if err == nil {
		return []openai.Message{{Role: "assistant", Content: text}}, nil
	}

	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("messages[%d].content: unsupported assistant content shape", idx)
	}

	msg := openai.Message{Role: "assistant"}
	var textParts []string
	for blockIdx, rawBlock := range blocks {
		blockType, err := contentBlockType(rawBlock)
		if err != nil {
			return nil, fmt.Errorf("messages[%d].content[%d]: %w", idx, blockIdx, err)
		}
		switch blockType {
		case "text":
			text, err := textFromBlock(rawBlock)
			if err != nil {
				return nil, fmt.Errorf("messages[%d].content[%d]: %w", idx, blockIdx, err)
			}
			textParts = append(textParts, text)
		case "tool_use":
			toolCall, err := toolUseToChatToolCall(rawBlock)
			if err != nil {
				return nil, fmt.Errorf("messages[%d].content[%d]: %w", idx, blockIdx, err)
			}
			msg.ToolCalls = append(msg.ToolCalls, toolCall)
		default:
			return nil, fmt.Errorf("messages[%d].content[%d].type %q is not supported in anthropic_to_chat mode", idx, blockIdx, blockType)
		}
	}
	if len(textParts) > 0 {
		msg.Content = strings.Join(textParts, "")
	}
	return []openai.Message{msg}, nil
}

func contentBlockType(raw json.RawMessage) (string, error) {
	var blockType struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &blockType); err != nil {
		return "", err
	}
	if blockType.Type == "" {
		return "", fmt.Errorf("type is required")
	}
	return blockType.Type, nil
}

func textFromBlock(raw json.RawMessage) (string, error) {
	var block struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &block); err != nil {
		return "", err
	}
	return block.Text, nil
}

func toolUseToChatToolCall(raw json.RawMessage) (openai.ToolCall, error) {
	var block struct {
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &block); err != nil {
		return openai.ToolCall{}, err
	}
	if strings.TrimSpace(block.ID) == "" {
		return openai.ToolCall{}, fmt.Errorf("tool_use.id is required")
	}
	if strings.TrimSpace(block.Name) == "" {
		return openai.ToolCall{}, fmt.Errorf("tool_use.name is required")
	}
	args := string(block.Input)
	if args == "" || args == "null" {
		args = "{}"
	}
	return openai.ToolCall{
		ID:   block.ID,
		Type: "function",
		Function: openai.FunctionCall{
			Name:      block.Name,
			Arguments: args,
		},
	}, nil
}

func toolResultToChatMessage(raw json.RawMessage) (openai.Message, error) {
	var block struct {
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
		IsError   *bool           `json:"is_error,omitempty"`
	}
	if err := json.Unmarshal(raw, &block); err != nil {
		return openai.Message{}, err
	}
	if strings.TrimSpace(block.ToolUseID) == "" {
		return openai.Message{}, fmt.Errorf("tool_result.tool_use_id is required")
	}
	if block.IsError != nil && *block.IsError {
		return openai.Message{}, fmt.Errorf("tool_result.is_error is not supported in anthropic_to_chat mode")
	}
	content, err := contentBlocksToText(block.Content, "tool_result.content")
	if err != nil {
		return openai.Message{}, err
	}
	return openai.Message{
		Role:       "tool",
		ToolCallID: block.ToolUseID,
		Content:    content,
	}, nil
}

func contentBlocksToText(raw json.RawMessage, field string) (string, error) {
	text, err := decodeOptionalString(raw)
	if err == nil {
		return text, nil
	}

	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", fmt.Errorf("%s: unsupported content shape", field)
	}

	var textParts []string
	for idx, rawBlock := range blocks {
		blockType, err := contentBlockType(rawBlock)
		if err != nil {
			return "", fmt.Errorf("%s[%d]: %w", field, idx, err)
		}
		if blockType != "text" {
			return "", fmt.Errorf("%s[%d].type %q is not supported in anthropic_to_chat mode", field, idx, blockType)
		}
		text, err := textFromBlock(rawBlock)
		if err != nil {
			return "", fmt.Errorf("%s[%d]: %w", field, idx, err)
		}
		textParts = append(textParts, text)
	}
	return strings.Join(textParts, "\n\n"), nil
}

func decodeOptionalString(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

func decodeStringArray(raw json.RawMessage) ([]string, error) {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func convertAnthropicToolChoice(raw json.RawMessage) (json.RawMessage, error) {
	var toolChoiceType string
	if err := json.Unmarshal(raw, &toolChoiceType); err == nil {
		switch toolChoiceType {
		case "auto":
			return json.RawMessage(`"auto"`), nil
		case "any":
			return json.RawMessage(`"required"`), nil
		default:
			return nil, fmt.Errorf("unsupported string value %q", toolChoiceType)
		}
	}

	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &choice); err != nil {
		return nil, fmt.Errorf("unsupported shape")
	}
	switch choice.Type {
	case "auto":
		return json.RawMessage(`"auto"`), nil
	case "any":
		return json.RawMessage(`"required"`), nil
	case "tool":
		if strings.TrimSpace(choice.Name) == "" {
			return nil, fmt.Errorf("tool name is required")
		}
		return json.Marshal(map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": choice.Name,
			},
		})
	default:
		return nil, fmt.Errorf("unsupported type %q", choice.Type)
	}
}

// ChatResponseToMessagesResponse converts an OpenAI Chat response into an Anthropic Messages response.
func ChatResponseToMessagesResponse(resp openai.ChatCompletionResponse) ([]byte, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("chat response contains no choices")
	}
	message := resp.Choices[0].Message
	content, err := chatMessageToAnthropicContent(message)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"id":          resp.ID,
		"type":        "message",
		"role":        "assistant",
		"model":       resp.Model,
		"content":     content,
		"stop_reason": chatFinishReasonToAnthropic(resp.Choices[0].FinishReason),
		"usage": map[string]any{
			"input_tokens":  resp.Usage.PromptTokens,
			"output_tokens": resp.Usage.CompletionTokens,
		},
	}
	return json.Marshal(result)
}

func chatMessageToAnthropicContent(msg openai.Message) ([]map[string]any, error) {
	var content []map[string]any
	if msg.Content != nil {
		text, err := chatContentToText(msg.Content)
		if err != nil {
			return nil, err
		}
		if text != "" {
			content = append(content, map[string]any{
				"type": "text",
				"text": text,
			})
		}
	}
	for _, toolCall := range msg.ToolCalls {
		input := map[string]any{}
		if strings.TrimSpace(toolCall.Function.Arguments) != "" {
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &input); err != nil {
				input = map[string]any{}
			}
		}
		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    toolCall.ID,
			"name":  toolCall.Function.Name,
			"input": input,
		})
	}
	return content, nil
}

func chatContentToText(content any) (string, error) {
	switch v := content.(type) {
	case string:
		return v, nil
	case []any:
		var textParts []string
		for idx, part := range v {
			partMap, ok := part.(map[string]any)
			if !ok {
				return "", fmt.Errorf("content[%d] is not an object", idx)
			}
			partType, _ := partMap["type"].(string)
			if partType != "" && partType != "text" {
				return "", fmt.Errorf("content[%d].type %q is not supported in anthropic_to_chat mode", idx, partType)
			}
			text, _ := partMap["text"].(string)
			textParts = append(textParts, text)
		}
		return strings.Join(textParts, ""), nil
	default:
		return "", fmt.Errorf("assistant content type %T is not supported in anthropic_to_chat mode", content)
	}
}

func chatFinishReasonToAnthropic(reason string) string {
	switch reason {
	case "", "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return reason
	}
}

type chatStreamChunk struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage,omitempty"`
	Choices []struct {
		Delta struct {
			Role      string `json:"role,omitempty"`
			Content   string `json:"content,omitempty"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id,omitempty"`
				Type     string `json:"type,omitempty"`
				Function struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices"`
}

type ChatToMessagesStreamState struct {
	messageStarted   bool
	messageID        string
	model            string
	promptTokens     int64
	completionTokens int64
	textStarted      bool
	toolStarted      map[int]bool
	toolOffset       int
	finishReason     string
}

func NewChatToMessagesStreamState() *ChatToMessagesStreamState {
	return &ChatToMessagesStreamState{
		toolStarted: make(map[int]bool),
		toolOffset:  -1,
	}
}

func (s *ChatToMessagesStreamState) ConvertEvent(evt protocol.Event) ([]byte, error) {
	if evt.Data == "" || evt.Data == "[DONE]" {
		return nil, nil
	}

	var chunk chatStreamChunk
	if err := json.Unmarshal([]byte(evt.Data), &chunk); err != nil {
		return nil, nil
	}
	if len(chunk.Choices) == 0 {
		return nil, nil
	}

	if s.messageID == "" {
		s.messageID = chunk.ID
	}
	if s.model == "" {
		s.model = chunk.Model
	}
	if chunk.Usage.PromptTokens > 0 {
		s.promptTokens = chunk.Usage.PromptTokens
	}
	if chunk.Usage.CompletionTokens > 0 {
		s.completionTokens = chunk.Usage.CompletionTokens
	}
	if chunk.Choices[0].FinishReason != "" {
		s.finishReason = chunk.Choices[0].FinishReason
	}

	var out []protocol.Event
	if !s.messageStarted {
		s.messageStarted = true
		out = append(out, protocol.Event{
			EventType: "message_start",
			Data: mustJSON(map[string]any{
				"type": "message_start",
				"message": map[string]any{
					"id":            s.messageID,
					"type":          "message",
					"role":          "assistant",
					"model":         s.model,
					"content":       []any{},
					"stop_reason":   nil,
					"stop_sequence": nil,
					"usage": map[string]any{
						"input_tokens":  s.promptTokens,
						"output_tokens": int64(0),
					},
				},
			}),
			HasData: true,
		})
	}

	out = append(out, s.convertChunk(chunk)...)
	return protocol.ReplayEvents(out), nil
}

func (s *ChatToMessagesStreamState) convertChunk(chunk chatStreamChunk) []protocol.Event {
	choice := chunk.Choices[0]
	var out []protocol.Event

	if choice.Delta.Content != "" {
		if !s.textStarted {
			s.textStarted = true
			if s.toolOffset < 0 {
				s.toolOffset = 1
			}
			out = append(out, protocol.Event{
				EventType: "content_block_start",
				Data: mustJSON(map[string]any{
					"type":  "content_block_start",
					"index": 0,
					"content_block": map[string]any{
						"type": "text",
						"text": "",
					},
				}),
				HasData: true,
			})
		}
		out = append(out, protocol.Event{
			EventType: "content_block_delta",
			Data: mustJSON(map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{
					"type": "text_delta",
					"text": choice.Delta.Content,
				},
			}),
			HasData: true,
		})
	}

	if s.toolOffset < 0 {
		s.toolOffset = 0
	}
	for _, toolCall := range choice.Delta.ToolCalls {
		index := toolCall.Index + s.toolOffset
		if !s.toolStarted[index] {
			s.toolStarted[index] = true
			out = append(out, protocol.Event{
				EventType: "content_block_start",
				Data: mustJSON(map[string]any{
					"type":  "content_block_start",
					"index": index,
					"content_block": map[string]any{
						"type":  "tool_use",
						"id":    toolCall.ID,
						"name":  toolCall.Function.Name,
						"input": map[string]any{},
					},
				}),
				HasData: true,
			})
		}
		if toolCall.Function.Arguments != "" {
			out = append(out, protocol.Event{
				EventType: "content_block_delta",
				Data: mustJSON(map[string]any{
					"type":  "content_block_delta",
					"index": index,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": toolCall.Function.Arguments,
					},
				}),
				HasData: true,
			})
		}
	}

	return out
}

func (s *ChatToMessagesStreamState) Finalize() ([]byte, error) {
	if !s.messageStarted {
		return nil, fmt.Errorf("chat stream does not contain any chunks")
	}

	var out []protocol.Event
	if s.textStarted {
		out = append(out, protocol.Event{
			EventType: "content_block_stop",
			Data: mustJSON(map[string]any{
				"type":  "content_block_stop",
				"index": 0,
			}),
			HasData: true,
		})
	}

	toolIndexes := make([]int, 0, len(s.toolStarted))
	for index := range s.toolStarted {
		toolIndexes = append(toolIndexes, index)
	}
	slices.Sort(toolIndexes)
	for _, index := range toolIndexes {
		out = append(out, protocol.Event{
			EventType: "content_block_stop",
			Data: mustJSON(map[string]any{
				"type":  "content_block_stop",
				"index": index,
			}),
			HasData: true,
		})
	}

	out = append(out, protocol.Event{
		EventType: "message_delta",
		Data: mustJSON(map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   chatFinishReasonToAnthropic(s.finishReason),
				"stop_sequence": nil,
			},
			"usage": map[string]any{
				"output_tokens": s.completionTokens,
			},
		}),
		HasData: true,
	})
	out = append(out, protocol.Event{
		EventType: "message_stop",
		Data:      mustJSON(map[string]any{"type": "message_stop"}),
		HasData:   true,
	})
	return protocol.ReplayEvents(out), nil
}

// ConvertChatStreamToAnthropic converts OpenAI Chat SSE bytes into Anthropic Messages SSE bytes.
func ConvertChatStreamToAnthropic(rawSSE []byte) ([]byte, error) {
	events := protocol.ParseEvents(rawSSE)
	state := NewChatToMessagesStreamState()
	var out []byte
	streamComplete := false
	for _, evt := range events {
		if evt.Data == "[DONE]" {
			streamComplete = true
			continue
		}
		converted, err := state.ConvertEvent(evt)
		if err != nil {
			return nil, err
		}
		out = append(out, converted...)
	}
	if !streamComplete {
		return nil, fmt.Errorf("chat stream terminated before [DONE]")
	}
	final, err := state.Finalize()
	if err != nil {
		return nil, err
	}
	out = append(out, final...)
	return out, nil
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}
