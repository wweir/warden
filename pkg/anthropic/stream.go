package anthropic

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/wweir/warden/pkg/sse"
)

// StreamParser parses SSE events from Anthropic Messages API streaming responses.
type StreamParser struct{}

func (p *StreamParser) Parse(events []sse.Event, injectedTools []string) ([]sse.ToolCallInfo, bool, error) {
	type toolBlock struct {
		ID        string
		Name      string
		Arguments string
	}

	blocks := make(map[int]*toolBlock)
	var stopReason string

	for _, evt := range events {
		if evt.Data == "" {
			continue
		}

		var baseMsg struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &baseMsg); err != nil {
			continue
		}

		switch baseMsg.Type {
		case "content_block_start":
			var msg struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type  string `json:"type"`
					ID    string `json:"id"`
					Name  string `json:"name"`
					Input any    `json:"input"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &msg); err != nil {
				continue
			}
			if msg.ContentBlock.Type == "tool_use" {
				blocks[msg.Index] = &toolBlock{
					ID:   msg.ContentBlock.ID,
					Name: msg.ContentBlock.Name,
				}
			}

		case "content_block_delta":
			var msg struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &msg); err != nil {
				continue
			}
			if msg.Delta.Type == "input_json_delta" {
				if block, ok := blocks[msg.Index]; ok {
					block.Arguments += msg.Delta.PartialJSON
				}
			}

		case "message_delta":
			var msg struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &msg); err != nil {
				continue
			}
			if msg.Delta.StopReason != "" {
				stopReason = msg.Delta.StopReason
			}
		}
	}

	if stopReason != "tool_use" || len(blocks) == 0 {
		return nil, false, nil
	}

	var infos []sse.ToolCallInfo
	hasInjected := false
	for i := range len(blocks) {
		block, ok := blocks[i]
		if !ok {
			continue
		}
		infos = append(infos, sse.ToolCallInfo{
			ID:        block.ID,
			Name:      block.Name,
			Arguments: block.Arguments,
		})
		if slices.Contains(injectedTools, block.Name) {
			hasInjected = true
		}
	}

	return infos, hasInjected, nil
}

func (p *StreamParser) Filter(events []sse.Event, injectedTools []string) []sse.Event {
	injectedIndices := make(map[int]bool)

	for _, evt := range events {
		var msg struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &msg); err != nil {
			continue
		}
		if msg.Type == "content_block_start" && msg.ContentBlock.Type == "tool_use" {
			if slices.Contains(injectedTools, msg.ContentBlock.Name) {
				injectedIndices[msg.Index] = true
			}
		}
	}

	var filtered []sse.Event
	for _, evt := range events {
		var msg struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &msg); err != nil {
			filtered = append(filtered, evt)
			continue
		}

		switch msg.Type {
		case "content_block_start", "content_block_delta", "content_block_stop":
			if injectedIndices[msg.Index] {
				continue
			}
		}

		filtered = append(filtered, evt)
	}

	return filtered
}

// ConvertStreamToOpenAI converts Anthropic Messages API SSE bytes to OpenAI
// Chat Completions SSE format so that clients expecting OpenAI streaming can
// parse the response correctly.
func ConvertStreamToOpenAI(rawSSE []byte) []byte {
	events := sse.ParseEvents(rawSSE)

	var (
		msgID   string
		model   string
		created = time.Now().Unix()
		buf     []byte
	)

	for _, evt := range events {
		if evt.Data == "" {
			continue
		}

		var base struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(evt.Data), &base) != nil {
			continue
		}

		switch base.Type {
		case "message_start":
			var msg struct {
				Message struct {
					ID    string `json:"id"`
					Model string `json:"model"`
				} `json:"message"`
			}
			if json.Unmarshal([]byte(evt.Data), &msg) != nil {
				continue
			}
			msgID = msg.Message.ID
			model = msg.Message.Model

			// emit initial chunk with role
			buf = appendOpenAIChunk(buf, msgID, model, created,
				map[string]any{"role": "assistant"}, nil)

		case "content_block_delta":
			var msg struct {
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			if json.Unmarshal([]byte(evt.Data), &msg) != nil {
				continue
			}
			if msg.Delta.Type == "text_delta" && msg.Delta.Text != "" {
				buf = appendOpenAIChunk(buf, msgID, model, created,
					map[string]any{"content": msg.Delta.Text}, nil)
			}

		case "message_delta":
			var msg struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int64 `json:"output_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal([]byte(evt.Data), &msg) != nil {
				continue
			}
			finishReason := MapStopReason(msg.Delta.StopReason)
			buf = appendOpenAIChunk(buf, msgID, model, created,
				map[string]any{}, &finishReason)
		}
	}

	buf = append(buf, []byte("data: [DONE]\n\n")...)
	return buf
}

func appendOpenAIChunk(buf []byte, id, model string, created int64, delta map[string]any, finishReason *string) []byte {
	chunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         delta,
				"finish_reason": finishReason,
			},
		},
	}
	data, _ := json.Marshal(chunk)
	return append(buf, []byte(fmt.Sprintf("data: %s\n\n", data))...)
}
