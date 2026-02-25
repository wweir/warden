package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/wweir/warden/pkg/openai"
)

func TestMarshalRequest_Basic(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "claude-3-opus-20240229",
		Messages: []openai.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	body, err := MarshalRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// max_tokens should be defaulted to 4096
	var maxTokens int
	json.Unmarshal(result["max_tokens"], &maxTokens)
	if maxTokens != 4096 {
		t.Errorf("expected max_tokens=4096, got %d", maxTokens)
	}

	var model string
	json.Unmarshal(result["model"], &model)
	if model != "claude-3-opus-20240229" {
		t.Errorf("expected model=claude-3-opus-20240229, got %s", model)
	}
}

func TestMarshalRequest_SystemExtraction(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "claude-3-opus-20240229",
		Messages: []openai.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "system", Content: "Be concise."},
			{Role: "user", Content: "Hello"},
		},
	}

	body, err := MarshalRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(body, &result)

	var system string
	json.Unmarshal(result["system"], &system)
	if system != "You are helpful.\n\nBe concise." {
		t.Errorf("unexpected system: %s", system)
	}

	var msgs []map[string]any
	json.Unmarshal(result["messages"], &msgs)
	if len(msgs) != 1 {
		t.Errorf("expected 1 message (system extracted), got %d", len(msgs))
	}
}

func TestMarshalRequest_ToolCalls(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "claude-3-opus-20240229",
		Messages: []openai.Message{
			{Role: "user", Content: "What's the weather?"},
			{
				Role: "assistant",
				ToolCalls: []openai.ToolCall{
					{
						ID:   "call_123",
						Type: "function",
						Function: openai.FunctionCall{
							Name:      "get_weather",
							Arguments: `{"location":"NYC"}`,
						},
					},
				},
			},
			{Role: "tool", ToolCallID: "call_123", Content: "Sunny, 72F"},
		},
	}

	body, err := MarshalRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(body, &result)

	var msgs []json.RawMessage
	json.Unmarshal(result["messages"], &msgs)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// assistant message should have content blocks with tool_use
	var assistantMsg map[string]json.RawMessage
	json.Unmarshal(msgs[1], &assistantMsg)

	var blocks []map[string]any
	json.Unmarshal(assistantMsg["content"], &blocks)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(blocks))
	}
	if blocks[0]["type"] != "tool_use" {
		t.Errorf("expected tool_use block, got %v", blocks[0]["type"])
	}
	if blocks[0]["name"] != "get_weather" {
		t.Errorf("expected name=get_weather, got %v", blocks[0]["name"])
	}

	// tool message should be converted to user with tool_result
	var toolMsg map[string]json.RawMessage
	json.Unmarshal(msgs[2], &toolMsg)

	var role string
	json.Unmarshal(toolMsg["role"], &role)
	if role != "user" {
		t.Errorf("expected role=user for tool_result, got %s", role)
	}

	var toolResults []map[string]any
	json.Unmarshal(toolMsg["content"], &toolResults)
	if len(toolResults) != 1 {
		t.Fatalf("expected 1 tool_result, got %d", len(toolResults))
	}
	if toolResults[0]["type"] != "tool_result" {
		t.Errorf("expected type=tool_result, got %v", toolResults[0]["type"])
	}
	if toolResults[0]["tool_use_id"] != "call_123" {
		t.Errorf("expected tool_use_id=call_123, got %v", toolResults[0]["tool_use_id"])
	}
}

func TestMarshalRequest_Tools(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "claude-3-opus-20240229",
		Messages: []openai.Message{
			{Role: "user", Content: "Hello"},
		},
		Tools: []openai.Tool{
			{
				Type: "function",
				Function: openai.Function{
					Name:        "get_weather",
					Description: "Get weather info",
					Parameters:  map[string]any{"type": "object"},
				},
			},
		},
	}

	body, err := MarshalRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(body, &result)

	var tools []map[string]any
	json.Unmarshal(result["tools"], &tools)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0]["name"] != "get_weather" {
		t.Errorf("expected name=get_weather, got %v", tools[0]["name"])
	}
	// should use input_schema, not parameters
	if tools[0]["input_schema"] == nil {
		t.Error("expected input_schema to be set")
	}
	if tools[0]["parameters"] != nil {
		t.Error("should not have parameters field")
	}
}

