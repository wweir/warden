package bridge

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/wweir/warden/pkg/protocol"
)

// AnthropicToChatStreamState converts Anthropic Messages API SSE events into
// OpenAI Chat Completions SSE events incrementally. It is used by
// StreamAnthropicAsResponses to avoid buffering the entire upstream body.
type AnthropicToChatStreamState struct {
	msgID     string
	model     string
	created   int64
	toolCalls map[int]map[string]any
	usage     struct {
		InputTokens  int64
		OutputTokens int64
	}
	messageStarted bool
}

func NewAnthropicToChatStreamState() *AnthropicToChatStreamState {
	return &AnthropicToChatStreamState{
		created:   time.Now().Unix(),
		toolCalls: make(map[int]map[string]any),
	}
}

// ConvertEvent transforms a single Anthropic event into zero or more Chat
// completion chunk events (as raw SSE bytes).
func (s *AnthropicToChatStreamState) ConvertEvent(evt protocol.Event) ([]byte, error) {
	if evt.Data == "" {
		return nil, nil
	}

	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(evt.Data), &base); err != nil {
		return nil, fmt.Errorf("unmarshal anthropic event type: %w", err)
	}

	switch base.Type {
	case "message_start":
		return s.handleMessageStart(evt.Data)
	case "content_block_start":
		return s.handleContentBlockStart(evt.Data)
	case "content_block_delta":
		return s.handleContentBlockDelta(evt.Data)
	case "message_delta":
		return s.handleMessageDelta(evt.Data)
	case "content_block_stop", "message_stop":
		// No direct Chat equivalent for these events.
		return nil, nil
	default:
		return nil, nil
	}
}

func (s *AnthropicToChatStreamState) handleMessageStart(data string) ([]byte, error) {
	var msg struct {
		Message struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage struct {
				InputTokens  int64 `json:"input_tokens"`
				OutputTokens int64 `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return nil, fmt.Errorf("unmarshal message_start: %w", err)
	}
	s.msgID = msg.Message.ID
	s.model = msg.Message.Model
	s.usage.InputTokens = msg.Message.Usage.InputTokens
	s.usage.OutputTokens = msg.Message.Usage.OutputTokens
	s.messageStarted = true

	return s.emitChatChunk(map[string]any{"role": "assistant"}, nil, nil), nil
}

func (s *AnthropicToChatStreamState) handleContentBlockStart(data string) ([]byte, error) {
	var msg struct {
		Index        int `json:"index"`
		ContentBlock struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"content_block"`
	}
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return nil, fmt.Errorf("unmarshal content_block_start: %w", err)
	}
	if msg.ContentBlock.Type != "tool_use" {
		return nil, nil
	}

	toolCall := map[string]any{
		"index": msg.Index,
		"id":    msg.ContentBlock.ID,
		"type":  "function",
		"function": map[string]any{
			"name":      msg.ContentBlock.Name,
			"arguments": "",
		},
	}
	s.toolCalls[msg.Index] = toolCall
	return s.emitChatChunk(map[string]any{"tool_calls": []any{toolCall}}, nil, nil), nil
}

func (s *AnthropicToChatStreamState) handleContentBlockDelta(data string) ([]byte, error) {
	var msg struct {
		Index int `json:"index"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Thinking    string `json:"thinking"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return nil, fmt.Errorf("unmarshal content_block_delta: %w", err)
	}

	switch msg.Delta.Type {
	case "text_delta":
		if msg.Delta.Text == "" {
			return nil, nil
		}
		return s.emitChatChunk(map[string]any{"content": msg.Delta.Text}, nil, nil), nil
	case "thinking_delta":
		if msg.Delta.Thinking == "" {
			return nil, nil
		}
		return s.emitChatChunk(map[string]any{"reasoning_content": msg.Delta.Thinking}, nil, nil), nil
	case "input_json_delta":
		toolCall, ok := s.toolCalls[msg.Index]
		if !ok {
			toolCall = map[string]any{
				"index": msg.Index,
				"type":  "function",
				"function": map[string]any{
					"arguments": "",
				},
			}
			s.toolCalls[msg.Index] = toolCall
		}
		fn := toolCall["function"].(map[string]any)
		prev, _ := fn["arguments"].(string)
		fn["arguments"] = prev + msg.Delta.PartialJSON
		return s.emitChatChunk(map[string]any{"tool_calls": []any{map[string]any{
			"index":    msg.Index,
			"function": map[string]any{"arguments": msg.Delta.PartialJSON},
		}}}, nil, nil), nil
	default:
		return nil, nil
	}
}

func (s *AnthropicToChatStreamState) handleMessageDelta(data string) ([]byte, error) {
	var msg struct {
		Delta struct {
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Usage struct {
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return nil, fmt.Errorf("unmarshal message_delta: %w", err)
	}

	if msg.Usage.OutputTokens > 0 {
		s.usage.OutputTokens = msg.Usage.OutputTokens
	}

	var finishReasonPtr *string
	if msg.Delta.StopReason != "" {
		fr := mapStopReason(msg.Delta.StopReason)
		finishReasonPtr = &fr
	}
	extra := map[string]any{}
	if s.usage.InputTokens > 0 || s.usage.OutputTokens > 0 {
		extra["usage"] = map[string]any{
			"prompt_tokens":     s.usage.InputTokens,
			"completion_tokens": s.usage.OutputTokens,
			"total_tokens":      s.usage.InputTokens + s.usage.OutputTokens,
		}
	}
	return s.emitChatChunk(map[string]any{}, finishReasonPtr, extra), nil
}

func mapStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	default:
		return reason
	}
}

func (s *AnthropicToChatStreamState) emitChatChunk(delta map[string]any, finishReason *string, extra map[string]any) []byte {
	chunk := map[string]any{
		"id":      s.msgID,
		"object":  "chat.completion.chunk",
		"created": s.created,
		"model":   s.model,
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         delta,
				"finish_reason": finishReason,
			},
		},
	}
	for k, v := range extra {
		chunk[k] = v
	}
	data, _ := json.Marshal(chunk)
	return []byte("data: " + string(data) + "\n\n")
}

// Finalize emits the [DONE] marker to signal stream completion.
func (s *AnthropicToChatStreamState) Finalize() []byte {
	return []byte("data: [DONE]\n\n")
}

// MessageStarted reports whether a message_start event has been seen.
func (s *AnthropicToChatStreamState) MessageStarted() bool {
	return s.messageStarted
}
