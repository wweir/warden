package admin

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	sel "github.com/wweir/warden/internal/selector"
)

type cliproxyRuntimeUsage struct {
	summary []cliproxyAuthUsageMetric
	data    map[string]json.RawMessage
	status  string
	note    string
	kind    string
}

func (h *Handler) mergeCLIProxyRuntimeUsage(resp *cliproxyAuthFileUsageResponse) {
	if h == nil || h.selector == nil || h.cfg == nil || resp == nil {
		return
	}
	runtime, ok := h.latestCLIProxyRuntimeUsage(resp.Provider)
	if !ok {
		return
	}
	resp.Summary = mergeUsageSummary(resp.Summary, runtime.summary)
	if len(runtime.data) > 0 {
		if resp.Data == nil {
			resp.Data = map[string]json.RawMessage{}
		}
		for key, value := range runtime.data {
			resp.Data[key] = value
		}
	}
	if resp.Status == "unknown" || resp.Status == "ok" {
		resp.Status = runtime.status
	}
	if resp.Note == "" {
		resp.Note = runtime.note
	}
}

func (h *Handler) latestCLIProxyRuntimeUsage(authProvider string) (cliproxyRuntimeUsage, bool) {
	authProvider = strings.ToLower(strings.TrimSpace(authProvider))
	if authProvider == "" {
		return cliproxyRuntimeUsage{}, false
	}
	var bestAuth *cliproxyRuntimeUsage
	var bestAuthAt time.Time
	var bestLimit *cliproxyRuntimeUsage
	var bestLimitAt time.Time
	var bestAny *cliproxyRuntimeUsage
	var bestAnyAt time.Time
	for name, prov := range h.cfg.Provider {
		if prov == nil || prov.Backend != config.ProviderBackendCLIProxy {
			continue
		}
		if !cliproxyAuthProviderMatchesBackend(authProvider, prov.BackendProvider) {
			continue
		}
		status := h.selector.ProviderDetail(name)
		if status == nil {
			continue
		}
		for i := range status.SuppressReasons {
			reason := status.SuppressReasons[i]
			runtime, ok := runtimeUsageFromSuppressReason(name, reason)
			if !ok {
				continue
			}
			switch runtime.kind {
			case "auth_error":
				if bestAuth == nil || reason.Time.After(bestAuthAt) {
					bestAuth = &runtime
					bestAuthAt = reason.Time
				}
				continue
			case "usage_limit":
				if bestLimit == nil || reason.Time.After(bestLimitAt) {
					bestLimit = &runtime
					bestLimitAt = reason.Time
				}
				continue
			}
			if bestAny == nil || reason.Time.After(bestAnyAt) {
				bestAny = &runtime
				bestAnyAt = reason.Time
			}
		}
	}
	if bestAuth != nil && (bestLimit == nil || bestAuthAt.After(bestLimitAt)) {
		return *bestAuth, true
	}
	if bestLimit != nil {
		return *bestLimit, true
	}
	if bestAny != nil {
		return *bestAny, true
	}
	return cliproxyRuntimeUsage{}, false
}

