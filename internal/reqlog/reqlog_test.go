package reqlog

import (
	"encoding/json"
	"testing"
)

func TestHashN(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  int // expected length
	}{
		{"hello", 6, 6},
		{"hello", 5, 5},
		{"hello", 4, 4},
		{"hello", 3, 3},
		{"hello", 2, 2},
		{"hello", 1, 1},
		{"", 6, 6},
	}

	for _, tt := range tests {
		got := hashN(tt.input, tt.n)
		if len(got) != tt.want {
			t.Errorf("hashN(%q, %d) length = %d, want %d", tt.input, tt.n, len(got), tt.want)
		}
	}
}

func TestHashNConsistency(t *testing.T) {
	// Same input should always produce same output
	h1 := hashN("test input", 6)
	h2 := hashN("test input", 6)
	if h1 != h2 {
		t.Errorf("hashN not consistent: %q != %q", h1, h2)
	}

	// Different inputs should produce different outputs (with high probability)
	h3 := hashN("different input", 6)
	if h1 == h3 {
		t.Errorf("hashN collision: %q == %q", h1, h3)
	}
}

func TestBuildFingerprint(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantLen  int // minimum expected length (6 for sys + at least 2 per message)
		wantNone bool
	}{
		{
			name:     "empty body",
			body:     `{}`,
			wantNone: true,
		},
		{
			name:    "single user message",
			body:    `{"messages":[{"role":"user","content":"hello"}]}`,
			wantLen: 6 + 6, // sys(0) + first msg(6)
		},
		{
			name:    "user + assistant messages",
			body:    `{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi there"}]}`,
			wantLen: 6 + 6 + 5, // sys(0) + first msg(6) + second msg(5)
		},
		{
			name:    "three messages",
			body:    `{"messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"},{"role":"user","content":"c"}]}`,
			wantLen: 6 + 6 + 5 + 4, // sys(0) + 6 + 5 + 4
		},
		{
			name:    "with system prompt",
			body:    `{"messages":[{"role":"system","content":"you are helpful"},{"role":"user","content":"hello"}]}`,
			wantLen: 6 + 6,
		},
		{
			name:    "five messages - minimum width 2",
			body:    `{"messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"},{"role":"user","content":"c"},{"role":"assistant","content":"d"},{"role":"user","content":"e"}]}`,
			wantLen: 6 + 6 + 5 + 4 + 3 + 2, // widths: 6,5,4,3,2
		},
		{
			name:    "six messages - continues with width 2",
			body:    `{"messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"},{"role":"user","content":"c"},{"role":"assistant","content":"d"},{"role":"user","content":"e"},{"role":"assistant","content":"f"}]}`,
			wantLen: 6 + 6 + 5 + 4 + 3 + 2 + 2, // widths: 6,5,4,3,2,2
		},
		{
			name:     "only system message - no fsm",
			body:     `{"messages":[{"role":"system","content":"you are helpful"}]}`,
			wantNone: true,
		},
		{
			name:    "tool message",
			body:    `{"messages":[{"role":"user","content":"test"},{"role":"assistant","content":"","tool_calls":[{"function":{"name":"get_weather","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_123","content":"sunny"}]}`,
			wantLen: 6 + 6 + 5 + 4,
		},
		{
			name:    "Responses API string input",
			body:    `{"input":"simple prompt"}`,
			wantLen: 6 + 6,
		},
		{
			name:    "Responses API array input",
			body:    `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`,
			wantLen: 6 + 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildFingerprint(json.RawMessage(tt.body))
			if tt.wantNone {
				if got != "" {
					t.Errorf("BuildFingerprint() = %q, want empty", got)
				}
				return
			}
			if got == "" {
				t.Errorf("BuildFingerprint() = empty, want non-empty")
				return
			}
			if len(got) < tt.wantLen {
				t.Errorf("BuildFingerprint() length = %d, want at least %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestBuildFingerprintFormat(t *testing.T) {
	// Test exact format: no separator between sys and fsm
	body := `{"messages":[{"role":"user","content":"hello"}]}`
	got := BuildFingerprint(json.RawMessage(body))

	// Should be exactly 12 chars: 6 for sys + 6 for first message
	if len(got) != 12 {
		t.Errorf("BuildFingerprint() length = %d, want 12", len(got))
	}

	// All characters should be hex
	for i, c := range got {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("BuildFingerprint() char %d = %c, want hex digit", i, c)
		}
	}

	// No separators (no dot, no colon after position 6)
	for _, c := range got {
		if c == '.' || c == ':' {
			t.Errorf("BuildFingerprint() contains separator %c", c)
		}
	}
}

func TestBuildFingerprintConsistency(t *testing.T) {
	// Same body should produce same fingerprint
	body := `{"messages":[{"role":"user","content":"consistent"}]}`
	fp1 := BuildFingerprint(json.RawMessage(body))
	fp2 := BuildFingerprint(json.RawMessage(body))

	if fp1 != fp2 {
		t.Errorf("BuildFingerprint not consistent: %q != %q", fp1, fp2)
	}
}

func TestBuildFingerprintDifferentInputs(t *testing.T) {
	// Different content should produce different fingerprints
	body1 := `{"messages":[{"role":"user","content":"hello"}]}`
	body2 := `{"messages":[{"role":"user","content":"goodbye"}]}`

	fp1 := BuildFingerprint(json.RawMessage(body1))
	fp2 := BuildFingerprint(json.RawMessage(body2))

	if fp1 == fp2 {
		t.Errorf("BuildFingerprint collision: %q == %q", fp1, fp2)
	}
}

func TestBuildFingerprintPrefix(t *testing.T) {
	// Longer conversation should have shorter conversation as prefix
	short := `{"messages":[{"role":"user","content":"hello"}]}`
	long := `{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]}`

	fpShort := BuildFingerprint(json.RawMessage(short))
	fpLong := BuildFingerprint(json.RawMessage(long))

	// First 12 chars of fpLong should equal fpShort (same first message)
	if fpLong[:12] != fpShort {
		t.Errorf("Prefix mismatch: fpLong[:12] = %q, fpShort = %q", fpLong[:12], fpShort)
	}

	// fpLong should be longer than fpShort
	if len(fpLong) <= len(fpShort) {
		t.Errorf("fpLong length %d should be > fpShort length %d", len(fpLong), len(fpShort))
	}
}

func TestContentText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "plain string",
			raw:  `"hello world"`,
			want: "hello world",
		},
		{
			name: "array of text parts",
			raw:  `[{"type":"text","text":"hello"},{"type":"text","text":" world"}]`,
			want: "hello world",
		},
		{
			name: "mixed content types",
			raw:  `[{"type":"text","text":"hello"},{"type":"image","url":"..."}]`,
			want: "hello",
		},
		{
			name: "empty",
			raw:  ``,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contentText(json.RawMessage(tt.raw))
			if got != tt.want {
				t.Errorf("contentText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUserMessageText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "plain string",
			raw:  `"user message"`,
			want: "user message",
		},
		{
			name: "text parts",
			raw:  `[{"type":"text","text":"hello"}]`,
			want: "hello",
		},
		{
			name: "tool_result",
			raw:  `[{"type":"tool_result","tool_use_id":"abc123","content":"result text"}]`,
			want: "abc123result text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := userMessageText(json.RawMessage(tt.raw))
			if got != tt.want {
				t.Errorf("userMessageText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnthropicToolUseText(t *testing.T) {
	raw := `[{"type":"tool_use","name":"get_weather","input":{"city":"SF"}}]`
	got := anthropicToolUseText(json.RawMessage(raw))
	want := `get_weather{"city":"SF"}`
	if got != want {
		t.Errorf("anthropicToolUseText() = %q, want %q", got, want)
	}
}

func TestFilterBillingHeader(t *testing.T) {
	tests := []struct {
		name string
		input string
		want string
	}{
		{
			name: "removes billing header",
			input: "x-anthropic-billing-header: cc_version=1.0; cch=abc123;\nYou are helpful.",
			want: "You are helpful.",
		},
		{
			name: "no billing header",
			input: "You are helpful.",
			want: "You are helpful.",
		},
		{
			name: "multiple billing headers",
			input: "x-anthropic-billing-header: cch=abc;\nOther text\nx-anthropic-billing-header: cch=def;\nMore text",
			want: "Other text\nMore text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterBillingHeader(tt.input)
			if got != tt.want {
				t.Errorf("filterBillingHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContentTextTruncated(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		maxLen int
		want   string
	}{
		{
			name:   "plain string truncated",
			raw:    `"hello world this is a long string"`,
			maxLen: 10,
			want:   "hello worl",
		},
		{
			name:   "skips thinking blocks",
			raw:    `[{"type":"thinking","thinking":"hidden"},{"type":"text","text":"visible"}]`,
			maxLen: 100,
			want:   "visible",
		},
		{
			name:   "multiple text blocks truncated",
			raw:    `[{"type":"text","text":"first"},{"type":"text","text":"second"}]`,
			maxLen: 8,
			want:   "firstsec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contentTextTruncated(json.RawMessage(tt.raw), tt.maxLen)
			if got != tt.want {
				t.Errorf("contentTextTruncated() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildFingerprintStability(t *testing.T) {
	// Test that fingerprints are stable despite dynamic content
	tests := []struct {
		name string
		body string
		want string // partial fingerprint prefix
	}{
		{
			name: "billing header should not affect fingerprint",
			body: `{"messages":[{"role":"system","content":"x-anthropic-billing-header: cch=abc123;\nYou are helpful."},{"role":"user","content":"hello"}]}`,
			want: BuildFingerprint(json.RawMessage(`{"messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"hello"}]}`)),
		},
		{
			name: "thinking blocks should not affect fingerprint",
			body: `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"hidden"},{"type":"text","text":"visible"}]}]}`,
			want: BuildFingerprint(json.RawMessage(`{"messages":[{"role":"assistant","content":[{"type":"text","text":"visible"}]}]}`)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildFingerprint(json.RawMessage(tt.body))
			if got != tt.want {
				t.Errorf("BuildFingerprint() = %q, want %q", got, tt.want)
			}
		})
	}
}