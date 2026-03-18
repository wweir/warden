package openai

import (
	"encoding/json"
	"testing"
)

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
