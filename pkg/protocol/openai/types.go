package openai

import (
	"encoding/json"
	"fmt"
)

// ChatCompletionRequest represents an OpenAI Chat Completion API request.
type ChatCompletionRequest struct {
	Model    string                     `json:"model"`
	Messages []Message                  `json:"messages"`
	Tools    []Tool                     `json:"tools,omitempty"`
	Stream   bool                       `json:"stream,omitempty"`
	Extra    map[string]json.RawMessage `json:"-"`
}

func (r ChatCompletionRequest) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage)
	for k, v := range r.Extra {
		m[k] = v
	}
	if b, err := json.Marshal(r.Model); err == nil {
		m["model"] = b
	}
	if b, err := json.Marshal(r.Messages); err == nil {
		m["messages"] = b
	}
	if len(r.Tools) > 0 {
		if b, err := json.Marshal(r.Tools); err == nil {
			m["tools"] = b
		}
	}
	if r.Stream {
		m["stream"] = json.RawMessage(`true`)
	}
	return json.Marshal(m)
}

func (r *ChatCompletionRequest) UnmarshalJSON(data []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if v, ok := m["model"]; ok {
		json.Unmarshal(v, &r.Model)
		delete(m, "model")
	}
	if v, ok := m["messages"]; ok {
		json.Unmarshal(v, &r.Messages)
		delete(m, "messages")
	}
	if v, ok := m["tools"]; ok {
		json.Unmarshal(v, &r.Tools)
		delete(m, "tools")
	}
	if v, ok := m["stream"]; ok {
		json.Unmarshal(v, &r.Stream)
		delete(m, "stream")
	}
	if len(m) > 0 {
		r.Extra = m
	}
	return nil
}

// Validate checks required fields of ChatCompletionRequest.
func (req *ChatCompletionRequest) Validate() error {
	if req.Model == "" {
		return fmt.Errorf("model is required")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("messages is required")
	}
	for i, msg := range req.Messages {
		if msg.Role == "" {
			return fmt.Errorf("message[%d].role is required", i)
		}
		if msg.Content == nil {
			return fmt.Errorf("message[%d].content is required", i)
		}
	}
	return nil
}

// Message represents a single message in a conversation.
type Message struct {
	Role             string                     `json:"role"`
	Content          any                        `json:"content,omitempty"`
	ReasoningContent string                     `json:"reasoning_content,omitempty"` // Extended thinking content
	ToolCalls        []ToolCall                 `json:"tool_calls,omitempty"`
	ToolCallID       string                     `json:"tool_call_id,omitempty"`
	Name             string                     `json:"name,omitempty"`
	Extra            map[string]json.RawMessage `json:"-"`
}

func (msg Message) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage)
	for k, v := range msg.Extra {
		m[k] = v
	}
	if b, err := json.Marshal(msg.Role); err == nil {
		m["role"] = b
	}
	if msg.Content != nil {
		if b, err := json.Marshal(msg.Content); err == nil {
			m["content"] = b
		}
	}
	if msg.ReasoningContent != "" {
		if b, err := json.Marshal(msg.ReasoningContent); err == nil {
			m["reasoning_content"] = b
		}
	}
	if len(msg.ToolCalls) > 0 {
		if b, err := json.Marshal(msg.ToolCalls); err == nil {
			m["tool_calls"] = b
		}
	}
	if msg.ToolCallID != "" {
		if b, err := json.Marshal(msg.ToolCallID); err == nil {
			m["tool_call_id"] = b
		}
	}
	if msg.Name != "" {
		if b, err := json.Marshal(msg.Name); err == nil {
			m["name"] = b
		}
	}
	return json.Marshal(m)
}

func (msg *Message) UnmarshalJSON(data []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if v, ok := m["role"]; ok {
		json.Unmarshal(v, &msg.Role)
		delete(m, "role")
	}
	if v, ok := m["content"]; ok {
		// content can be string or array, keep as-is via any
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			msg.Content = s
		} else {
			var arr []any
			if err := json.Unmarshal(v, &arr); err == nil {
				msg.Content = arr
			} else {
				msg.Content = v
			}
		}
		delete(m, "content")
	}
	if v, ok := m["reasoning_content"]; ok {
		json.Unmarshal(v, &msg.ReasoningContent)
		delete(m, "reasoning_content")
	}
	if v, ok := m["tool_calls"]; ok {
		json.Unmarshal(v, &msg.ToolCalls)
		delete(m, "tool_calls")
	}
	if v, ok := m["tool_call_id"]; ok {
		json.Unmarshal(v, &msg.ToolCallID)
		delete(m, "tool_call_id")
	}
	if v, ok := m["name"]; ok {
		json.Unmarshal(v, &msg.Name)
		delete(m, "name")
	}
	if len(m) > 0 {
		msg.Extra = m
	}
	return nil
}

// Tool represents an available tool definition.
type Tool struct {
	Type     string   `json:"type,omitempty"`
	Function Function `json:"function,omitempty"`
}

// Function represents a function definition.
type Function struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
	Strict      *bool  `json:"strict,omitempty"`
}

// ToolCall represents a tool call in a response.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// GetName returns the function name for the tool call (implements namedItem constraint).
func (tc ToolCall) GetName() string {
	return tc.Function.Name
}

// FunctionCall represents the function call details within a ToolCall.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionResponse represents an OpenAI Chat Completion API response.
type ChatCompletionResponse struct {
	ID      string                     `json:"id"`
	Object  string                     `json:"object"`
	Created int64                      `json:"created"`
	Model   string                     `json:"model"`
	Choices []Choice                   `json:"choices"`
	Usage   Usage                      `json:"usage,omitempty"`
	Extra   map[string]json.RawMessage `json:"-"`
}

func (r ChatCompletionResponse) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage)
	for k, v := range r.Extra {
		m[k] = v
	}
	if b, err := json.Marshal(r.ID); err == nil {
		m["id"] = b
	}
	if b, err := json.Marshal(r.Object); err == nil {
		m["object"] = b
	}
	if b, err := json.Marshal(r.Created); err == nil {
		m["created"] = b
	}
	if b, err := json.Marshal(r.Model); err == nil {
		m["model"] = b
	}
	if b, err := json.Marshal(r.Choices); err == nil {
		m["choices"] = b
	}
	if r.Usage != (Usage{}) {
		if b, err := json.Marshal(r.Usage); err == nil {
			m["usage"] = b
		}
	}
	return json.Marshal(m)
}

func (r *ChatCompletionResponse) UnmarshalJSON(data []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if v, ok := m["id"]; ok {
		json.Unmarshal(v, &r.ID)
		delete(m, "id")
	}
	if v, ok := m["object"]; ok {
		json.Unmarshal(v, &r.Object)
		delete(m, "object")
	}
	if v, ok := m["created"]; ok {
		json.Unmarshal(v, &r.Created)
		delete(m, "created")
	}
	if v, ok := m["model"]; ok {
		json.Unmarshal(v, &r.Model)
		delete(m, "model")
	}
	if v, ok := m["choices"]; ok {
		json.Unmarshal(v, &r.Choices)
		delete(m, "choices")
	}
	if v, ok := m["usage"]; ok {
		json.Unmarshal(v, &r.Usage)
		delete(m, "usage")
	}
	if len(m) > 0 {
		r.Extra = m
	}
	return nil
}

// Choice represents a response choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage represents API token usage statistics.
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}
