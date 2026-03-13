package openai

import (
	"encoding/json"
	"testing"
)

func TestChatRequestToResponsesRequest(t *testing.T) {
	tests := []struct {
		name      string
		chatReq   ChatCompletionRequest
		wantErr   bool
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
						Role:    "assistant",
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
				Model:    "gpt-4o",
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
				Model:    "gpt-4o",
				Messages: []Message{{Role: "user", Content: "Hi"}},
				Extra: map[string]json.RawMessage{
					"temperature":  json.RawMessage(`0.7`),
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

func TestResponsesRequestToChatRequest(t *testing.T) {
	tests := []struct {
		name      string
		respReq   ResponsesRequest
		wantErr   bool
		checkFunc func(t *testing.T, chatReq ChatCompletionRequest)
	}{
		{
			name: "string input and function tool",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`"hello"`),
				Tools: []json.RawMessage{json.RawMessage(`{"type":"function","name":"lookup","description":"lookup data","parameters":{"type":"object"}}`)},
			},
			checkFunc: func(t *testing.T, chatReq ChatCompletionRequest) {
				if len(chatReq.Messages) != 1 {
					t.Fatalf("expected 1 message, got %d", len(chatReq.Messages))
				}
				if chatReq.Messages[0].Role != "user" || chatReq.Messages[0].Content != "hello" {
					t.Fatalf("unexpected first message: %#v", chatReq.Messages[0])
				}
				if len(chatReq.Tools) != 1 || chatReq.Tools[0].Function.Name != "lookup" {
					t.Fatalf("unexpected tools: %#v", chatReq.Tools)
				}
			},
		},
		{
			name: "input items become chat messages",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`[
					{"type":"message","role":"developer","content":"be precise"},
					{"type":"message","role":"user","content":"weather?"},
					{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{}"},
					{"type":"function_call_output","call_id":"call_1","output":"sunny"}
				]`),
			},
			checkFunc: func(t *testing.T, chatReq ChatCompletionRequest) {
				if len(chatReq.Messages) != 4 {
					t.Fatalf("expected 4 messages, got %d", len(chatReq.Messages))
				}
				if chatReq.Messages[0].Role != "system" {
					t.Fatalf("expected system role, got %s", chatReq.Messages[0].Role)
				}
				if len(chatReq.Messages[2].ToolCalls) != 1 || chatReq.Messages[2].ToolCalls[0].Function.Name != "get_weather" {
					t.Fatalf("unexpected tool call message: %#v", chatReq.Messages[2])
				}
				if chatReq.Messages[3].Role != "tool" || chatReq.Messages[3].ToolCallID != "call_1" {
					t.Fatalf("unexpected tool output message: %#v", chatReq.Messages[3])
				}
			},
		},
		{
			name: "reject previous response id",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`"hello"`),
				Extra: map[string]json.RawMessage{"previous_response_id": json.RawMessage(`"resp_123"`)},
			},
			wantErr: true,
		},
		{
			name: "reject non function tool",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`"hello"`),
				Tools: []json.RawMessage{json.RawMessage(`{"type":"web_search_preview"}`)},
			},
			wantErr: false, // Now converts to mock tool instead of error
			checkFunc: func(t *testing.T, chatReq ChatCompletionRequest) {
				if len(chatReq.Tools) != 1 {
					t.Errorf("expected 1 mock tool, got %d", len(chatReq.Tools))
				}
				if chatReq.Tools[0].Function.Name != MockToolPrefix+"web_search_preview_" {
					t.Errorf("expected mock tool name '%s', got %s", MockToolPrefix+"web_search_preview_", chatReq.Tools[0].Function.Name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatReq, err := ResponsesRequestToChatRequest(tt.respReq)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResponsesRequestToChatRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, chatReq)
			}
		})
	}
}

