package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
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
				Tools: []json.RawMessage{json.RawMessage(`{"type":"function","name":"lookup","description":"lookup data","parameters":{"type":"object"},"strict":true}`)},
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
				if chatReq.Tools[0].Function.Strict == nil || !*chatReq.Tools[0].Function.Strict {
					t.Fatalf("expected strict tool to be preserved, got %#v", chatReq.Tools[0].Function.Strict)
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
					{"type":"function_call_output","call_id":"call_1","output":{"status":"sunny"}}
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
				if got := chatReq.Messages[3].Content; got != `{"status":"sunny"}` {
					t.Fatalf("unexpected tool output content: %#v", got)
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
			name: "map max_output_tokens to max_completion_tokens",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`"hello"`),
				Extra: map[string]json.RawMessage{"max_output_tokens": json.RawMessage(`128`)},
			},
			checkFunc: func(t *testing.T, chatReq ChatCompletionRequest) {
				if got := string(chatReq.Extra["max_completion_tokens"]); got != `128` {
					t.Fatalf("max_completion_tokens = %s, want 128", got)
				}
				if _, ok := chatReq.Extra["max_output_tokens"]; ok {
					t.Fatal("max_output_tokens should not be forwarded directly")
				}
			},
		},
		{
			name: "normalize function tool_choice object",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`"hello"`),
				Tools: []json.RawMessage{json.RawMessage(`{"type":"function","name":"lookup","parameters":{"type":"object"}}`)},
				Extra: map[string]json.RawMessage{"tool_choice": json.RawMessage(`{"type":"function","name":"lookup"}`)},
			},
			checkFunc: func(t *testing.T, chatReq ChatCompletionRequest) {
				if got := string(chatReq.Extra["tool_choice"]); got != `{"function":{"name":"lookup"},"type":"function"}` {
					t.Fatalf("tool_choice = %s, want canonical function tool_choice", got)
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
			name: "convert reasoning effort to reasoning_effort",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`"hello"`),
				Extra: map[string]json.RawMessage{"reasoning": json.RawMessage(`{"effort":"medium"}`)},
			},
			checkFunc: func(t *testing.T, chatReq ChatCompletionRequest) {
				if got := string(chatReq.Extra["reasoning_effort"]); got != `"medium"` {
					t.Fatalf("reasoning_effort = %s, want \"medium\"", got)
				}
			},
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
			name: "skip non function tools",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`"hello"`),
				Tools: []json.RawMessage{json.RawMessage(`{"type":"web_search_preview"}`)},
			},
			checkFunc: func(t *testing.T, chatReq ChatCompletionRequest) {
				if len(chatReq.Tools) != 0 {
					t.Fatalf("expected 0 tools, got %d", len(chatReq.Tools))
				}
			},
		},
		{
			name: "reject conflicting max_output_tokens and max_completion_tokens",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`"hello"`),
				Extra: map[string]json.RawMessage{
					"max_output_tokens":     json.RawMessage(`128`),
					"max_completion_tokens": json.RawMessage(`64`),
				},
			},
			wantErr:   true,
			errSubstr: "conflicts with max_completion_tokens",
		},
		{
			name: "reject tool_choice referencing unknown function",
			respReq: ResponsesRequest{
				Model: "gpt-4o",
				Input: json.RawMessage(`"hello"`),
				Tools: []json.RawMessage{json.RawMessage(`{"type":"function","name":"lookup","parameters":{"type":"object"}}`)},
				Extra: map[string]json.RawMessage{"tool_choice": json.RawMessage(`{"type":"function","name":"missing"}`)},
			},
			wantErr:   true,
			errSubstr: "unknown function",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatReq, err := ResponsesRequestToChatRequest(tt.respReq)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResponsesRequestToChatRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errSubstr != "" && (err == nil || !strings.Contains(err.Error(), tt.errSubstr)) {
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
		Usage: Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
			Extra: map[string]json.RawMessage{
				"prompt_tokens_details":     json.RawMessage(`{"cached_tokens":2}`),
				"completion_tokens_details": json.RawMessage(`{"reasoning_tokens":1}`),
			},
		},
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
	if got := string(resp.Output[0]); !strings.Contains(got, `"type":"output_text"`) || !strings.Contains(got, `"text":"Let me check."`) {
		t.Fatalf("unexpected output message item: %s", got)
	}
	if string(resp.Extra["model"]) != `"gpt-4o"` {
		t.Fatalf("expected model in extra, got %s", string(resp.Extra["model"]))
	}
	if resp.Status != "completed" {
		t.Fatalf("expected completed status, got %s", resp.Status)
	}
	usageJSON := resp.Extra["usage"]
	if got := gjson.GetBytes(usageJSON, "input_tokens").Int(); got != 10 {
		t.Fatalf("usage.input_tokens = %d, want 10", got)
	}
	if got := gjson.GetBytes(usageJSON, "output_tokens").Int(); got != 5 {
		t.Fatalf("usage.output_tokens = %d, want 5", got)
	}
	if got := gjson.GetBytes(usageJSON, "total_tokens").Int(); got != 15 {
		t.Fatalf("usage.total_tokens = %d, want 15", got)
	}
	if got := gjson.GetBytes(usageJSON, "input_tokens_details.cached_tokens").Int(); got != 2 {
		t.Fatalf("usage.input_tokens_details.cached_tokens = %d, want 2", got)
	}
	if got := gjson.GetBytes(usageJSON, "output_tokens_details.reasoning_tokens").Int(); got != 1 {
		t.Fatalf("usage.output_tokens_details.reasoning_tokens = %d, want 1", got)
	}
}

