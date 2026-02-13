package openai

import "encoding/json"

// ChatCompletionRequest 表示 OpenAI Chat Completion API 请求
type ChatCompletionRequest struct {
	Model    string                     `json:"model"`
	Messages []Message                  `json:"messages"`
	Tools    []Tool                     `json:"tools,omitempty"`
	Stream   bool                       `json:"stream,omitempty"`
	Extra    map[string]json.RawMessage `json:"-"` // 其余字段透传
}

// Message 表示对话中的一条消息
type Message struct {
	Role       string                     `json:"role"`
	Content    any                        `json:"content,omitempty"`
	ToolCalls  []ToolCall                 `json:"tool_calls,omitempty"`
	ToolCallID string                     `json:"tool_call_id,omitempty"`
	Name       string                     `json:"name,omitempty"`
	Extra      map[string]json.RawMessage `json:"-"` // 其余字段透传
}

// Tool 表示可用的工具
type Tool struct {
	Type     string   `json:"type,omitempty"`
	Function Function `json:"function,omitempty"`
}

// Function 表示函数定义
type Function struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// ToolCall 表示工具调用
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall 表示函数调用
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionResponse 表示 OpenAI Chat Completion API 响应
type ChatCompletionResponse struct {
	ID      string                     `json:"id"`
	Object  string                     `json:"object"`
	Created int64                      `json:"created"`
	Model   string                     `json:"model"`
	Choices []Choice                   `json:"choices"`
	Usage   Usage                      `json:"usage,omitempty"`
	Extra   map[string]json.RawMessage `json:"-"` // 其余字段透传
}

// Choice 表示响应选择
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage 表示 API 使用情况
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}
