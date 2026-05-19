package openai

import "strings"

// sanitizeAssistantText removes known system-prompt leakage tags (e.g.
// <tool_reminder> and <｜DSML｜tool_reminder>) that some upstream models
// emit into assistant content. These tags are internal instructions and
// must not reach the client.
func sanitizeAssistantText(text string) string {
	// tagPairs lists known opening/closing tag pairs that should be stripped.
	tagPairs := [][2]string{
		{"<tool_reminder>", "</tool_reminder>"},
		{"<｜DSML｜tool_reminder>", "</｜DSML｜tool_reminder>"},
		{"<tool_specific_instructions>", "</tool_specific_instructions>"},
		{"<｜DSML｜tool_specific_instructions>", "</｜DSML｜tool_specific_instructions>"},
	}
	for _, pair := range tagPairs {
		for {
			start := strings.Index(text, pair[0])
			if start == -1 {
				break
			}
			end := strings.Index(text[start:], pair[1])
			if end == -1 {
				break
			}
			end += start + len(pair[1])
			prefix := strings.TrimRight(text[:start], " \t\n\r")
			suffix := strings.TrimLeft(text[end:], " \t\n\r")
			if prefix != "" && suffix != "" {
				text = prefix + " " + suffix
			} else {
				text = prefix + suffix
			}
		}
	}
	return text
}
