package openai

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	// dsmlBlockPattern matches a complete DSML tool_calls or function_calls block.
	// The pattern does NOT consume surrounding whitespace; callers trim as needed.
	dsmlBlockPattern = regexp.MustCompile(`(?s)<｜DSML｜(?:tool_calls|function_calls)>(.*?)</｜DSML｜(?:tool_calls|function_calls)>`)

	// dsmlInvokePattern matches individual invoke elements inside a DSML block.
	dsmlInvokePattern = regexp.MustCompile(`(?s)<｜DSML｜invoke\s+name="([^"]*)">(.*?)</｜DSML｜invoke>`)

	// dsmlParamPattern matches parameter elements inside an invoke.
	dsmlParamPattern = regexp.MustCompile(`(?s)<｜DSML｜parameter\s+name="([^"]*)"(?:\s+string="(true|false)")?\s*>(.*?)</｜DSML｜parameter>`)
)

// dsmlToolCall holds a single parsed DSML tool invocation.
type dsmlToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// parseDSMLToolCalls searches text for DSML tool call blocks, returns the
// remaining text (with DSML blocks removed) and any parsed tool calls.
func parseDSMLToolCalls(text string) (string, []dsmlToolCall, bool) {
	var calls []dsmlToolCall
	remaining := text

	for {
		loc := dsmlBlockPattern.FindStringIndex(remaining)
		if loc == nil {
			break
		}
		submatch := dsmlBlockPattern.FindStringSubmatch(remaining[loc[0]:loc[1]])
		if len(submatch) < 2 {
			break
		}
		inner := submatch[1]

		invokes := parseDSMLInvokes(inner)
		calls = append(calls, invokes...)

		prefix := strings.TrimRight(remaining[:loc[0]], " \t\n\r")
		suffix := strings.TrimLeft(remaining[loc[1]:], " \t\n\r")
		if prefix != "" && suffix != "" {
			remaining = prefix + " " + suffix
		} else {
			remaining = prefix + suffix
		}
	}

	return strings.TrimSpace(remaining), calls, len(calls) > 0
}

func parseDSMLInvokes(inner string) []dsmlToolCall {
	var calls []dsmlToolCall
	matches := dsmlInvokePattern.FindAllStringSubmatch(inner, -1)
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		name := match[1]
		paramContent := match[2]

		args := parseDSMLParams(paramContent)
		calls = append(calls, dsmlToolCall{
			Name:      name,
			Arguments: args,
		})
	}
	return calls
}

func parseDSMLParams(content string) string {
	params := make(map[string]any)
	matches := dsmlParamPattern.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		paramName := match[1]
		stringAttr := match[2]
		value := match[3]

		if stringAttr == "true" {
			params[paramName] = value
		} else {
			// string="false" or no string attribute: try JSON literal first,
			// fall back to string on parse error.
			var v any
			if err := json.Unmarshal([]byte(value), &v); err == nil {
				params[paramName] = v
			} else {
				params[paramName] = value
			}
		}
	}

	// If there is exactly one top-level parameter named "arguments",
	// unwrap it so the actual argument object sits at the top level.
	if len(params) == 1 {
		if v, ok := params["arguments"]; ok {
			switch val := v.(type) {
			case map[string]any:
				params = val
			case string:
				var obj map[string]any
				if err := json.Unmarshal([]byte(val), &obj); err == nil {
					params = obj
				}
			}
		}
	}

	if len(params) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(params)
	return string(b)
}

// hasIncompleteDSML reports whether text contains an unterminated DSML block that
// might need more stream content. It checks every opening tag has a matching close;
// an unmatched open at ANY position (not just the last) means incomplete.
func hasIncompleteDSML(text string) bool {
	openTool := strings.Count(text, "<｜DSML｜tool_calls>")
	closeTool := strings.Count(text, "</｜DSML｜tool_calls>")
	if openTool != closeTool {
		return true
	}
	openFunc := strings.Count(text, "<｜DSML｜function_calls>")
	closeFunc := strings.Count(text, "</｜DSML｜function_calls>")
	if openFunc != closeFunc {
		return true
	}
	// Also check for crossed nesting (malformed).
	lastStart := strings.LastIndex(text, "<｜DSML｜tool_calls>")
	lastFunc := strings.LastIndex(text, "<｜DSML｜function_calls>")
	if lastStart == -1 && lastFunc == -1 {
		return false
	}
	start := lastStart
	if lastFunc > start {
		start = lastFunc
	}
	after := text[start:]
	return !strings.Contains(after, "</｜DSML｜tool_calls>") &&
		!strings.Contains(after, "</｜DSML｜function_calls>")
}

// generateDSMLCallID creates a deterministic call ID for DSML calls.
func generateDSMLCallID(index int) string {
	return fmt.Sprintf("call_dsml_%d", index)
}
