package openai

import (
	"encoding/json"
	"testing"
)

func TestParseDSMLToolCalls(t *testing.T) {
	cases := []struct {
		name          string
		input         string
		wantText      string
		wantCalls     int
		wantFirstName string
		wantFirstArgs string
	}{
		{
			name:     "no dsml",
			input:    "hello world",
			wantText: "hello world",
		},
		{
			name: "v4 single tool call",
			input: "Let me check.\n" +
				"<｜DSML｜tool_calls>\n" +
				"<｜DSML｜invoke name=\"get_weather\">\n" +
				"<｜DSML｜parameter name=\"city\" string=\"true\">Paris</｜DSML｜parameter>\n" +
				"</｜DSML｜invoke>\n" +
				"</｜DSML｜tool_calls>",
			wantText:      "Let me check.",
			wantCalls:     1,
			wantFirstName: "get_weather",
			wantFirstArgs: `{"city":"Paris"}`,
		},
		{
			name: "v3.2 function_calls wrapper",
			input: "Hello\n" +
				"<｜DSML｜function_calls>\n" +
				"<｜DSML｜invoke name=\"lookup\">\n" +
				"<｜DSML｜parameter name=\"query\" string=\"true\">test</｜DSML｜parameter>\n" +
				"</｜DSML｜invoke>\n" +
				"</｜DSML｜function_calls>",
			wantText:      "Hello",
			wantCalls:     1,
			wantFirstName: "lookup",
			wantFirstArgs: `{"query":"test"}`,
		},
		{
			name: "multiple tool calls",
			input: "prefix\n" +
				"<｜DSML｜tool_calls>\n" +
				"<｜DSML｜invoke name=\"a\">\n" +
				"<｜DSML｜parameter name=\"x\" string=\"false\">1</｜DSML｜parameter>\n" +
				"</｜DSML｜invoke>\n" +
				"<｜DSML｜invoke name=\"b\">\n" +
				"<｜DSML｜parameter name=\"y\" string=\"false\">2</｜DSML｜parameter>\n" +
				"</｜DSML｜invoke>\n" +
				"</｜DSML｜tool_calls>\n" +
				"suffix",
			wantText:      "prefix suffix",
			wantCalls:     2,
			wantFirstName: "a",
			wantFirstArgs: `{"x":1}`,
		},
		{
			name: "arguments wrapper normalization",
			input: "<｜DSML｜tool_calls>\n" +
				"<｜DSML｜invoke name=\"write_file\">\n" +
				"<｜DSML｜parameter name=\"arguments\" string=\"false\">{\"path\":\"/tmp/test.txt\",\"content\":\"hello\"}</｜DSML｜parameter>\n" +
				"</｜DSML｜invoke>\n" +
				"</｜DSML｜tool_calls>",
			wantText:      "",
			wantCalls:     1,
			wantFirstName: "write_file",
			wantFirstArgs: `{"content":"hello","path":"/tmp/test.txt"}`,
		},
		{
			name: "json parameter without string attr",
			input: "<｜DSML｜tool_calls>\n" +
				"<｜DSML｜invoke name=\"calc\">\n" +
				"<｜DSML｜parameter name=\"a\" string=\"false\">42</｜DSML｜parameter>\n" +
				"<｜DSML｜parameter name=\"b\">true</｜DSML｜parameter>\n" +
				"</｜DSML｜invoke>\n" +
				"</｜DSML｜tool_calls>",
			wantText:      "",
			wantCalls:     1,
			wantFirstName: "calc",
			wantFirstArgs: `{"a":42,"b":true}`,
		},
		{
			name: "ascii tool call wrapper",
			input: "\n\n<tool_call>\n" +
				"<function=lookup_weather>\n" +
				"<parameter=city>\n" +
				"Hangzhou\n" +
				"</parameter>\n" +
				"</function>\n" +
				"</tool_call>",
			wantText:      "",
			wantCalls:     1,
			wantFirstName: "lookup_weather",
			wantFirstArgs: `{"city":"Hangzhou"}`,
		},
		{
			name:      "unclosed tag returns original",
			input:     `text <｜DSML｜tool_calls> incomplete`,
			wantText:  `text <｜DSML｜tool_calls> incomplete`,
			wantCalls: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, calls, found := parseDSMLToolCalls(tc.input)
			if text != tc.wantText {
				t.Errorf("remaining text = %q, want %q", text, tc.wantText)
			}
			if len(calls) != tc.wantCalls {
				t.Errorf("calls = %d, want %d", len(calls), tc.wantCalls)
			}
			if tc.wantCalls > 0 && len(calls) > 0 {
				if calls[0].Name != tc.wantFirstName {
					t.Errorf("first call name = %q, want %q", calls[0].Name, tc.wantFirstName)
				}
				var got, want map[string]any
				_ = json.Unmarshal([]byte(calls[0].Arguments), &got)
				_ = json.Unmarshal([]byte(tc.wantFirstArgs), &want)
				gotBytes, _ := json.Marshal(got)
				wantBytes, _ := json.Marshal(want)
				if string(gotBytes) != string(wantBytes) {
					t.Errorf("first call args = %s, want %s", calls[0].Arguments, tc.wantFirstArgs)
				}
			}
			if (len(calls) > 0) != (tc.wantCalls > 0) {
				t.Errorf("found = %v, want %v", len(calls) > 0, tc.wantCalls > 0)
			}
			_ = found
		})
	}
}

func TestHasIncompleteDSML(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"no dsml here", false},
		{"<｜DSML｜tool_calls>hello</｜DSML｜tool_calls>", false},
		{"<｜DSML｜tool_calls> incomplete", true},
		{"<｜DSML｜function_calls> incomplete", true},
		{"closed then open <｜DSML｜tool_calls>x</｜DSML｜tool_calls> then <｜DSML｜tool_calls> open", true},
	}

	for _, tc := range cases {
		got := hasIncompleteDSML(tc.input)
		if got != tc.want {
			t.Errorf("hasIncompleteDSML(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
