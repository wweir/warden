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
		errSubstr string
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
				if chatReq.Messages[0].Role != "developer" {
					t.Fatalf("expected developer role, got %s", chatReq.Messages[0].Role)
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
			name: "convert instructions to developer message",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`"hello"`),
				Extra: map[string]json.RawMessage{"instructions": json.RawMessage(`"be precise"`)},
			},
			checkFunc: func(t *testing.T, chatReq ChatCompletionRequest) {
				if len(chatReq.Messages) != 2 {
					t.Fatalf("expected 2 messages, got %d", len(chatReq.Messages))
				}
				if chatReq.Messages[0].Role != "developer" || chatReq.Messages[0].Content != "be precise" {
					t.Fatalf("unexpected first message: %#v", chatReq.Messages[0])
				}
				if _, ok := chatReq.Extra["instructions"]; ok {
					t.Fatal("instructions should not be forwarded to chat extra")
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
			wantErr:   true,
			errSubstr: "previous_response_id",
		},
		{
			name: "reject unsupported stateless responses field",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`"hello"`),
				Extra: map[string]json.RawMessage{"max_output_tokens": json.RawMessage(`128`)},
			},
			wantErr:   true,
			errSubstr: "max_output_tokens",
		},
		{
			name: "reject n greater than one",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`"hello"`),
				Extra: map[string]json.RawMessage{"n": json.RawMessage(`2`)},
			},
			wantErr:   true,
			errSubstr: "n > 1",
		},
		{
			name: "reject non function tool",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`"hello"`),
				Tools: []json.RawMessage{json.RawMessage(`{"type":"web_search_preview"}`)},
			},
			wantErr:   true,
			errSubstr: "unsupported tool type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatReq, err := ResponsesRequestToChatRequest(tt.respReq)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResponsesRequestToChatRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errSubstr != "" && (err == nil || !contains(err.Error(), tt.errSubstr)) {
				t.Fatalf("ResponsesRequestToChatRequest() error = %v, want substring %q", err, tt.errSubstr)
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
	if got := string(resp.Output[0]); !contains(got, `"type":"output_text"`) || !contains(got, `"text":"Let me check."`) {
		t.Fatalf("unexpected output message item: %s", got)
	}
	if string(resp.Extra["model"]) != `"gpt-4o"` {
		t.Fatalf("expected model in extra, got %s", string(resp.Extra["model"]))
	}
	if resp.Status != "completed" {
		t.Fatalf("expected completed status, got %s", resp.Status)
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
	if !contains(outputStr, `"status":"completed"`) {
		t.Fatal("expected status:completed for complete stream")
	}
	if !contains(outputStr, `"type":"output_text"`) {
		t.Fatal("expected completed response to contain canonical output_text block")
	}
}

func TestChatSSEToResponsesSSEIncompleteStream(t *testing.T) {
	input := `data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}
`

	output := ChatSSEToResponsesSSE([]byte(input), "gpt-4o")
	outputStr := string(output)
	if !contains(outputStr, "event: response.completed") {
		t.Fatal("expected response.completed event")
	}
	if !contains(outputStr, `"status":"incomplete"`) {
		t.Fatal("expected status:incomplete for incomplete stream")
	}
	if !contains(outputStr, "stream disconnected before completion") {
		t.Fatal("expected stream disconnected error message")
	}
}

func TestResponsesRequestToChatRequestRejectReasoningItem(t *testing.T) {
	respReq := ResponsesRequest{
		Model: "gpt-4o",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"What is 2+2?"},
			{"type":"reasoning","summary":"step by step"},
			{"type":"message","role":"assistant","content":"The answer is 4."}
		]`),
	}

	_, err := ResponsesRequestToChatRequest(respReq)
	if err == nil {
		t.Fatal("expected error for reasoning item")
	}
	if !contains(err.Error(), `unsupported input item type "reasoning"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResponsesRequestToChatRequestRejectUnknownInputType(t *testing.T) {
	respReq := ResponsesRequest{
		Model: "gpt-4o",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"Hello"},
			{"type":"unknown_type","some_field":"some_value"}
		]`),
	}

	_, err := ResponsesRequestToChatRequest(respReq)
	if err == nil {
		t.Fatal("expected error for unknown input item")
	}
	if !contains(err.Error(), `unsupported input item type "unknown_type"`) {
		t.Fatalf("unexpected error: %v", err)
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
