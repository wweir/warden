package anthropic

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/wweir/warden/pkg/protocol"
)

// StreamParser parses SSE events from Anthropic Messages API streaming responses.
type StreamParser struct{}

func (p *StreamParser) Parse(events []protocol.Event) ([]protocol.ToolCallInfo, error) {
	type toolBlock struct {
		ID        string
		Name      string
		Arguments string
	}

	blocks := make(map[int]*toolBlock)

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
			continue
		}
	}

	if len(blocks) == 0 {
		return nil, nil
	}

	var infos []protocol.ToolCallInfo
	for i := range len(blocks) {
		block, ok := blocks[i]
		if !ok {
			continue
		}
		infos = append(infos, protocol.ToolCallInfo{
			ID:        block.ID,
			Name:      block.Name,
			Arguments: block.Arguments,
		})
	}

	return infos, nil
}

// ConvertStreamToOpenAI converts Anthropic Messages API SSE bytes to OpenAI
// Chat Completions SSE format so that clients expecting OpenAI streaming can
// parse the response correctly.
func ConvertStreamToOpenAI(rawSSE []byte) []byte {
	events := protocol.ParseEvents(rawSSE)

	var (
		msgID   string
		model   string
		created = time.Now().Unix()
		buf     []byte
		usage   struct {
			InputTokens  int64 `json:"input_tokens,omitempty"`
			OutputTokens int64 `json:"output_tokens,omitempty"`
		}
		toolCalls = map[int]map[string]any{}
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
					Usage struct {
						InputTokens  int64 `json:"input_tokens"`
						OutputTokens int64 `json:"output_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if json.Unmarshal([]byte(evt.Data), &msg) != nil {
				continue
			}
			msgID = msg.Message.ID
			model = msg.Message.Model
			usage.InputTokens = msg.Message.Usage.InputTokens
			usage.OutputTokens = msg.Message.Usage.OutputTokens

			// emit initial chunk with role
			buf = appendOpenAIChunk(buf, msgID, model, created,
				map[string]any{"role": "assistant"}, nil, nil)

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
			if json.Unmarshal([]byte(evt.Data), &msg) != nil {
				continue
			}
			if msg.ContentBlock.Type != "tool_use" {
				continue
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
			if input, ok := normalizeToolUseInput(msg.ContentBlock.Input); ok {
				toolCall["initial_input"] = input
			}
			toolCalls[msg.Index] = toolCall
			emitToolCall := map[string]any{
				"index": msg.Index,
				"id":    msg.ContentBlock.ID,
				"type":  "function",
				"function": map[string]any{
					"name":      msg.ContentBlock.Name,
					"arguments": "",
				},
			}
			buf = appendOpenAIChunk(buf, msgID, model, created,
				map[string]any{"tool_calls": []any{emitToolCall}}, nil, nil)

		case "content_block_delta":
			var msg struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if json.Unmarshal([]byte(evt.Data), &msg) != nil {
				continue
			}
			if msg.Delta.Type == "text_delta" && msg.Delta.Text != "" {
				buf = appendOpenAIChunk(buf, msgID, model, created,
					map[string]any{"content": msg.Delta.Text}, nil, nil)
				continue
			}
			if msg.Delta.Type == "input_json_delta" && msg.Delta.PartialJSON != "" {
				toolCall, ok := toolCalls[msg.Index]
				if !ok {
					toolCall = map[string]any{
						"index": msg.Index,
						"type":  "function",
						"function": map[string]any{
							"arguments": "",
						},
					}
					toolCalls[msg.Index] = toolCall
				}
				function := toolCall["function"].(map[string]any)
				prev, _ := function["arguments"].(string)
				function["arguments"] = prev + msg.Delta.PartialJSON
				delete(toolCall, "initial_input")
				buf = appendOpenAIChunk(buf, msgID, model, created,
					map[string]any{"tool_calls": []any{map[string]any{
						"index": msg.Index,
						"function": map[string]any{
							"arguments": msg.Delta.PartialJSON,
						},
					}}}, nil, nil)
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
			for index, toolCall := range toolCalls {
				initialInput, ok := toolCall["initial_input"].(string)
				if !ok || initialInput == "" {
					continue
				}
				toolCall["function"].(map[string]any)["arguments"] = initialInput
				delete(toolCall, "initial_input")
				buf = appendOpenAIChunk(buf, msgID, model, created,
					map[string]any{"tool_calls": []any{map[string]any{
						"index": index,
						"function": map[string]any{
							"arguments": initialInput,
						},
					}}}, nil, nil)
			}
			finishReason := MapStopReason(msg.Delta.StopReason)
			if msg.Usage.OutputTokens > 0 {
				usage.OutputTokens = msg.Usage.OutputTokens
			}
			extra := map[string]any{}
			if usage.InputTokens > 0 || usage.OutputTokens > 0 {
				extra["usage"] = map[string]any{
					"prompt_tokens":     usage.InputTokens,
					"completion_tokens": usage.OutputTokens,
					"total_tokens":      usage.InputTokens + usage.OutputTokens,
				}
			}
			buf = appendOpenAIChunk(buf, msgID, model, created,
				map[string]any{}, &finishReason, extra)
		}
	}

	buf = append(buf, []byte("data: [DONE]\n\n")...)
	return buf
}

func normalizeToolUseInput(input any) (string, bool) {
	if input == nil {
		return "", false
	}
	switch v := input.(type) {
	case string:
		return v, v != ""
	case json.RawMessage:
		return string(v), len(v) > 0
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(raw), true
	}
}

func appendOpenAIChunk(buf []byte, id, model string, created int64, delta map[string]any, finishReason *string, extra map[string]any) []byte {
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
	for k, v := range extra {
		chunk[k] = v
	}
	data, _ := json.Marshal(chunk)
	return append(buf, []byte(fmt.Sprintf("data: %s\n\n", data))...)
}

// AssembleStream merges Anthropic Messages API SSE events into a single JSON
// object equivalent to a non-streaming response. Uses map[string]any throughout
// to tolerate non-standard or extended protocol implementations.
//
// Strategy:
//   - message_start: take the nested "message" object as the base
//   - content_block_start/delta/stop: accumulate text and tool_use blocks by index
//   - message_delta: merge delta fields (stop_reason etc.) and update usage
//   - all other event data is ignored
//
// Returns nil if no meaningful data could be extracted.
func AssembleStream(rawSSE []byte) []byte {
	events := protocol.ParseEvents(rawSSE)

	var base map[string]any
	// index -> content block being assembled
	type block struct {
		data map[string]any
		text string // accumulated text for text blocks
	}
	blocks := make(map[int]*block)

	for _, evt := range events {
		if evt.Data == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(evt.Data), &msg); err != nil {
			continue
		}

		switch msg["type"] {
		case "message_start":
			if m, ok := msg["message"].(map[string]any); ok {
				base = m
				// content will be rebuilt from deltas
				base["content"] = []any{}
			}

		case "content_block_start":
			idx := int(asFloat64(msg["index"]))
			cb, _ := msg["content_block"].(map[string]any)
			if cb == nil {
				cb = map[string]any{}
			}
			// clone to avoid mutating parsed data
			cloned := make(map[string]any, len(cb))
			for k, v := range cb {
				cloned[k] = v
			}
			blocks[idx] = &block{data: cloned}

		case "content_block_delta":
			idx := int(asFloat64(msg["index"]))
			b := blocks[idx]
			if b == nil {
				break
			}
			delta, _ := msg["delta"].(map[string]any)
			if delta == nil {
				break
			}
			switch delta["type"] {
			case "text_delta":
				if t, ok := delta["text"].(string); ok {
					b.text += t
				}
			case "input_json_delta":
				if t, ok := delta["partial_json"].(string); ok {
					prev, _ := b.data["input"].(string)
					b.data["input"] = prev + t
				}
			}

		case "content_block_stop":
			// finalize the block

		case "message_delta":
			if base == nil {
				break
			}
			delta, _ := msg["delta"].(map[string]any)
			for k, v := range delta {
				base[k] = v
			}
			// merge usage
			if u, ok := msg["usage"].(map[string]any); ok {
				existing, _ := base["usage"].(map[string]any)
				if existing == nil {
					existing = map[string]any{}
				}
				for k, v := range u {
					existing[k] = v
				}
				base["usage"] = existing
			}
		}
	}

	if base == nil {
		return nil
	}

	// build content array in index order
	var content []any
	for i := range len(blocks) {
		b, ok := blocks[i]
		if !ok {
			continue
		}
		switch b.data["type"] {
		case "text":
			b.data["text"] = b.text
		case "tool_use":
			// parse accumulated JSON string into object for readability
			var parsed any
			if s, ok := b.data["input"].(string); ok && s != "" {
				if json.Unmarshal([]byte(s), &parsed) == nil {
					b.data["input"] = parsed
				}
			}
		}
		content = append(content, b.data)
	}
	base["content"] = content

	out, err := json.Marshal(base)
	if err != nil {
		return nil
	}
	return out
}

func asFloat64(v any) float64 {
	f, _ := v.(float64)
	return f
}