func TestUnmarshalResponse_TextOnly(t *testing.T) {
	anthJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello!"}],
		"model": "claude-3-opus-20240229",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	resp, err := UnmarshalResponse([]byte(anthJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.ID != "msg_123" {
		t.Errorf("expected id=msg_123, got %s", resp.ID)
	}
	if resp.Object != "chat.completion" {
		t.Errorf("expected object=chat.completion, got %s", resp.Object)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "Hello!" {
		t.Errorf("expected content=Hello!, got %v", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason=stop, got %s", resp.Choices[0].FinishReason)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("expected prompt_tokens=10, got %d", resp.Usage.PromptTokens)
	}
}

func TestUnmarshalResponse_ToolUse(t *testing.T) {
	anthJSON := `{
		"id": "msg_456",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Let me check the weather."},
			{"type": "tool_use", "id": "toolu_789", "name": "get_weather", "input": {"location": "NYC"}}
		],
		"model": "claude-3-opus-20240229",
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 20, "output_tokens": 15}
	}`

	resp, err := UnmarshalResponse([]byte(anthJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason=tool_calls, got %s", resp.Choices[0].FinishReason)
	}

	msg := resp.Choices[0].Message
	if msg.Content != "Let me check the weather." {
		t.Errorf("unexpected content: %v", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool_call, got %d", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].ID != "toolu_789" {
		t.Errorf("expected id=toolu_789, got %s", msg.ToolCalls[0].ID)
	}
	if msg.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("expected name=get_weather, got %s", msg.ToolCalls[0].Function.Name)
	}
	// Anthropic input is json.RawMessage, preserves original formatting
	var argMap map[string]string
	if err := json.Unmarshal([]byte(msg.ToolCalls[0].Function.Arguments), &argMap); err != nil {
		t.Fatalf("failed to parse arguments: %v", err)
	}
	if argMap["location"] != "NYC" {
		t.Errorf("expected location=NYC, got %s", argMap["location"])
	}
}

func TestMapStopReason(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"end_turn", "stop"},
		{"tool_use", "tool_calls"},
		{"max_tokens", "length"},
		{"stop_sequence", "stop"},
		{"unknown", "unknown"},
	}

	for _, tc := range tests {
		got := MapStopReason(tc.input)
		if got != tc.expected {
			t.Errorf("MapStopReason(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestConsecutiveToolMessagesMerge(t *testing.T) {
	msgs := []openai.Message{
		{Role: "user", Content: "Check two things"},
		{
			Role: "assistant",
			ToolCalls: []openai.ToolCall{
				{ID: "c1", Type: "function", Function: openai.FunctionCall{Name: "t1", Arguments: `{}`}},
				{ID: "c2", Type: "function", Function: openai.FunctionCall{Name: "t2", Arguments: `{}`}},
			},
		},
		{Role: "tool", ToolCallID: "c1", Content: "result1"},
		{Role: "tool", ToolCallID: "c2", Content: "result2"},
	}

	// test via MarshalRequest which calls convertMessages internally
	req := openai.ChatCompletionRequest{
		Model:    "claude-3-opus-20240229",
		Messages: msgs,
	}

	body, err := MarshalRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(body, &result)

	var anthMsgs []json.RawMessage
	json.Unmarshal(result["messages"], &anthMsgs)

	// Should be: user, assistant, user(tool_results merged)
	if len(anthMsgs) != 3 {
		t.Fatalf("expected 3 Anthropic messages, got %d", len(anthMsgs))
	}

	// third message should have 2 tool_result blocks
	var thirdMsg map[string]json.RawMessage
	json.Unmarshal(anthMsgs[2], &thirdMsg)

	var blocks []map[string]any
	json.Unmarshal(thirdMsg["content"], &blocks)
	if len(blocks) != 2 {
		t.Errorf("expected 2 tool_result blocks, got %d", len(blocks))
	}
}
