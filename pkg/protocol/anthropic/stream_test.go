package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wweir/warden/pkg/protocol"
)

func TestConvertStreamToOpenAI(t *testing.T) {
	// Simulate a typical Anthropic streaming response
	anthropicSSE := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_abc123","type":"message","role":"assistant","model":"claude-3-opus-20240229","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	result := ConvertStreamToOpenAI([]byte(anthropicSSE))
	resultStr := string(result)

	// Should end with [DONE]
	if !strings.Contains(resultStr, "data: [DONE]") {
		t.Error("expected data: [DONE] in output")
	}

	// Parse each SSE data line
	lines := strings.Split(resultStr, "\n")
	var chunks []map[string]any
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]
		if data == "[DONE]" {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("failed to parse chunk: %v, data: %s", err, data)
		}
		chunks = append(chunks, chunk)
	}

	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks (role + 2 content + finish), got %d", len(chunks))
	}

	// First chunk: role
	if chunks[0]["id"] != "msg_abc123" {
		t.Errorf("expected id=msg_abc123, got %v", chunks[0]["id"])
	}
	if chunks[0]["object"] != "chat.completion.chunk" {
		t.Errorf("expected object=chat.completion.chunk, got %v", chunks[0]["object"])
	}
	choices0 := chunks[0]["choices"].([]any)
	delta0 := choices0[0].(map[string]any)["delta"].(map[string]any)
	if delta0["role"] != "assistant" {
		t.Errorf("expected role=assistant in first chunk, got %v", delta0["role"])
	}

	// Content chunks
	choices1 := chunks[1]["choices"].([]any)
	delta1 := choices1[0].(map[string]any)["delta"].(map[string]any)
	if delta1["content"] != "Hello" {
		t.Errorf("expected content=Hello, got %v", delta1["content"])
	}

	choices2 := chunks[2]["choices"].([]any)
	delta2 := choices2[0].(map[string]any)["delta"].(map[string]any)
	if delta2["content"] != " world" {
		t.Errorf("expected content= world, got %v", delta2["content"])
	}

	// Last chunk: finish_reason
	lastChunk := chunks[len(chunks)-1]
	lastChoices := lastChunk["choices"].([]any)
	finishReason := lastChoices[0].(map[string]any)["finish_reason"]
	if finishReason != "stop" {
		t.Errorf("expected finish_reason=stop, got %v", finishReason)
	}
}

func TestConvertStreamToOpenAI_Empty(t *testing.T) {
	result := ConvertStreamToOpenAI([]byte(""))
	if !strings.Contains(string(result), "data: [DONE]") {
		t.Error("expected data: [DONE] even for empty input")
	}
}

func TestConvertStreamToOpenAI_ToolUse(t *testing.T) {
	anthropicSSE := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_tool","type":"message","role":"assistant","model":"claude-3-opus-20240229","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"Paris\"}"}}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	resultStr := string(ConvertStreamToOpenAI([]byte(anthropicSSE)))
	if !strings.Contains(resultStr, `"tool_calls":[{"function":{"arguments":"","name":"lookup"},"id":"toolu_1","index":0,"type":"function"}]`) {
		t.Fatalf("expected initial tool_call chunk without duplicated arguments, got %q", resultStr)
	}
	if !strings.Contains(resultStr, `"tool_calls":[{"function":{"arguments":"{\"city\":\"Paris\"}"},"index":0}]`) {
		t.Fatalf("expected tool_call arguments delta, got %q", resultStr)
	}
	if strings.Contains(resultStr, `{}{"city":"Paris"}`) {
		t.Fatalf("expected tool_call arguments to avoid duplicated initial input, got %q", resultStr)
	}
	if !strings.Contains(resultStr, `"finish_reason":"tool_calls"`) {
		t.Fatalf("expected tool_calls finish reason, got %q", resultStr)
	}
}

func TestStreamParserSupportsIncrementalToolUseWithoutStopReason(t *testing.T) {
	t.Parallel()

	rawSSE := strings.Join([]string{
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"Paris\"}"}}`,
		``,
	}, "\n")

	infos, err := (&StreamParser{}).Parse(protocol.ParseEvents([]byte(rawSSE)))
	if err != nil {
		t.Fatalf("StreamParser.Parse error = %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(infos))
	}
	if infos[0].ID != "toolu_1" {
		t.Fatalf("tool call id = %q, want toolu_1", infos[0].ID)
	}
	if infos[0].Name != "lookup" {
		t.Fatalf("tool call name = %q, want lookup", infos[0].Name)
	}
	if infos[0].Arguments != "{\"city\":\"Paris\"}" {
		t.Fatalf("tool call arguments = %q, want merged JSON", infos[0].Arguments)
	}
}