func runtimeUsageFromSuppressReason(providerName string, reason sel.SuppressReason) (cliproxyRuntimeUsage, bool) {
	body := extractJSONFromText(reason.Reason)
	if body == "" || !gjson.Valid(body) {
		return cliproxyRuntimeUsage{}, false
	}
	root := gjson.Parse(body)
	errObj := root.Get("error")
	if errObj.Exists() {
		root = errObj
	}
	summary := make([]cliproxyAuthUsageMetric, 0, 6)
	if plan := firstGJSONString(root, "plan_type", "plan", "chatgpt_plan_type"); plan != "" {
		summary = append(summary, cliproxyAuthUsageMetric{Name: "plan", Value: plan})
	}
	errType := firstGJSONString(root, "type", "code")
	switch errType {
	case "usage_limit_reached":
		summary = append(summary, cliproxyAuthUsageMetric{Name: "5h", Value: "limited"})
		if resetAt := runtimeResetAt(root, reason.Time, "resets_at", "resets_in_seconds"); resetAt != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "5h_reset", Value: resetAt})
		}
	case "authentication_error", "auth_unavailable":
		summary = append(summary, cliproxyAuthUsageMetric{Name: "auth", Value: runtimeAuthErrorValue(root)})
	case "model_cooldown":
		summary = append(summary, cliproxyAuthUsageMetric{Name: "quota", Value: "model_cooldown"})
		if model := firstGJSONString(root, "model"); model != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "model", Value: model})
		}
		if cooldown := firstGJSONString(root, "reset_time"); cooldown != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "cooldown", Value: cooldown})
		}
		if resetAt := runtimeResetAt(root, reason.Time, "", "reset_seconds"); resetAt != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "recover_at", Value: resetAt})
		}
	case "server_error", "internal_server_error":
		if strings.Contains(strings.ToLower(firstGJSONString(root, "message")), "auth_unavailable") || strings.Contains(strings.ToLower(firstGJSONString(root, "code")), "auth_unavailable") {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "auth", Value: "unavailable"})
		}
	}
	summary = appendFirstUsageMetric(summary, "weekly", root, runtimeWeeklyPaths...)
	summary = appendFirstUsageMetric(summary, "weekly_reset", root, runtimeWeeklyResetPaths...)
	if len(summary) == 0 {
		return cliproxyRuntimeUsage{}, false
	}
	data := map[string]json.RawMessage{}
	runtimePayload := map[string]any{
		"provider":    providerName,
		"observed_at": reason.Time.UTC().Format(time.RFC3339),
		"body":        json.RawMessage(body),
	}
	if encoded, err := json.Marshal(runtimePayload); err == nil {
		data[runtimeUsageDataKey(summary)] = encoded
	}
	status := "warning"
	note := "runtime quota data is from the latest sanitized upstream error body"
	kind := "runtime"
	switch {
	case hasUsageMetric(summary, "auth"):
		status = "error"
		note = "runtime auth status is from the latest sanitized upstream error body"
		kind = "auth_error"
	case hasUsageMetric(summary, "5h"):
		kind = "usage_limit"
	case hasUsageMetric(summary, "quota"):
		kind = "quota"
	}
	return cliproxyRuntimeUsage{
		summary: summary,
		data:    data,
		status:  status,
		note:    note,
		kind:    kind,
	}, true
}

func runtimeAuthErrorValue(root gjson.Result) string {
	msg := strings.ToLower(firstGJSONString(root, "message"))
	if strings.Contains(msg, "invalidated") {
		return "invalidated"
	}
	if strings.Contains(msg, "no auth available") {
		return "unavailable"
	}
	code := firstGJSONString(root, "code")
	if strings.EqualFold(code, "auth_unavailable") {
		return "unavailable"
	}
	return firstNonEmptyString(code, "error")
}

func runtimeUsageDataKey(summary []cliproxyAuthUsageMetric) string {
	if hasUsageMetric(summary, "auth") {
		return "runtime_auth"
	}
	return "runtime_quota"
}

func mergeUsageSummary(base, extra []cliproxyAuthUsageMetric) []cliproxyAuthUsageMetric {
	if len(extra) == 0 {
		return base
	}
	out := append([]cliproxyAuthUsageMetric(nil), base...)
	seen := make(map[string]int, len(out))
	for i, item := range out {
		seen[item.Name] = i
	}
	for _, item := range extra {
		if item.Name == "" || item.Value == "" {
			continue
		}
		if idx, ok := seen[item.Name]; ok {
			if out[idx].Value == "" || item.Name == "plan" {
				out[idx].Value = item.Value
			}
			continue
		}
		out = append(out, item)
		seen[item.Name] = len(out) - 1
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func runtimeResetAt(root gjson.Result, observedAt time.Time, unixPath, secondsPath string) string {
	if unixPath != "" {
		if unix := root.Get(unixPath).Int(); unix > 0 {
			return time.Unix(unix, 0).UTC().Format(time.RFC3339)
		}
	}
	if secondsPath != "" {
		if seconds := root.Get(secondsPath).Int(); seconds > 0 {
			return observedAt.Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339)
		}
	}
	return ""
}