func TestChatResponseToResponsesResponse(t *testing.T) {
	chatResp := ChatCompletionResponse{
		ID:     "chatcmpl_123",
		Object: "chat.completion",
		Model:  "gpt-4o",
		Choices: []Choice{{
			Index: 0,
			Message: Message{
				Role:    "assistant",
				Content: "Let me check.",
				ToolCalls: []ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: FunctionCall{
						Name:      "lookup",
						Arguments: `{"city":"Paris"}`,
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
		Usage: Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}

	resp, err := ChatResponseToResponsesResponse(chatResp, chatResp.Model)
	if err != nil {
		t.Fatalf("ChatResponseToResponsesResponse() error = %v", err)
	}
	if resp.ID != "chatcmpl_123" {
		t.Fatalf("expected id chatcmpl_123, got %s", resp.ID)
	}
	if len(resp.Output) != 2 {
		t.Fatalf("expected 2 output items, got %d", len(resp.Output))
	}
	if string(resp.Extra["model"]) != `"gpt-4o"` {
		t.Fatalf("expected model in extra, got %s", string(resp.Extra["model"]))
	}
	if string(resp.Extra["status"]) != `"completed"` {
		t.Fatalf("expected completed status, got %s", string(resp.Extra["status"]))
	}
}

func TestChatSSEToResponsesSSE(t *testing.T) {
	input := `data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":""}}]},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\"Paris\"}"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]
`

	output := ChatSSEToResponsesSSE([]byte(input), "gpt-4o")
	outputStr := string(output)
	if !contains(outputStr, "event: response.output_text.delta") {
		t.Fatal("expected response.output_text.delta event")
	}
	if !contains(outputStr, "event: response.output_item.added") {
		t.Fatal("expected response.output_item.added event")
	}
	if !contains(outputStr, "event: response.function_call_arguments.delta") {
		t.Fatal("expected response.function_call_arguments.delta event")
	}
	if !contains(outputStr, "event: response.completed") {
		t.Fatal("expected response.completed event")
	}
	if !contains(outputStr, `"name":"lookup"`) {
		t.Fatal("expected function name in output")
	}
	// Verify complete stream has "completed" status
	if !contains(outputStr, `"status":"completed"`) {
		t.Fatal("expected status:completed for complete stream")
	}
}

func TestChatSSEToResponsesSSE_IncompleteStream(t *testing.T) {
	// Stream without [DONE] marker - simulates disconnection
	input := `data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}
`

	output := ChatSSEToResponsesSSE([]byte(input), "gpt-4o")
	outputStr := string(output)

	// Should have response.completed event
	if !contains(outputStr, "event: response.completed") {
		t.Fatal("expected response.completed event")
	}

	// Should have "incomplete" status
	if !contains(outputStr, `"status":"incomplete"`) {
		t.Fatal("expected status:incomplete for incomplete stream")
	}

	// Should have error message
	if !contains(outputStr, "stream disconnected before completion") {
		t.Fatal("expected stream disconnected error message")
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

// TestResponsesRequestToChatRequest_Reasoning tests that reasoning input items
// are converted to ReasoningContent field in Chat messages.
func TestResponsesRequestToChatRequest_Reasoning(t *testing.T) {
	input := json.RawMessage(`[
		{"type": "message", "role": "user", "content": "What is 2+2?"},
		{"type": "reasoning", "summary": "Let me think about this step by step..."},
		{"type": "message", "role": "assistant", "content": "The answer is 4."}
	]`)

	respReq := ResponsesRequest{
		Model: "gpt-4o",
		Input: input,
	}

	chatReq, err := ResponsesRequestToChatRequest(respReq)
	if err != nil {
		t.Fatalf("ResponsesRequestToChatRequest() error = %v", err)
	}

	// Should have 2 messages: user and assistant with reasoning
	if len(chatReq.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(chatReq.Messages))
	}

	// First message should be user
	if chatReq.Messages[0].Role != "user" {
		t.Errorf("expected first message role 'user', got %s", chatReq.Messages[0].Role)
	}

	// Second message should be assistant with reasoning content
	if chatReq.Messages[1].Role != "assistant" {
		t.Errorf("expected second message role 'assistant', got %s", chatReq.Messages[1].Role)
	}

	// Check ReasoningContent was set
	if chatReq.Messages[1].ReasoningContent != "Let me think about this step by step..." {
		t.Errorf("expected ReasoningContent 'Let me think about this step by step...', got %q", chatReq.Messages[1].ReasoningContent)
	}

	// Check content is also present
	if chatReq.Messages[1].Content != "The answer is 4." {
		t.Errorf("expected content 'The answer is 4.', got %v", chatReq.Messages[1].Content)
	}
}

// TestResponsesRequestToChatRequest_CustomTools tests that custom tool types
// are converted to mock function tools for passthrough.
func TestResponsesRequestToChatRequest_CustomTools(t *testing.T) {
	respReq := ResponsesRequest{
		Model: "gpt-4o",
		Input: json.RawMessage(`[{"type": "message", "role": "user", "content": "Search for weather"}]`),
		Tools: []json.RawMessage{
			json.RawMessage(`{"type": "custom", "name": "web_search", "description": "Search the web"}`),
			json.RawMessage(`{"type": "file_search", "name": "search_files"}`),
			json.RawMessage(`{"type": "function", "name": "get_weather", "description": "Get weather info"}`),
		},
	}

	chatReq, err := ResponsesRequestToChatRequest(respReq)
	if err != nil {
		t.Fatalf("ResponsesRequestToChatRequest() error = %v", err)
	}

	// Should have 3 tools
	if len(chatReq.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(chatReq.Tools))
	}

	// Check custom tool was converted to mock function
	customTool := chatReq.Tools[0]
	if customTool.Type != "function" {
		t.Errorf("expected custom tool type 'function', got %s", customTool.Type)
	}
	if customTool.Function.Name != MockToolPrefix+"custom_web_search" {
		t.Errorf("expected custom tool name '%s', got %s", MockToolPrefix+"custom_web_search", customTool.Function.Name)
	}
	// Original tool data should be preserved in Parameters
	if len(customTool.Function.Parameters.(json.RawMessage)) == 0 {
		t.Error("expected custom tool to have preserved parameters")
	}

	// Check file_search tool was converted
	fileTool := chatReq.Tools[1]
	if fileTool.Function.Name != MockToolPrefix+"file_search_search_files" {
		t.Errorf("expected file_search tool name '%s', got %s", MockToolPrefix+"file_search_search_files", fileTool.Function.Name)
	}

	// Check normal function tool was preserved
	funcTool := chatReq.Tools[2]
	if funcTool.Function.Name != "get_weather" {
		t.Errorf("expected function tool name 'get_weather', got %s", funcTool.Function.Name)
	}
}

// TestResponsesRequestToChatRequest_UnknownInputType tests that unknown input item types
// are converted to mock tool calls for passthrough.
func TestResponsesRequestToChatRequest_UnknownInputType(t *testing.T) {
	input := json.RawMessage(`[
		{"type": "message", "role": "user", "content": "Hello"},
		{"type": "unknown_type", "some_field": "some_value"}
	]`)

	respReq := ResponsesRequest{
		Model: "gpt-4o",
		Input: input,
	}

	chatReq, err := ResponsesRequestToChatRequest(respReq)
	if err != nil {
		t.Fatalf("ResponsesRequestToChatRequest() error = %v", err)
	}

	// Should have 2 messages
	if len(chatReq.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(chatReq.Messages))
	}

	// First message should be user
	if chatReq.Messages[0].Role != "user" {
		t.Errorf("expected first message role 'user', got %s", chatReq.Messages[0].Role)
	}

	// Second message should be assistant with mock tool call
	if chatReq.Messages[1].Role != "assistant" {
		t.Errorf("expected second message role 'assistant', got %s", chatReq.Messages[1].Role)
	}

	// Check tool call was created
	if len(chatReq.Messages[1].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(chatReq.Messages[1].ToolCalls))
	}

	tc := chatReq.Messages[1].ToolCalls[0]
	if tc.Type != "function" {
		t.Errorf("expected tool call type 'function', got %s", tc.Type)
	}
	if tc.Function.Name != MockToolPrefix+"unknown_type" {
		t.Errorf("expected tool call name '%s', got %s", MockToolPrefix+"unknown_type", tc.Function.Name)
	}
	// Original data should be preserved in Arguments
	if !contains(tc.Function.Arguments, "some_field") {
		t.Errorf("expected Arguments to contain original data, got %s", tc.Function.Arguments)
	}
}

// TestChatRequestToResponsesRequest_ReasoningContent tests that ReasoningContent
// in Chat messages is properly converted back to Responses format.
func TestChatRequestToResponsesRequest_ReasoningContent(t *testing.T) {
	chatReq := ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: "user", Content: "Think about this"},
			{Role: "assistant", Content: "The answer is 42.", ReasoningContent: "I need to think deeply..."},
		},
	}

	respReq, err := ChatRequestToResponsesRequest(chatReq)
	if err != nil {
		t.Fatalf("ChatRequestToResponsesRequest() error = %v", err)
	}

	// Parse the input to check reasoning was converted
	var items []map[string]any
	if err := json.Unmarshal(respReq.Input, &items); err != nil {
		t.Fatalf("failed to unmarshal input: %v", err)
	}

	// Should have 3 items: user message, reasoning item, assistant message
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// Second item should be reasoning
	if items[1]["type"] != "reasoning" {
		t.Errorf("expected second item type 'reasoning', got %s", items[1]["type"])
	}
	if items[1]["summary"] != "I need to think deeply..." {
		t.Errorf("expected reasoning summary 'I need to think deeply...', got %s", items[1]["summary"])
	}

	// Third item should be assistant message
	if items[2]["role"] != "assistant" {
		t.Errorf("expected third item role 'assistant', got %s", items[2]["role"])
	}
}
