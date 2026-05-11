package admin

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// firstNonEmptyString returns the first trimmed non-empty string value among
// the supplied any values, skipping values that are not strings or whose
// trimmed form is empty.
func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		if s, ok := value.(string); ok {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// firstGJSONString returns the first trimmed non-empty string at any of the
// supplied gjson paths within root.
func firstGJSONString(root gjson.Result, paths ...string) string {
	for _, path := range paths {
		result := root.Get(path)
		if result.Exists() {
			if value := strings.TrimSpace(result.String()); value != "" {
				return value
			}
		}
	}
	return ""
}

// firstExistingGJSON returns the first existing gjson result among the
// supplied paths.
func firstExistingGJSON(root gjson.Result, paths ...string) gjson.Result {
	for _, path := range paths {
		result := root.Get(path)
		if result.Exists() {
			return result
		}
	}
	return gjson.Result{}
}

func isGJSONScalar(result gjson.Result) bool {
	return result.Type == gjson.String || result.Type == gjson.Number || result.Type == gjson.True || result.Type == gjson.False
}

func gjsonScalarString(result gjson.Result) string {
	if result.Type == gjson.String {
		return strings.TrimSpace(result.String())
	}
	return result.Raw
}

// boolFromAny attempts to interpret value as a boolean. Returns the boolean
// and whether the conversion succeeded.
func boolFromAny(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "y":
			return true, true
		case "false", "0", "no", "n":
			return false, true
		}
	}
	return false, false
}

func mapStringAny(value any) (map[string]any, bool) {
	switch m := value.(type) {
	case map[string]any:
		return m, true
	case map[string]string:
		out := make(map[string]any, len(m))
		for key, value := range m {
			out[key] = value
		}
		return out, true
	default:
		return nil, false
	}
}

// extractJSONFromText scans text for the first balanced JSON object literal.
// Returns the trimmed JSON string or "" when no parseable object is found.
func extractJSONFromText(text string) string {
	text = strings.TrimSpace(text)
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return ""
	}
	candidate := strings.TrimSpace(text[start:])
	for len(candidate) > 0 {
		if gjson.Valid(candidate) {
			return candidate
		}
		candidate = strings.TrimSpace(candidate[:len(candidate)-1])
	}
	return ""
}

// appendFirstUsageMetric appends the first matching usage metric (by gjson
// path) to the summary slice. It is a no-op when the summary already contains
// the metric name or when no path resolves to a non-empty value.
func appendFirstUsageMetric(summary []cliproxyAuthUsageMetric, name string, root gjson.Result, paths ...string) []cliproxyAuthUsageMetric {
	if len(summary) >= 8 || hasUsageMetric(summary, name) {
		return summary
	}
	for _, path := range paths {
		result := root.Get(path)
		if !result.Exists() {
			continue
		}
		value := usageMetricValue(result)
		if value == "" {
			continue
		}
		return append(summary, cliproxyAuthUsageMetric{Name: name, Value: value})
	}
	return summary
}

func hasUsageMetric(summary []cliproxyAuthUsageMetric, name string) bool {
	for _, item := range summary {
		if item.Name == name {
			return true
		}
	}
	return false
}

func usageMetricValue(result gjson.Result) string {
	if isGJSONScalar(result) {
		return gjsonScalarString(result)
	}
	used := firstExistingGJSON(result, "used", "current", "consumed")
	limit := firstExistingGJSON(result, "limit", "total", "quota", "maximum")
	if used.Exists() && limit.Exists() && isGJSONScalar(used) && isGJSONScalar(limit) {
		return gjsonScalarString(used) + "/" + gjsonScalarString(limit)
	}
	remaining := firstExistingGJSON(result, "remaining", "available", "left")
	if remaining.Exists() && limit.Exists() && isGJSONScalar(remaining) && isGJSONScalar(limit) {
		return gjsonScalarString(remaining) + "/" + gjsonScalarString(limit) + " remaining"
	}
	if reset := firstExistingGJSON(result, "reset_at", "reset_after", "resets_at", "next_reset_at", "nextResetAt", "next_recover_at", "nextRecoverAt"); reset.Exists() && isGJSONScalar(reset) {
		return gjsonScalarString(reset)
	}
	raw := result.Raw
	if raw == "" {
		marshaled, err := json.Marshal(result.Value())
		if err != nil {
			return ""
		}
		raw = string(marshaled)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(raw)); err == nil {
		raw = compact.String()
	}
	if len(raw) > 96 {
		raw = raw[:93] + "..."
	}
	return raw
}
