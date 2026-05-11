package admin

import (
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
	summary = appendFirstUsageMetric(summary, "5h", root, usagePaths5H...)
	summary = appendFirstUsageMetric(summary, "5h_reset", root, usagePaths5HReset...)
	summary = appendFirstUsageMetric(summary, "weekly", root, usagePathsWeekly...)
	summary = appendFirstUsageMetric(summary, "weekly_reset", root, usagePathsWeeklyReset...)
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