func TestChatResponseToResponsesResponseMapsIncompleteFinishReason(t *testing.T) {
	chatResp := ChatCompletionResponse{
		ID:     "chatcmpl_123",
		Object: "chat.completion",
		Model:  "gpt-4o",
		Choices: []Choice{{
			Index: 0,
			Message: Message{
				Role:    "assistant",
				Content: "partial",
			},
			FinishReason: "length",
		}},
	}

	resp, err := ChatResponseToResponsesResponse(chatResp, chatResp.Model)
	if err != nil {
		t.Fatalf("ChatResponseToResponsesResponse() error = %v", err)
	}
	if resp.Status != "incomplete" {
		t.Fatalf("expected incomplete status, got %s", resp.Status)
	}
	if got := string(resp.Extra["incomplete_details"]); got != `{"reason":"max_output_tokens"}` {
		t.Fatalf("unexpected incomplete_details: %s", got)
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

	output, err := ChatSSEToResponsesSSE([]byte(input), "gpt-4o")
	if err != nil {
		t.Fatalf("ChatSSEToResponsesSSE error: %v", err)
	}
	outputStr := string(output)
	if !strings.Contains(outputStr, "event: response.output_text.delta") {
		t.Fatal("expected response.output_text.delta event")
	}
	if !strings.Contains(outputStr, "event: response.output_item.added") {
		t.Fatal("expected response.output_item.added event")
	}
	if !strings.Contains(outputStr, "event: response.function_call_arguments.delta") {
		t.Fatal("expected response.function_call_arguments.delta event")
	}
	if !strings.Contains(outputStr, "event: response.completed") {
		t.Fatal("expected response.completed event")
	}
	if !strings.Contains(outputStr, "event: response.created") {
		t.Fatal("expected response.created event")
	}
	if !strings.Contains(outputStr, "event: response.in_progress") {
		t.Fatal("expected response.in_progress event")
	}
	if !strings.Contains(outputStr, "event: response.output_text.done") {
		t.Fatal("expected response.output_text.done event")
	}
	if !strings.Contains(outputStr, "event: response.function_call_arguments.done") {
		t.Fatal("expected response.function_call_arguments.done event")
	}
	if !strings.Contains(outputStr, "event: response.output_item.done") {
		t.Fatal("expected response.output_item.done event")
	}
	if !strings.Contains(outputStr, `"output_index":0`) {
		t.Fatal("expected output_index metadata")
	}
	if !strings.Contains(outputStr, `"item_id":"chatcmpl_1_msg_0"`) {
		t.Fatal("expected message item_id metadata")
	}
	if !strings.Contains(outputStr, `"name":"lookup"`) {
		t.Fatal("expected function name in output")
	}
	if !strings.Contains(outputStr, `"text":"Hello"`) {
		t.Fatal("expected final text snapshot in done events")
	}
	if !strings.Contains(outputStr, `"arguments":"{\"city\":\"Paris\"}"`) {
		t.Fatal("expected final function arguments snapshot in done events")
	}
	if !strings.Contains(outputStr, `"status":"completed"`) {
		t.Fatal("expected status:completed for complete stream")
	}
	if !strings.Contains(outputStr, `"type":"output_text"`) {
		t.Fatal("expected completed response to contain canonical output_text block")
	}
}

func TestChatSSEToResponsesSSEIncompleteStream(t *testing.T) {
	input := `data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}
`

	output, err := ChatSSEToResponsesSSE([]byte(input), "gpt-4o")
	if err != nil {
		t.Fatalf("ChatSSEToResponsesSSE error: %v", err)
	}
	outputStr := string(output)
	if !strings.Contains(outputStr, "event: response.completed") {
		t.Fatal("expected response.completed event")
	}
	if !strings.Contains(outputStr, `"status":"incomplete"`) {
		t.Fatal("expected status:incomplete for incomplete stream")
	}
	if !strings.Contains(outputStr, "stream disconnected before completion") {
		t.Fatal("expected stream disconnected error message")
	}
}

func TestChatSSEToResponsesSSEMapsLengthFinishReasonToIncomplete(t *testing.T) {
	input := `data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]
`

	output, err := ChatSSEToResponsesSSE([]byte(input), "gpt-4o")
	if err != nil {
		t.Fatalf("ChatSSEToResponsesSSE error: %v", err)
	}
	outputStr := string(output)
	if !strings.Contains(outputStr, `"status":"incomplete"`) {
		t.Fatal("expected status:incomplete for length finish_reason")
	}
	if !strings.Contains(outputStr, `"incomplete_details":{"reason":"max_output_tokens"}`) {
		t.Fatal("expected max_output_tokens incomplete_details")
	}
}

func TestResponsesRequestToChatRequestWithReasoningItem(t *testing.T) {
	respReq := ResponsesRequest{
		Model: "gpt-4o",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"What is 2+2?"},
			{"type":"reasoning","id":"rs_1","summary":[]},
			{"type":"message","role":"assistant","content":"The answer is 4."}
		]`),
	}

	chatReq, err := ResponsesRequestToChatRequest(respReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chatReq.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(chatReq.Messages))
	}
	if chatReq.Messages[1].Role != "assistant" {
		t.Fatalf("expected assistant role for reasoning message, got %s", chatReq.Messages[1].Role)
	}
	if chatReq.Messages[1].ReasoningContent == "" {
		t.Fatalf("expected reasoning_content to be set")
	}
	if !strings.Contains(chatReq.Messages[1].ReasoningContent, `"type":"reasoning"`) {
		t.Fatalf("expected reasoning_content to contain item type, got %s", chatReq.Messages[1].ReasoningContent)
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
	if !strings.Contains(err.Error(), `unsupported input item type "unknown_type"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChatResponseToResponsesResponseWithReasoningContent(t *testing.T) {
	chatResp := ChatCompletionResponse{
		ID:     "chatcmpl_123",
		Object: "chat.completion",
		Model:  "gpt-4o",
		Choices: []Choice{{
			Index: 0,
			Message: Message{
				Role:             "assistant",
				ReasoningContent: "Let me think step by step.",
				Content:          "The answer is 4.",
			},
			FinishReason: "stop",
		}},
	}

	resp, err := ChatResponseToResponsesResponse(chatResp, chatResp.Model)
	if err != nil {
		t.Fatalf("ChatResponseToResponsesResponse() error = %v", err)
	}
	if len(resp.Output) != 2 {
		t.Fatalf("expected 2 output items, got %d", len(resp.Output))
	}
	if got := string(resp.Output[0]); !strings.Contains(got, `"type":"reasoning"`) || !strings.Contains(got, `"id":"chatcmpl_123_rs_0"`) {
		t.Fatalf("unexpected reasoning item: %s", got)
	}
	if got := string(resp.Output[1]); !strings.Contains(got, `"type":"output_text"`) || !strings.Contains(got, `"text":"The answer is 4."`) {
		t.Fatalf("unexpected message item: %s", got)
	}
}

func TestChatSSEToResponsesSSEWithReasoningContent(t *testing.T) {
	input := `data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"reasoning_content":"Let me think"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"reasoning_content":" step by step."},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"The answer is 4."},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]
`

	output, err := ChatSSEToResponsesSSE([]byte(input), "gpt-4o")
	if err != nil {
		t.Fatalf("ChatSSEToResponsesSSE error: %v", err)
	}
	outputStr := string(output)
	if !strings.Contains(outputStr, `"type":"reasoning"`) {
		t.Fatal("expected reasoning type in output")
	}
	if !strings.Contains(outputStr, `"id":"chatcmpl_1_rs_0"`) {
		t.Fatal("expected reasoning item id")
	}
	if !strings.Contains(outputStr, "event: response.output_item.done") {
		t.Fatal("expected response.output_item.done event")
	}
	// reasoning done event should appear before message done event
	reasoningDoneIdx := strings.Index(outputStr, `"type":"reasoning"},"output_index":0,"type":"response.output_item.done"`)
	messageDoneIdx := strings.Index(outputStr, `"type":"message"},"output_index":1,"type":"response.output_item.done"`)
	if reasoningDoneIdx == -1 || messageDoneIdx == -1 {
		t.Fatal("expected both reasoning and message done events")
	}
	if reasoningDoneIdx > messageDoneIdx {
		t.Fatal("expected reasoning done before message done")
	}
}

func TestChatResponseToResponsesResponseWithDSML(t *testing.T) {
	chatResp := ChatCompletionResponse{
		ID:     "chatcmpl_123",
		Object: "chat.completion",
		Model:  "gpt-4o",
		Choices: []Choice{{
			Index: 0,
			Message: Message{
				Role:    "assistant",
				Content: "Let me check.\n<｜DSML｜tool_calls>\n<｜DSML｜invoke name=\"get_weather\">\n<｜DSML｜parameter name=\"city\" string=\"true\">Paris</｜DSML｜parameter>\n</｜DSML｜invoke>\n</｜DSML｜tool_calls>",
			},
			FinishReason: "stop",
		}},
	}

	resp, err := ChatResponseToResponsesResponse(chatResp, chatResp.Model)
	if err != nil {
		t.Fatalf("ChatResponseToResponsesResponse() error = %v", err)
	}
	if len(resp.Output) != 2 {
		t.Fatalf("expected 2 output items, got %d", len(resp.Output))
	}
	// First item should be message with DSML stripped
	if got := string(resp.Output[0]); !strings.Contains(got, `"type":"message"`) || strings.Contains(got, "DSML") {
		t.Fatalf("unexpected message item: %s", got)
	}
	// Second item should be function_call
	if got := string(resp.Output[1]); !strings.Contains(got, `"type":"function_call"`) || !strings.Contains(got, `"name":"get_weather"`) {
		t.Fatalf("unexpected function_call item: %s", got)
	}
}

func TestChatSSEToResponsesSSEWithDSML(t *testing.T) {
	input := `data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Let me check.\n"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"<｜DSML｜tool_calls>\n<｜DSML｜invoke name=\"lookup\">\n<｜DSML｜parameter name=\"q\" string=\"true\">test</｜DSML｜parameter>\n</｜DSML｜invoke>\n</｜DSML｜tool_calls>"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]
`

	output, err := ChatSSEToResponsesSSE([]byte(input), "gpt-4o")
	if err != nil {
		t.Fatalf("ChatSSEToResponsesSSE error: %v", err)
	}
	outputStr := string(output)
	if !strings.Contains(outputStr, "event: response.output_text.delta") {
		t.Fatal("expected response.output_text.delta event")
	}
	if strings.Contains(outputStr, "DSML") {
		t.Fatal("expected no raw DSML tags in output")
	}
	if !strings.Contains(outputStr, `"type":"function_call"`) {
		t.Fatal("expected function_call output item")
	}
	if !strings.Contains(outputStr, `"name":"lookup"`) {
		t.Fatal("expected lookup function name")
	}
}

func TestResponsesRequestToChatRequestInputImage(t *testing.T) {
	respReq := ResponsesRequest{
		Model: "gpt-4o",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[
				{"type":"input_text","text":"What is in this image?"},
				{"type":"input_image","image_url":"https://example.com/img.png","detail":"high"}
			]}
		]`),
	}

	chatReq, err := ResponsesRequestToChatRequest(respReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chatReq.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(chatReq.Messages))
	}
	blocks, ok := chatReq.Messages[0].Content.([]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("expected 2 content blocks, got %#v", chatReq.Messages[0].Content)
	}
	imgBlock, ok := blocks[1].(map[string]any)
	if !ok {
		t.Fatalf("expected image block map, got %#v", blocks[1])
	}
	if imgBlock["type"] != "image_url" {
		t.Fatalf("expected type image_url, got %v", imgBlock["type"])
	}
	imgURL, ok := imgBlock["image_url"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested image_url object, got %#v", imgBlock["image_url"])
	}
	if imgURL["url"] != "https://example.com/img.png" {
		t.Fatalf("unexpected url: %v", imgURL["url"])
	}
	if imgURL["detail"] != "high" {
		t.Fatalf("unexpected detail: %v", imgURL["detail"])
	}
}

func TestCoalesceSystemMessages(t *testing.T) {
	tests := []struct {
		name     string
		input    []Message
		expected []Message
	}{
		{
			name: "no system messages",
			input: []Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
			expected: []Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
		},
		{
			name: "single system message at start",
			input: []Message{
				{Role: "system", Content: "be helpful"},
				{Role: "user", Content: "hello"},
			},
			expected: []Message{
				{Role: "system", Content: "be helpful"},
				{Role: "user", Content: "hello"},
			},
		},
		{
			name: "single system message moved to start",
			input: []Message{
				{Role: "user", Content: "hello"},
				{Role: "system", Content: "be helpful"},
			},
			expected: []Message{
				{Role: "system", Content: "be helpful"},
				{Role: "user", Content: "hello"},
			},
		},
		{
			name: "multiple system messages coalesced",
			input: []Message{
				{Role: "user", Content: "hello"},
				{Role: "system", Content: "be helpful"},
				{Role: "developer", Content: "be precise"},
				{Role: "assistant", Content: "hi"},
			},
			expected: []Message{
				{Role: "system", Content: "be helpful\n\nbe precise"},
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
		},
		{
			name: "developer message preserved when alone",
			input: []Message{
				{Role: "developer", Content: "think step by step"},
				{Role: "user", Content: "hello"},
			},
			expected: []Message{
				{Role: "developer", Content: "think step by step"},
				{Role: "user", Content: "hello"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := coalesceSystemMessages(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d messages, got %d: %#v", len(tt.expected), len(got), got)
			}
			for i, msg := range got {
				if msg.Role != tt.expected[i].Role || msg.Content != tt.expected[i].Content {
					t.Fatalf("message[%d] mismatch: got {role:%s content:%s}, want {role:%s content:%s}",
						i, msg.Role, msg.Content, tt.expected[i].Role, tt.expected[i].Content)
				}
			}
		})
	}
}

func TestChatResponseToResponsesResponseWithThinkTags(t *testing.T) {
	chatResp := ChatCompletionResponse{
		ID:     "chatcmpl_123",
		Object: "chat.completion",
		Model:  "gpt-4o",
		Choices: []Choice{{
			Index: 0,
			Message: Message{
				Role:    "assistant",
				Content: "<think>\nLet me think step by step.\n</think>\nThe answer is 4.",
			},
			FinishReason: "stop",
		}},
	}

	resp, err := ChatResponseToResponsesResponse(chatResp, chatResp.Model)
	if err != nil {
		t.Fatalf("ChatResponseToResponsesResponse() error = %v", err)
	}
	if len(resp.Output) != 2 {
		t.Fatalf("expected 2 output items, got %d", len(resp.Output))
	}
	// First item should be reasoning extracted from <think>
	if got := string(resp.Output[0]); !strings.Contains(got, `"type":"reasoning"`) || !strings.Contains(got, `"text":"Let me think step by step."`) {
		t.Fatalf("unexpected reasoning item: %s", got)
	}
	// Second item should be message with think tags stripped
	if got := string(resp.Output[1]); !strings.Contains(got, `"text":"The answer is 4."`) || strings.Contains(got, "think") {
		t.Fatalf("unexpected message item: %s", got)
	}
}

func TestChatSSEToResponsesSSEWithThinkTags(t *testing.T) {
	input := `data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"<think>\nLet me think"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":" step by step.\n</think>\nThe answer is 4."},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]
`

	output, err := ChatSSEToResponsesSSE([]byte(input), "gpt-4o")
	if err != nil {
		t.Fatalf("ChatSSEToResponsesSSE error: %v", err)
	}
	outputStr := string(output)
	if !strings.Contains(outputStr, "event: response.reasoning.delta") {
		t.Fatal("expected response.reasoning.delta event")
	}
	if !strings.Contains(outputStr, "event: response.output_text.delta") {
		t.Fatal("expected response.output_text.delta event")
	}
	if strings.Contains(outputStr, "<think>") {
		t.Fatal("expected no raw think tags in output")
	}
	if !strings.Contains(outputStr, `"text":"The answer is 4."`) {
		t.Fatal("expected final text snapshot")
	}
}

func TestChatSSEToResponsesSSEReasoningDelta(t *testing.T) {
	input := `data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"reasoning_content":"Let me think"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"reasoning_content":" step by step."},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"The answer is 4."},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]
