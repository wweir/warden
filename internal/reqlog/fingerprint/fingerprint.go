package fingerprint

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/tidwall/gjson"
)

// BuildFingerprint constructs a compact fingerprint string from a parsed request body.
// Returns empty string if rawBody is not valid JSON or contains no user messages.
func BuildFingerprint(rawBody json.RawMessage) string {
	sysTexts, fsmInputs := conversationParts(rawBody, true)
	if len(fsmInputs) == 0 {
		return ""
	}

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

// ConversationText returns normalized full conversation text for continuation
// checks. It is intentionally full text, not the compact fingerprint prefix.
func ConversationText(rawBody json.RawMessage) string {
	sysTexts, fsmInputs := conversationParts(rawBody, false)
	if len(fsmInputs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(sysTexts)+len(fsmInputs))
	for _, text := range sysTexts {
		if text != "" {
			parts = append(parts, "system:"+text)
		}
	}
	for _, text := range fsmInputs {
		if text != "" {
			parts = append(parts, "turn:"+text)
		}
	}
	return strings.Join(parts, "\x1f")
}

func conversationParts(rawBody json.RawMessage, truncateAssistant bool) ([]string, []string) {
	if len(rawBody) == 0 {
		return nil, nil
	}
	jsonStr := string(rawBody)

	var sysTexts []string
	var fsmInputs []string

	messages := gjson.Get(jsonStr, "messages")
	if messages.Exists() && messages.IsArray() {
		system := gjson.Get(jsonStr, "system")
		if system.Exists() {
			sysTexts = append(sysTexts, filterBillingHeader(contentTextFromResult(system)))
		}

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
				if s := assistantMessageText(msg, truncateAssistant); s != "" {
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
		input := gjson.Get(jsonStr, "input")
		if input.Exists() {
			if input.Type == gjson.String {
				fsmInputs = append(fsmInputs, input.String())
			} else if input.IsArray() {
				fsmInputs = append(fsmInputs, extractResponsesInput(input.Array(), &sysTexts, truncateAssistant)...)
			}
		}
	}

	return sysTexts, fsmInputs
}

func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return contentTextFromResult(gjson.ParseBytes(raw))
}

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
				continue
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

func userMessageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return userMessageTextFromResult(gjson.ParseBytes(raw))
}

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

func assistantMessageText(msg gjson.Result, truncateText bool) string {
	var parts []string

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
		text := contentTextFromResult(content)
		if truncateText {
			text = contentTextTruncated(contentRaw, 100)
		}
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

func extractResponsesInput(items []gjson.Result, sysTexts *[]string, truncateAssistant bool) []string {
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
			case "user":
				if text != "" {
					inputs = append(inputs, text)
				}
			case "assistant":
				if truncateAssistant && len(text) > 100 {
					text = text[:100]
				}
				if text != "" {
					inputs = append(inputs, text)
				}
			}
		case "function_call", "custom_tool_call":
			name := item.Get("name").String()
			if name == "" {
				name = item.Get("tool_name").String()
			}
			args := item.Get("arguments").String()
			if args == "" {
				args = item.Get("input").Raw
			}
			if s := name + args; s != "" {
				inputs = append(inputs, s)
			}
		case "function_call_output", "custom_tool_call_output":
			callID := item.Get("call_id").String()
			output := item.Get("output").String()
			if output == "" {
				output = item.Get("content").String()
			}
			if output == "" {
				output = item.Get("result").String()
			}
			if s := callID + output; s != "" {
				inputs = append(inputs, s)
			}
		}
	}
	return inputs
}

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

func hashN(s string, n int) string {
	h := fnv.New32a()
	h.Write([]byte(s))
	mask := uint32(1<<(n*4)) - 1
	return fmt.Sprintf("%0*x", n, h.Sum32()&mask)
}
