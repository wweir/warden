package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wweir/warden/pkg/protocol/openai"
)

func TestMessagesRequestToChatRequest(t *testing.T) {
	raw := []byte(`{
		"model":"claude-compatible",
		"system":[{"type":"text","text":"be precise"}],
		"messages":[
			{"role":"user","content":[{"type":"text","text":"hello"}]},
			{"role":"assistant","content":[
				{"type":"text","text":"checking"},
				{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"city":"Paris"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"toolu_1","content":"sunny"}
			]}
		],
		"tools":[{"name":"lookup","description":"Lookup weather","input_schema":{"type":"object"}}],
		"tool_choice":{"type":"tool","name":"lookup"},
		"stop_sequences":["END"],
		"max_tokens":128,
		"temperature":0.2
	}`)

	chatReq, err := MessagesRequestToChatRequest(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if chatReq.Model != "claude-compatible" {
		t.Fatalf("model = %q, want claude-compatible", chatReq.Model)
	}
	if len(chatReq.Messages) != 4 {
		t.Fatalf("messages len = %d, want 4", len(chatReq.Messages))
	}
	if chatReq.Messages[0].Role != "system" || chatReq.Messages[0].Content != "be precise" {
		t.Fatalf("system message = %#v, want system/be precise", chatReq.Messages[0])
	}
	if chatReq.Messages[2].Role != "assistant" {
		t.Fatalf("assistant role = %q, want assistant", chatReq.Messages[2].Role)
	}
	if len(chatReq.Messages[2].ToolCalls) != 1 {
		t.Fatalf("assistant tool calls = %d, want 1", len(chatReq.Messages[2].ToolCalls))
	}
	if chatReq.Messages[3].Role != "tool" || chatReq.Messages[3].ToolCallID != "toolu_1" {
		t.Fatalf("tool message = %#v, want tool/toolu_1", chatReq.Messages[3])
	}
	if got := string(chatReq.Extra["stop"]); got != `["END"]` {
		t.Fatalf("stop extra = %s, want [\"END\"]", got)
	}
	if got := string(chatReq.Extra["tool_choice"]); !strings.Contains(got, `"lookup"`) {
		t.Fatalf("tool_choice = %s, want lookup", got)
	}
}

func TestMessagesRequestToChatRequestRejectsMixedUserContent(t *testing.T) {
	raw := []byte(`{
		"model":"claude-compatible",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"hello"},
				{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}
			]}
		]
	}`)

	_, err := MessagesRequestToChatRequest(raw)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "mixes text and tool_result") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChatResponseToMessagesResponse(t *testing.T) {
	respJSON := `{
		"id":"chatcmpl_1",
		"object":"chat.completion",
		"created":1,
		"model":"gpt-4o",
		"choices":[
			{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"checking",
					"tool_calls":[
						{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"city\":\"Paris\"}"}}
					]
				},
				"finish_reason":"tool_calls"
			}
		],
		"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}
	}`

	var resp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role      string `json:"role"`
				Content   any    `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("unmarshal chat response: %v", err)
	}

	chatResp := openAICompatResponseToChat(resp)
	body, err := ChatResponseToMessagesResponse(chatResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal anthropic response: %v", err)
	}
	if result["type"] != "message" {
		t.Fatalf("type = %v, want message", result["type"])
	}
	if result["stop_reason"] != "tool_use" {
		t.Fatalf("stop_reason = %v, want tool_use", result["stop_reason"])
	}
	content := result["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content len = %d, want 2", len(content))
	}
}

func TestChatResponseToMessagesResponseWithReasoningContent(t *testing.T) {
	chatResp := openai.ChatCompletionResponse{
		ID:     "chatcmpl_1",
		Object: "chat.completion",
		Model:  "gpt-4o",
		Choices: []openai.Choice{{
			Index: 0,
			Message: openai.Message{
				Role:             "assistant",
				ReasoningContent: "Let me think step by step.",
				Content:          "The answer is 4.",
			},
			FinishReason: "stop",
		}},
		Usage: openai.Usage{
			PromptTokens:     3,
			CompletionTokens: 5,
			TotalTokens:      8,
		},
	}

	body, err := ChatResponseToMessagesResponse(chatResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal anthropic response: %v", err)
	}
	content := result["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content len = %d, want 2", len(content))
	}
	firstBlock, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("first block is not a map")
	}
	if firstBlock["type"] != "thinking" {
		t.Fatalf("first block type = %v, want thinking", firstBlock["type"])
	}
	if firstBlock["thinking"] != "Let me think step by step." {
		t.Fatalf("first block thinking = %v, want 'Let me think step by step.'", firstBlock["thinking"])
	}
	secondBlock, ok := content[1].(map[string]any)
	if !ok {
		t.Fatalf("second block is not a map")
	}
	if secondBlock["type"] != "text" {
		t.Fatalf("second block type = %v, want text", secondBlock["type"])
	}
}

func TestMessagesRequestToChatRequestPreservesAssistantThinking(t *testing.T) {
	raw := []byte(`{
		"model":"claude-compatible",
		"messages":[
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"Let me think step by step."},
				{"type":"text","text":"The answer is 4."}
			]}
		]
	}`)

	chatReq, err := MessagesRequestToChatRequest(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chatReq.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(chatReq.Messages))
	}
	if chatReq.Messages[0].Role != "assistant" {
		t.Fatalf("role = %q, want assistant", chatReq.Messages[0].Role)
	}
	if chatReq.Messages[0].ReasoningContent != "Let me think step by step." {
		t.Fatalf("reasoning_content = %q, want 'Let me think step by step.'", chatReq.Messages[0].ReasoningContent)
	}
	if chatReq.Messages[0].Content != "The answer is 4." {
		t.Fatalf("content = %q, want 'The answer is 4.'", chatReq.Messages[0].Content)
	}
}

func TestConvertChatStreamToAnthropic(t *testing.T) {
	raw := []byte("" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"city\\\":\\\"Paris\\\"}\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n" +
		"data: [DONE]\n\n")

	anthSSE, err := ConvertChatStreamToAnthropic(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := string(anthSSE)
	if !strings.Contains(text, "event: message_start") {
		t.Fatalf("stream = %q, want message_start", text)
	}
	if !strings.Contains(text, `"text":"hello"`) || !strings.Contains(text, `"type":"text_delta"`) {
		t.Fatalf("stream = %q, want text delta", text)
	}
	if !strings.Contains(text, `"type":"input_json_delta"`) || !strings.Contains(text, `city`) {
		t.Fatalf("stream = %q, want input_json_delta", text)
	}
	if !strings.Contains(text, `"stop_reason":"tool_use"`) {
		t.Fatalf("stream = %q, want tool_use stop reason", text)
	}
}

func TestConvertChatStreamToAnthropicWithReasoningDelta(t *testing.T) {
	raw := []byte("" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"Let me think\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning\":\" step by step.\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"The answer is 4.\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n" +
		"data: [DONE]\n\n")

	anthSSE, err := ConvertChatStreamToAnthropic(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := string(anthSSE)
	if !strings.Contains(text, `"type":"thinking_delta"`) {
		t.Fatalf("stream = %q, want thinking_delta", text)
	}
	if !strings.Contains(text, `"thinking":"Let me think"`) {
		t.Fatalf("stream = %q, want first reasoning delta", text)
	}
	if !strings.Contains(text, `"thinking":" step by step."`) {
		t.Fatalf("stream = %q, want second reasoning delta", text)
	}
	if !strings.Contains(text, `"type":"text_delta"`) {
		t.Fatalf("stream = %q, want text_delta", text)
	}
	if !strings.Contains(text, `"text":"The answer is 4."`) {
		t.Fatalf("stream = %q, want text content", text)
	}
	if !strings.Contains(text, `"stop_reason":"end_turn"`) {
		t.Fatalf("stream = %q, want end_turn stop reason", text)
	}

	// Verify content block indexes: thinking=0, text=1
	if !strings.Contains(text, `"index":0`) {
		t.Fatalf("stream = %q, want index 0 for thinking", text)
	}
	if !strings.Contains(text, `"index":1`) {
		t.Fatalf("stream = %q, want index 1 for text", text)
	}
}

func openAICompatResponseToChat(resp struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role      string `json:"role"`
			Content   any    `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		TotalTokens      int64 `json:"total_tokens"`
	} `json:"usage"`
}) openai.ChatCompletionResponse {
	chatResp := openai.ChatCompletionResponse{
		ID:      resp.ID,
		Object:  resp.Object,
		Created: resp.Created,
		Model:   resp.Model,
		Usage: openai.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
	for _, choice := range resp.Choices {
		outChoice := openai.Choice{
			Index: choice.Index,
			Message: openai.Message{
				Role:    choice.Message.Role,
				Content: choice.Message.Content,
			},
			FinishReason: choice.FinishReason,
		}
		for _, toolCall := range choice.Message.ToolCalls {
			outChoice.Message.ToolCalls = append(outChoice.Message.ToolCalls, openai.ToolCall{
				ID:   toolCall.ID,
				Type: toolCall.Type,
				Function: openai.FunctionCall{
					Name:      toolCall.Function.Name,
					Arguments: toolCall.Function.Arguments,
				},
			})
		}
		chatResp.Choices = append(chatResp.Choices, outChoice)
	}
	return chatResp
}