`

	output, err := ChatSSEToResponsesSSE([]byte(input), "gpt-4o")
	if err != nil {
		t.Fatalf("ChatSSEToResponsesSSE error: %v", err)
	}
	outputStr := string(output)
	if !strings.Contains(outputStr, "event: response.reasoning.delta") {
		t.Fatal("expected response.reasoning.delta event")
	}
	// Should contain two reasoning deltas
	reasoningDeltaCount := strings.Count(outputStr, "event: response.reasoning.delta")
	if reasoningDeltaCount < 2 {
		t.Fatalf("expected at least 2 reasoning.delta events, got %d", reasoningDeltaCount)
	}
	if !strings.Contains(outputStr, `"delta":"Let me think"`) {
		t.Fatal("expected first reasoning delta")
	}
	if !strings.Contains(outputStr, `"delta":" step by step."`) {
		t.Fatal("expected second reasoning delta")
	}
}

func TestExtractThinkTagsMultipleBlocks(t *testing.T) {
	text := "prefix<think>first reasoning</think>middle<think>second reasoning</think>suffix"
	reasoning, remaining := extractThinkTags(text)
	if reasoning != "first reasoning\n\nsecond reasoning" {
		t.Fatalf("unexpected reasoning: %q", reasoning)
	}
	if remaining != "prefixmiddlesuffix" {
		t.Fatalf("unexpected remaining: %q", remaining)
	}
}

func TestExtractThinkTagsEmptyBlock(t *testing.T) {
	text := "prefix<think></think>suffix"
	reasoning, remaining := extractThinkTags(text)
	if reasoning != "" {
		t.Fatalf("expected empty reasoning, got %q", reasoning)
	}
	if remaining != "prefixsuffix" {
		t.Fatalf("unexpected remaining: %q", remaining)
	}
}

func TestExtractThinkTagsUnclosed(t *testing.T) {
	text := "prefix<think>unfinished reasoning"
	reasoning, remaining := extractThinkTags(text)
	if reasoning != "" {
		t.Fatalf("expected empty reasoning for unclosed tag, got %q", reasoning)
	}
	if remaining != text {
		t.Fatalf("expected original text for unclosed tag, got %q", remaining)
	}
}

func TestChatResponseToResponsesResponseWithThinkInArrayBlocks(t *testing.T) {
	chatResp := ChatCompletionResponse{
		ID:     "chatcmpl_123",
		Object: "chat.completion",
		Model:  "gpt-4o",
		Choices: []Choice{{
			Index: 0,
			Message: Message{
				Role:    "assistant",
				Content: []any{map[string]any{"type": "text", "text": "<think>\nStep 1.\n</think>\nResult."}},
			},
			FinishReason: "stop",
		}},
	}

	resp, err := ChatResponseToResponsesResponse(chatResp, chatResp.Model)
	if err != nil {
		t.Fatalf("ChatResponseToResponsesResponse() error = %v", err)
	}
	if len(resp.Output) != 2 {
		t.Fatalf("expected 2 output items, got %d", len(resp.Output))
	}
	if got := string(resp.Output[0]); !strings.Contains(got, `"type":"reasoning"`) || !strings.Contains(got, `"text":"Step 1."`) {
		t.Fatalf("unexpected reasoning item: %s", got)
	}
	if got := string(resp.Output[1]); !strings.Contains(got, `"text":"Result."`) || strings.Contains(got, "think") {
		t.Fatalf("unexpected message item: %s", got)
	}
}

func TestChatSSEToResponsesSSEWithSplitThinkTags(t *testing.T) {
	input := `data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"<thin"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"k>reasoning</think>\ntext"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]
