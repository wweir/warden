package anthropic

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/protocol/openai"
)

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
		return nil, fmt.Errorf("unmarshal chat stream chunk: %w", err)
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

	chunkEvents, err := s.convertChunk(chunk)
	if err != nil {
		return nil, err
	}
	out = append(out, chunkEvents...)
	return protocol.ReplayEvents(out), nil
}

func (s *ChatToMessagesStreamState) convertChunk(chunk chatStreamChunk) ([]protocol.Event, error) {
	choice := chunk.Choices[0]
	var out []protocol.Event

	if choice.Delta.Content != "" {
		if !s.textStarted {
			s.textStarted = true
			if s.toolOffset < 0 {
				s.toolOffset = 1
			} else if s.toolOffset == 0 {
				// tool calls already appeared before text content,
				// which violates the expected OpenAI streaming order.
				return nil, fmt.Errorf("text content appeared after tool calls: unsupported streaming order")
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

	if len(choice.Delta.ToolCalls) > 0 && s.toolOffset < 0 {
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

	return out, nil
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
