package openai

import (
	"encoding/json"
	"testing"
)

func TestChatRequestToResponsesRequest(t *testing.T) {
	tests := []struct {
		name     string
		chatReq  ChatCompletionRequest
		wantErr  bool
		checkFunc func(t *testing.T, respReq ResponsesRequest)
	}{
		{
			name: "basic request with messages",
			chatReq: ChatCompletionRequest{
				Model: "gpt-4o",
				Messages: []Message{
					{Role: "system", Content: "You are helpful."},
					{Role: "user", Content: "Hello"},
					{Role: "assistant", Content: "Hi there!"},
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, respReq ResponsesRequest) {
				if respReq.Model != "gpt-4o" {
					t.Errorf("expected model gpt-4o, got %s", respReq.Model)
				}
				// Check input is an array
				if len(respReq.Input) == 0 || respReq.Input[0] != '[' {
					t.Errorf("expected input to be an array, got %s", string(respReq.Input))
				}
				// Verify system -> developer mapping
				var items []map[string]any
				if err := json.Unmarshal(respReq.Input, &items); err != nil {
					t.Fatalf("failed to unmarshal input: %v", err)
				}
				if len(items) != 3 {
					t.Errorf("expected 3 items, got %d", len(items))
				}
				if items[0]["role"] != "developer" {
					t.Errorf("expected first item role to be 'developer', got %s", items[0]["role"])
				}
			},
		},
		{
			name: "assistant with tool_calls",
			chatReq: ChatCompletionRequest{
				Model: "gpt-4o",
				Messages: []Message{
					{Role: "user", Content: "What's the weather?"},
					{
						Role: "assistant",
						Content: "Let me check.",
						ToolCalls: []ToolCall{
							{
								ID:   "call_123",
								Type: "function",
								Function: FunctionCall{
									Name:      "get_weather",
									Arguments: `{"location":"NYC"}`,
								},
							},
						},
					},
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, respReq ResponsesRequest) {
				var items []map[string]any
				if err := json.Unmarshal(respReq.Input, &items); err != nil {
					t.Fatalf("failed to unmarshal input: %v", err)
				}
				// Should have: user message, assistant message, function_call
				if len(items) != 3 {
					t.Errorf("expected 3 items, got %d", len(items))
				}
				// Check function_call item
				if items[2]["type"] != "function_call" {
					t.Errorf("expected third item type to be 'function_call', got %s", items[2]["type"])
				}
				if items[2]["call_id"] != "call_123" {
					t.Errorf("expected call_id 'call_123', got %s", items[2]["call_id"])
				}
			},
		},
		{
			name: "tool message",
			chatReq: ChatCompletionRequest{
				Model: "gpt-4o",
				Messages: []Message{
					{Role: "user", Content: "What's the weather?"},
					{Role: "assistant", ToolCalls: []ToolCall{{
						ID: "call_123", Type: "function", Function: FunctionCall{Name: "get_weather", Arguments: "{}"},
					}}},
					{Role: "tool", ToolCallID: "call_123", Content: "Sunny, 72F"},
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, respReq ResponsesRequest) {
				var items []map[string]any
				if err := json.Unmarshal(respReq.Input, &items); err != nil {
					t.Fatalf("failed to unmarshal input: %v", err)
				}
				// items[0] = user, items[1] = function_call, items[2] = function_call_output
				if len(items) != 3 {
					t.Errorf("expected 3 items, got %d", len(items))
				}
				// Check function_call_output item (last item)
				lastIdx := len(items) - 1
				if items[lastIdx]["type"] != "function_call_output" {
					t.Errorf("expected last item type to be 'function_call_output', got %s", items[lastIdx]["type"])
				}
				if items[lastIdx]["call_id"] != "call_123" {
					t.Errorf("expected call_id 'call_123', got %s", items[lastIdx]["call_id"])
				}
			},
		},
		{
			name: "tools conversion",
			chatReq: ChatCompletionRequest{
				Model: "gpt-4o",
				Messages: []Message{{Role: "user", Content: "Hi"}},
				Tools: []Tool{
					{
						Type: "function",
						Function: Function{
							Name:        "test_tool",
							Description: "A test tool",
							Parameters:  map[string]any{"type": "object"},
						},
					},
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, respReq ResponsesRequest) {
				if len(respReq.Tools) != 1 {
					t.Errorf("expected 1 tool, got %d", len(respReq.Tools))
				}
				var tool ResponsesFunctionTool
				if err := json.Unmarshal(respReq.Tools[0], &tool); err != nil {
					t.Fatalf("failed to unmarshal tool: %v", err)
				}
				if tool.Name != "test_tool" {
					t.Errorf("expected tool name 'test_tool', got %s", tool.Name)
				}
				if tool.Type != "function" {
					t.Errorf("expected tool type 'function', got %s", tool.Type)
				}
			},
		},
		{
			name: "extra fields passthrough",
			chatReq: ChatCompletionRequest{
				Model: "gpt-4o",
				Messages: []Message{{Role: "user", Content: "Hi"}},
				Extra: map[string]json.RawMessage{
					"temperature": json.RawMessage(`0.7`),
					"custom_field": json.RawMessage(`"custom_value"`),
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, respReq ResponsesRequest) {
				if string(respReq.Extra["temperature"]) != "0.7" {
					t.Errorf("expected temperature 0.7, got %s", string(respReq.Extra["temperature"]))
				}
				if string(respReq.Extra["custom_field"]) != `"custom_value"` {
					t.Errorf("expected custom_field 'custom_value', got %s", string(respReq.Extra["custom_field"]))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			respReq, err := ChatRequestToResponsesRequest(tt.chatReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("ChatRequestToResponsesRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, respReq)
			}
		})
	}
}

func TestResponsesResponseToChatResponse(t *testing.T) {
	tests := []struct {
		name      string
		resp      ResponsesResponse
		model     string
		wantErr   bool
		checkFunc func(t *testing.T, chatResp ChatCompletionResponse)
	}{
		{
			name: "text response",
			resp: ResponsesResponse{
				ID: "resp_123",
				Output: []json.RawMessage{
					json.RawMessage(`{"type":"message","content":"Hello!"}`),
				},
				Extra: map[string]json.RawMessage{
					"usage": json.RawMessage(`{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`),
				},
			},
			model:   "gpt-4o",
			wantErr: false,
			checkFunc: func(t *testing.T, chatResp ChatCompletionResponse) {
				if chatResp.ID != "resp_123" {
					t.Errorf("expected ID 'resp_123', got %s", chatResp.ID)
				}
				if chatResp.Model != "gpt-4o" {
					t.Errorf("expected model 'gpt-4o', got %s", chatResp.Model)
				}
				if chatResp.Object != "chat.completion" {
					t.Errorf("expected object 'chat.completion', got %s", chatResp.Object)
				}
				if len(chatResp.Choices) != 1 {
					t.Fatalf("expected 1 choice, got %d", len(chatResp.Choices))
				}
				if chatResp.Choices[0].Message.Content != "Hello!" {
					t.Errorf("expected content 'Hello!', got %s", chatResp.Choices[0].Message.Content)
				}
				if chatResp.Choices[0].FinishReason != "stop" {
					t.Errorf("expected finish_reason 'stop', got %s", chatResp.Choices[0].FinishReason)
				}
				if chatResp.Usage.TotalTokens != 15 {
					t.Errorf("expected total_tokens 15, got %d", chatResp.Usage.TotalTokens)
				}
			},
		},
		{
			name: "function_call response",
			resp: ResponsesResponse{
				ID: "resp_456",
				Output: []json.RawMessage{
					json.RawMessage(`{"type":"function_call","call_id":"call_abc","name":"get_weather","arguments":"{\"location\":\"NYC\"}"}`),
				},
			},
			model:   "gpt-4o",
			wantErr: false,
			checkFunc: func(t *testing.T, chatResp ChatCompletionResponse) {
				if len(chatResp.Choices[0].Message.ToolCalls) != 1 {
					t.Fatalf("expected 1 tool call, got %d", len(chatResp.Choices[0].Message.ToolCalls))
				}
				tc := chatResp.Choices[0].Message.ToolCalls[0]
				if tc.ID != "call_abc" {
					t.Errorf("expected tool call ID 'call_abc', got %s", tc.ID)
				}
				if tc.Function.Name != "get_weather" {
					t.Errorf("expected function name 'get_weather', got %s", tc.Function.Name)
				}
				if tc.Function.Arguments != `{"location":"NYC"}` {
					t.Errorf("expected arguments '{\"location\":\"NYC\"}', got %s", tc.Function.Arguments)
				}
				if chatResp.Choices[0].FinishReason != "tool_calls" {
					t.Errorf("expected finish_reason 'tool_calls', got %s", chatResp.Choices[0].FinishReason)
				}
			},
		},
		{
			name: "mixed content and tool_calls",
			resp: ResponsesResponse{
				ID: "resp_789",
				Output: []json.RawMessage{
					json.RawMessage(`{"type":"message","content":"Let me check that."}`),
					json.RawMessage(`{"type":"function_call","call_id":"call_xyz","name":"search","arguments":"{}"}`),
				},
			},
			model:   "gpt-4o",
			wantErr: false,
			checkFunc: func(t *testing.T, chatResp ChatCompletionResponse) {
				if chatResp.Choices[0].Message.Content != "Let me check that." {
					t.Errorf("expected content 'Let me check that.', got %s", chatResp.Choices[0].Message.Content)
				}
				if len(chatResp.Choices[0].Message.ToolCalls) != 1 {
					t.Errorf("expected 1 tool call, got %d", len(chatResp.Choices[0].Message.ToolCalls))
				}
				if chatResp.Choices[0].FinishReason != "tool_calls" {
					t.Errorf("expected finish_reason 'tool_calls', got %s", chatResp.Choices[0].FinishReason)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatResp, err := ResponsesResponseToChatResponse(tt.resp, tt.model)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResponsesResponseToChatResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, chatResp)
			}
		})
	}
}

func TestResponsesSSEToChatSSE(t *testing.T) {
	tests := []struct {
		name      string
		inputSSE  string
		checkFunc func(t *testing.T, output []byte)
	}{
		{
			name: "text delta events",
			inputSSE: `event: response.output_text.delta
data: {"delta":"Hello"}

event: response.output_text.delta
data: {"delta":" world"}

event: response.completed
data: {"response":{"id":"resp_123","output":[]}}

`,
			checkFunc: func(t *testing.T, output []byte) {
				outputStr := string(output)
				// Check for role chunk
				if !contains(outputStr, `"role":"assistant"`) {
					t.Error("expected role chunk in output")
				}
				// Check for content chunks
				if !contains(outputStr, `"content":"Hello"`) {
					t.Error("expected 'Hello' content chunk")
				}
				if !contains(outputStr, `"content":" world"`) {
					t.Error("expected ' world' content chunk")
				}
				// Check for finish_reason
				if !contains(outputStr, `"finish_reason":"stop"`) {
					t.Error("expected finish_reason 'stop'")
				}
				// Check for [DONE]
				if !contains(outputStr, "[DONE]") {
					t.Error("expected [DONE] marker")
				}
			},
		},
		{
			name: "function_call events",
			inputSSE: `event: response.output_item.added
data: {"item":{"type":"function_call","call_id":"call_123","name":"test_func"}}

event: response.function_call_arguments.delta
data: {"delta":"{\"arg\":"}

event: response.function_call_arguments.delta
data: {"delta":"1}"}

event: response.completed
data: {"response":{"id":"resp_456","output":[{"type":"function_call","call_id":"call_123","name":"test_func","arguments":"{\"arg\":1}"}]}}

`,
			checkFunc: func(t *testing.T, output []byte) {
				outputStr := string(output)
				// Check for tool_calls chunk
				if !contains(outputStr, `"tool_calls"`) {
					t.Error("expected tool_calls in output")
				}
				// Check for function name
				if !contains(outputStr, `"name":"test_func"`) {
					t.Error("expected function name 'test_func'")
				}
				// Check for finish_reason tool_calls
				if !contains(outputStr, `"finish_reason":"tool_calls"`) {
					t.Error("expected finish_reason 'tool_calls'")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := ResponsesSSEToChatSSE([]byte(tt.inputSSE))
			if tt.checkFunc != nil {
				tt.checkFunc(t, output)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}