`

	output, err := ChatSSEToResponsesSSE([]byte(input), "gpt-4o")
	if err != nil {
		t.Fatalf("ChatSSEToResponsesSSE error: %v", err)
	}
	outputStr := string(output)
	if !strings.Contains(outputStr, "event: response.reasoning.delta") {
		t.Fatal("expected response.reasoning.delta event")
	}
	if !strings.Contains(outputStr, "event: response.output_text.delta") {
		t.Fatal("expected response.output_text.delta event")
	}
	if strings.Contains(outputStr, "<think>") {
		t.Fatal("expected no raw think tags in output")
	}
	if !strings.Contains(outputStr, `"text":"text"`) {
		t.Fatal("expected final text snapshot")
	}
}

func TestChatSSEToResponsesSSEWithDanglingThink(t *testing.T) {
	input := `data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"<think>\nunfinished"},"finish_reason":null}]}

data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]
`

	output, err := ChatSSEToResponsesSSE([]byte(input), "gpt-4o")
	if err != nil {
		t.Fatalf("ChatSSEToResponsesSSE error: %v", err)
	}
	outputStr := string(output)
	if !strings.Contains(outputStr, "event: response.reasoning.delta") {
		t.Fatal("expected response.reasoning.delta event for dangling think")
	}
	if !strings.Contains(outputStr, `"type":"reasoning"`) {
		t.Fatal("expected reasoning item in completed response")
	}
	if strings.Contains(outputStr, "<think>") {
		t.Fatal("expected no raw think tags in output")
	}
}

func TestCoalesceSystemMessagesWithArrayContent(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "hello"},
		{Role: "system", Content: []any{map[string]any{"type": "text", "text": "be helpful"}}},
		{Role: "developer", Content: "be precise"},
	}
	got := coalesceSystemMessages(messages)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].Role != "system" {
		t.Fatalf("expected system role, got %s", got[0].Role)
	}
	// The string content from array block should be collected.
	expectedContent := "be helpful\n\nbe precise"
	if got[0].Content != expectedContent {
		t.Fatalf("expected content %q, got %v", expectedContent, got[0].Content)
	}
}
