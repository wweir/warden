package reqlog

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Record holds structured data for one request/response round-trip.
type Record struct {
	Timestamp   time.Time       `json:"timestamp"`
	RequestID   string          `json:"request_id"`
	Route       string          `json:"route"`
	Endpoint    string          `json:"endpoint"`
	Model       string          `json:"model"`
	Stream      bool            `json:"stream"`
	Provider    string          `json:"provider"`
	UserAgent   string          `json:"user_agent,omitempty"`
	DurationMs  int64           `json:"duration_ms"`
	Error       string          `json:"error,omitempty"`
	Request     json.RawMessage `json:"request"`
	Response    json.RawMessage `json:"response,omitempty"`
	Steps       []Step          `json:"steps,omitempty"`
}

// Step records one intermediate round-trip during tool call execution.
type Step struct {
	Iteration   int               `json:"iteration"`
	ToolCalls   []ToolCallEntry   `json:"tool_calls"`
	ToolResults []ToolResultEntry `json:"tool_results"`
	LLMRequest  json.RawMessage   `json:"llm_request,omitempty"`
	LLMResponse json.RawMessage   `json:"llm_response,omitempty"`
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

// GenerateID returns an 8-character hex string for request identification.
func GenerateID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Sanitize ensures all json.RawMessage fields contain valid JSON.
// Raw bytes that are not valid JSON (e.g. SSE stream data) are wrapped as JSON strings.
func (r *Record) Sanitize() {
	r.Request = ensureJSON(r.Request)
	r.Response = ensureJSON(r.Response)
	for i := range r.Steps {
		r.Steps[i].LLMRequest = ensureJSON(r.Steps[i].LLMRequest)
		r.Steps[i].LLMResponse = ensureJSON(r.Steps[i].LLMResponse)
	}
}

// ensureJSON returns raw as-is if it is valid JSON, otherwise wraps it as a JSON string.
func ensureJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || json.Valid(raw) {
		return raw
	}
	b, _ := json.Marshal(string(raw))
	return b
}
