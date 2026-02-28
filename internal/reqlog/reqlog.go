package reqlog

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// Fingerprint is a compact string that identifies a conversation state for session grouping.
// Format: "{sys_hash}{fsm}" where
//   - sys_hash is a 6-hex-char FNV-32a hash of all system prompt text concatenated
//   - fsm is a variable-length hash string without separators:
//     first hash is 6 chars, then 5, 4, 3 chars, minimum 2 chars for subsequent hashes
//
// Session continuity: two records belong to the same session when they share the same model,
// the same sys_hash, and the earlier fsm is a strict prefix of the later one.

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
	Fingerprint string          `json:"fingerprint,omitempty"`
	Request     json.RawMessage `json:"request"`
	Response    json.RawMessage `json:"response,omitempty"`
	Steps       []Step          `json:"steps,omitempty"`
}

// BuildFingerprint constructs a compact fingerprint string from a parsed request body.
// Returns empty string if rawBody is not valid JSON or contains no user messages.
func BuildFingerprint(rawBody json.RawMessage) string {
	if len(rawBody) == 0 {
		return ""
	}
	jsonStr := string(rawBody)

	var sysTexts []string
	var fsmInputs []string

	// Check for messages array (Chat Completions / Anthropic Messages API)
	messages := gjson.Get(jsonStr, "messages")
	if messages.Exists() && messages.IsArray() {
		// Extract system field (Anthropic format)
		system := gjson.Get(jsonStr, "system")
		if system.Exists() {
			sysTexts = append(sysTexts, filterBillingHeader(contentTextFromResult(system)))
		}

		// Process messages
		for _, msg := range messages.Array() {
			role := msg.Get("role").String()
			contentResult := msg.Get("content")

			switch role {
			case "system":
				sysTexts = append(sysTexts, filterBillingHeader(contentTextFromResult(contentResult)))
			case "user":
				if s := userMessageTextFromResult(contentResult); s != "" {
					fsmInputs = append(fsmInputs, s)
				}
			case "assistant":
				s := assistantMessageText(msg)
				if s != "" {
					fsmInputs = append(fsmInputs, s)
				}
			case "tool", "function":
				toolCallID := msg.Get("tool_call_id").String()
				text := contentTextFromResult(contentResult)
				if s := toolCallID + text; s != "" {
					fsmInputs = append(fsmInputs, s)
				}
			}
		}
	} else {
		// Check for input (Responses API)
		input := gjson.Get(jsonStr, "input")
		if input.Exists() {
			if input.Type == gjson.String {
				fsmInputs = append(fsmInputs, input.String())
			} else if input.IsArray() {
				fsmInputs = append(fsmInputs, extractResponsesInput(input.Array(), &sysTexts)...)
			}
		}
	}

	if len(fsmInputs) == 0 {
		return ""
	}

	// Build fingerprint with decreasing hash lengths
	var b strings.Builder
	b.WriteString(hashN(strings.Join(sysTexts, ""), 6))
	for i, input := range fsmInputs {
		width := 6 - i
		if width < 2 {
			width = 2
		}
		b.WriteString(hashN(input, width))
	}
	return b.String()
}

// contentText extracts plain text from a content JSON field.
// Handles string, array of {type: "text", text: "..."}, and mixed content.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return contentTextFromResult(gjson.ParseBytes(raw))
}

// contentTextFromResult extracts plain text from a gjson.Result content field.
func contentTextFromResult(result gjson.Result) string {
	if !result.Exists() {
		return ""
	}

	if result.Type == gjson.String {
		return result.String()
	}

	if result.IsArray() {
		var parts []string
		for _, item := range result.Array() {
			if item.Get("type").String() == "text" {
				parts = append(parts, item.Get("text").String())
			}
		}
		return strings.Join(parts, "")
	}

	return result.String()
}

// contentTextTruncated extracts text from a content JSON field, skipping thinking blocks,
// and truncates the result to maxLen characters.
func contentTextTruncated(raw json.RawMessage, maxLen int) string {
	if len(raw) == 0 {
		return ""
	}
	result := gjson.ParseBytes(raw)

	var text string
	if result.Type == gjson.String {
		text = result.String()
	} else if result.IsArray() {
		var parts []string
		for _, item := range result.Array() {
			typ := item.Get("type").String()
			if typ == "thinking" {
				continue // skip thinking blocks - they contain dynamic content
			}
			if typ == "text" {
				parts = append(parts, item.Get("text").String())
			}
		}
		text = strings.Join(parts, "")
	} else {
		text = result.String()
	}

	if len(text) > maxLen {
		text = text[:maxLen]
	}
	return text
}

