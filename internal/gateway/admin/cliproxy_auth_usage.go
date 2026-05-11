package admin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
)

func summarizeCLIProxyAuthUsage(raw []byte, payload map[string]any, auth *cliproxyauth.Auth) ([]cliproxyAuthUsageMetric, map[string]json.RawMessage, string, string) {
	usageData := cliproxyAuthUsageData(raw)
	summary := make([]cliproxyAuthUsageMetric, 0, 8)
	root := gjson.ParseBytes(raw)
	if plan := firstGJSONString(root, "plan", "plan_type", "chatgpt_plan_type", "subscription_plan", "account_plan"); plan != "" {
		summary = append(summary, cliproxyAuthUsageMetric{Name: "plan", Value: plan})
	} else if plan := codexPlanTypeFromIDToken(firstGJSONString(root, "id_token", "token.id_token", "tokens.id_token")); plan != "" {
		summary = append(summary, cliproxyAuthUsageMetric{Name: "plan", Value: plan})
	} else if auth != nil && auth.Attributes != nil {
		if plan = firstNonEmptyString(auth.Attributes["plan"], auth.Attributes["plan_type"], auth.Attributes["chatgpt_plan_type"]); plan != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "plan", Value: plan})
		}
	}
	summary = appendFirstUsageMetric(summary, "5h", root,
		"usage.5h", "usage.5_hour", "usage.five_hour", "usage.fiveHour", "usage.five_hour_limit", "usage.five_hour_quota",
		"quota.5h", "quota.5_hour", "quota.five_hour", "quota.fiveHour", "quota.five_hour_limit", "quota.five_hour_quota",
		"limits.5h", "limits.5_hour", "limits.five_hour", "limits.fiveHour", "limits.five_hour_limit", "limits.five_hour_quota",
	)
	summary = appendFirstUsageMetric(summary, "5h_reset", root,
		"usage.5h.reset_at", "usage.5_hour.reset_at", "usage.five_hour.reset_at", "usage.fiveHour.reset_at",
		"usage.5h.reset_after", "usage.5_hour.reset_after", "usage.five_hour.reset_after", "usage.fiveHour.reset_after",
		"quota.5h.reset_at", "quota.5_hour.reset_at", "quota.five_hour.reset_at", "quota.fiveHour.reset_at",
		"limits.5h.reset_at", "limits.5_hour.reset_at", "limits.five_hour.reset_at", "limits.fiveHour.reset_at",
	)
	summary = appendFirstUsageMetric(summary, "weekly", root,
		"usage.weekly", "usage.week", "usage.7d", "usage.seven_day", "usage.sevenDay", "usage.weekly_limit", "usage.weekly_quota",
		"quota.weekly", "quota.week", "quota.7d", "quota.seven_day", "quota.sevenDay", "quota.weekly_limit", "quota.weekly_quota",
		"limits.weekly", "limits.week", "limits.7d", "limits.seven_day", "limits.sevenDay", "limits.weekly_limit", "limits.weekly_quota",
	)
	summary = appendFirstUsageMetric(summary, "weekly_reset", root,
		"usage.weekly.reset_at", "usage.week.reset_at", "usage.7d.reset_at", "usage.seven_day.reset_at", "usage.sevenDay.reset_at",
		"usage.weekly.reset_after", "usage.week.reset_after", "usage.7d.reset_after", "usage.seven_day.reset_after", "usage.sevenDay.reset_after",
		"quota.weekly.reset_at", "quota.week.reset_at", "quota.7d.reset_at", "quota.seven_day.reset_at", "quota.sevenDay.reset_at",
		"limits.weekly.reset_at", "limits.week.reset_at", "limits.7d.reset_at", "limits.seven_day.reset_at", "limits.sevenDay.reset_at",
	)
	if quota := root.Get("quota"); quota.Exists() {
		if quota.Get("exceeded").Bool() {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "quota", Value: "exceeded"})
		} else if reason := strings.TrimSpace(quota.Get("reason").String()); reason != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "quota", Value: reason})
		}
		if recover := firstGJSONString(quota, "next_recover_at", "nextRecoverAt"); recover != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "recover_at", Value: recover})
		}
	}
	for _, key := range []string{"reset_at", "reset_after", "remaining", "credits", "credit_balance", "credits_balance"} {
		if len(summary) >= 8 {
			break
		}
		if result := root.Get(key); result.Exists() && isGJSONScalar(result) {
			summary = append(summary, cliproxyAuthUsageMetric{Name: key, Value: gjsonScalarString(result)})
		}
	}
	if len(summary) > 8 {
		summary = summary[:8]
	}
	disabled, _ := boolFromAny(payload["disabled"])
	unavailable, _ := boolFromAny(payload["unavailable"])
	quotaExceeded := root.Get("quota.exceeded").Bool()
	switch {
	case disabled:
		return summary, usageData, "disabled", "auth file is disabled"
	case unavailable:
		return summary, usageData, "warning", "auth file is marked unavailable"
	case quotaExceeded:
		return summary, usageData, "warning", "quota is marked exceeded"
	case len(summary) > 0 || len(usageData) > 0:
		return summary, usageData, "ok", ""
	default:
		return summary, usageData, "unknown", ""
	}
}

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

func firstExistingGJSON(root gjson.Result, paths ...string) gjson.Result {
	for _, path := range paths {
		result := root.Get(path)
		if result.Exists() {
			return result
		}
	}
	return gjson.Result{}
}

func cliproxyAuthUsageData(raw []byte) map[string]json.RawMessage {
	root := gjson.ParseBytes(raw)
	allowed := []string{
		"usage",
		"quota",
		"model_states",
		"limits",
		"remaining",
		"reset_at",
		"reset_after",
		"credits",
		"credit_balance",
		"credits_balance",
		"minimum_credit_amount_for_usage",
		"maximum_credits",
	}
	data := make(map[string]json.RawMessage)
	for _, path := range allowed {
		result := root.Get(path)
		if !result.Exists() {
			continue
		}
		rawValue := result.Raw
		if rawValue == "" {
			marshaled, err := json.Marshal(result.Value())
			if err != nil {
				continue
			}
			rawValue = string(marshaled)
		}
		if rawValue == "" || rawValue == "null" {
			continue
		}
		data[path] = json.RawMessage(rawValue)
	}
	if len(data) == 0 {
		return nil
	}
	return data
}

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

func isGJSONScalar(result gjson.Result) bool {
	return result.Type == gjson.String || result.Type == gjson.Number || result.Type == gjson.True || result.Type == gjson.False
}

func gjsonScalarString(result gjson.Result) string {
	if result.Type == gjson.String {
		return strings.TrimSpace(result.String())
	}
	return result.Raw
}

func codexPlanTypeFromIDToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	if plan := strings.TrimSpace(gjson.GetBytes(claims, `https://api\.openai\.com/auth.chatgpt_plan_type`).String()); plan != "" {
		return plan
	}
	var payload map[string]any
	if err := json.Unmarshal(claims, &payload); err != nil {
		return ""
	}
	authInfo, _ := payload["https://api.openai.com/auth"].(map[string]any)
	return firstNonEmptyString(authInfo["chatgpt_plan_type"])
}
