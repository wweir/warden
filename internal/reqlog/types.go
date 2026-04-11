package reqlog

import (
	"encoding/json"
	"time"

	"github.com/wweir/warden/pkg/toolhook"
)

// Record holds structured data for one request/response round-trip.
type Record struct {
	Timestamp    time.Time              `json:"timestamp"`
	RequestID    string                 `json:"request_id"`
	Route        string                 `json:"route"`
	Endpoint     string                 `json:"endpoint"`
	Model        string                 `json:"model"`
	APIKey       string                 `json:"api_key,omitempty"`
	Stream       bool                   `json:"stream"`
	Pending      bool                   `json:"pending,omitempty"`
	Provider     string                 `json:"provider"`
	UserAgent    string                 `json:"user_agent,omitempty"`
	DurationMs   int64                  `json:"duration_ms"`
	Error        string                 `json:"error,omitempty"`
	Fingerprint  string                 `json:"fingerprint,omitempty"`
	Request      json.RawMessage        `json:"request"`
	Response     json.RawMessage        `json:"response,omitempty"`
	TokenUsage   *TokenUsage            `json:"token_usage,omitempty"`
	Failovers    []Failover             `json:"failovers,omitempty"`
	Steps        []Step                 `json:"steps,omitempty"`
	ToolVerdicts []toolhook.HookVerdict `json:"tool_verdicts,omitempty"`
}

type TokenUsage struct {
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	CacheTokens      int64  `json:"cache_tokens,omitempty"`
	TotalTokens      int64  `json:"total_tokens,omitempty"`
	Source           string `json:"source,omitempty"`
	Completeness     string `json:"completeness,omitempty"`
}

// Step records one intermediate round-trip during tool call execution.
type Step struct {
	Iteration   int               `json:"iteration"`
	ToolCalls   []ToolCallEntry   `json:"tool_calls"`
	ToolResults []ToolResultEntry `json:"tool_results"`
	LLMRequest  json.RawMessage   `json:"llm_request,omitempty"`
	LLMResponse json.RawMessage   `json:"llm_response,omitempty"`
}

// Failover records one upstream switch within the same client request.
type Failover struct {
	FailedProvider      string `json:"failed_provider"`
	FailedProviderModel string `json:"failed_provider_model,omitempty"`
	NextProvider        string `json:"next_provider"`
	NextProviderModel   string `json:"next_provider_model,omitempty"`
	Error               string `json:"error"`
}

// ToolCallEntry records one tool call from the LLM.
type ToolCallEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolResultEntry records one tool execution result.
type ToolResultEntry struct {
	CallID  string `json:"call_id"`
	Output  string `json:"output"`
	IsError bool   `json:"is_error,omitempty"`
}

// Logger defines the interface for request/response logging backends.
type Logger interface {
	Log(Record)
	Close() error
}