// userMessageText extracts content from a user message JSON field.
// Handles text and tool_result blocks.
func userMessageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return userMessageTextFromResult(gjson.ParseBytes(raw))
}

// userMessageTextFromResult extracts content from a user message gjson.Result.
func userMessageTextFromResult(content gjson.Result) string {
	if !content.Exists() {
		return ""
	}

	if content.Type == gjson.String {
		return content.String()
	}

	if content.IsArray() {
		var parts []string
		for _, item := range content.Array() {
			typ := item.Get("type").String()
			switch typ {
			case "text":
				parts = append(parts, item.Get("text").String())
			case "tool_result":
				toolUseID := item.Get("tool_use_id").String()
				nestedContent := contentTextFromResult(item.Get("content"))
				parts = append(parts, toolUseID+nestedContent)
			}
		}
		return strings.Join(parts, "")
	}

	return ""
}

// anthropicToolUseText extracts tool_use blocks from an Anthropic assistant content JSON field.
// Returns concatenated name+input for each tool_use block.
func anthropicToolUseText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	result := gjson.ParseBytes(raw)
	if !result.IsArray() {
		return ""
	}

	var parts []string
	for _, item := range result.Array() {
		if item.Get("type").String() == "tool_use" {
			name := item.Get("name").String()
			input := item.Get("input").Raw
			parts = append(parts, name+input)
		}
	}
	return strings.Join(parts, "")
}

// assistantMessageText extracts fingerprint-relevant content from an assistant message gjson.Result.
// Includes OpenAI tool_calls, Anthropic tool_use blocks, and truncated text (skips thinking blocks).
func assistantMessageText(msg gjson.Result) string {
	var parts []string

	// OpenAI format: tool_calls at message level
	toolCalls := msg.Get("tool_calls")
	if toolCalls.Exists() && toolCalls.IsArray() {
		for _, tc := range toolCalls.Array() {
			name := tc.Get("function.name").String()
			args := tc.Get("function.arguments").String()
			parts = append(parts, name+args)
		}
	}

	content := msg.Get("content")
	if content.Exists() {
		contentRaw := json.RawMessage(content.Raw)
		text := contentTextTruncated(contentRaw, 100)

		// Also collect Anthropic tool_use blocks from array content
		if content.IsArray() {
			if tu := anthropicToolUseText(contentRaw); tu != "" {
				parts = append(parts, tu)
			}
		}

		if text != "" {
			parts = append(parts, text)
		}
	}

	return strings.Join(parts, "")
}

// filterBillingHeader removes x-anthropic-billing-header lines from system text.
func filterBillingHeader(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "x-anthropic-billing-header:") {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String()
}

// extractResponsesInput extracts FSM inputs from Responses API input array.
func extractResponsesInput(items []gjson.Result, sysTexts *[]string) []string {
	var inputs []string
	for _, item := range items {
		typ := item.Get("type").String()
		switch typ {
		case "message":
			role := item.Get("role").String()
			content := item.Get("content")
			text := extractResponsesTextContent(content)
			switch role {
			case "system":
				*sysTexts = append(*sysTexts, text)
			case "user", "assistant":
				if text != "" {
					inputs = append(inputs, text)
				}
			}
		case "function_call":
			name := item.Get("name").String()
			args := item.Get("arguments").String()
			if s := name + args; s != "" {
				inputs = append(inputs, s)
			}
		case "function_call_output":
			callID := item.Get("call_id").String()
			output := item.Get("output").String()
			if s := callID + output; s != "" {
				inputs = append(inputs, s)
			}
		}
	}
	return inputs
}

// extractResponsesTextContent extracts text from Responses API content.
func extractResponsesTextContent(content gjson.Result) string {
	if !content.Exists() {
		return ""
	}

	if content.Type == gjson.String {
		return content.String()
	}

	if content.IsArray() {
		var parts []string
		for _, item := range content.Array() {
			typ := item.Get("type").String()
			if typ == "text" || typ == "input_text" {
				parts = append(parts, item.Get("text").String())
			}
		}
		return strings.Join(parts, "")
	}

	return ""
}

// hashN returns an n-character lowercase hex FNV-32a hash of s.
func hashN(s string, n int) string {
	h := fnv.New32a()
	h.Write([]byte(s))
	mask := uint32(1<<(n*4)) - 1
	return fmt.Sprintf("%0*x", n, h.Sum32()&mask)
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
