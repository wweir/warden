package openai

import "testing"

func TestSanitizeAssistantText(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello world"},
		{"prefix <tool_reminder>secret</tool_reminder> suffix", "prefix suffix"},
		{"<tool_reminder>a</tool_reminder><tool_reminder>b</tool_reminder>", ""},
		{"text\n<tool_reminder>\ninstructions\n</tool_reminder>\nmore", "text more"},
		{"<tool_reminder>unclosed", "<tool_reminder>unclosed"},
		{"prefix <｜DSML｜tool_reminder>secret</｜DSML｜tool_reminder> suffix", "prefix suffix"},
		{"<｜DSML｜tool_specific_instructions>x</｜DSML｜tool_specific_instructions>", ""},
	}

	for _, tc := range cases {
		got := sanitizeAssistantText(tc.input)
		if got != tc.expected {
			t.Fatalf("sanitizeAssistantText(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
