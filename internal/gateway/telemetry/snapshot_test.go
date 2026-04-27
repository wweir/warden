package telemetry

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/wweir/warden/config"
)

func TestListAPIKeysPayloadIncludesCacheTokens(t *testing.T) {
	route := "/snapshot-cache-test"
	apiKey := "cache-client"

	APIKeyRequestCounter.WithLabelValues(apiKey, route, config.RouteProtocolChat, "gpt-4o", "", "chat/completions", "success").Add(2)
	APIKeyTokenCounter.WithLabelValues(apiKey, route, config.RouteProtocolChat, "gpt-4o", "", "prompt").Add(11)
	APIKeyTokenCounter.WithLabelValues(apiKey, route, config.RouteProtocolChat, "gpt-4o", "", "completion").Add(7)
	APIKeyTokenCounter.WithLabelValues(apiKey, route, config.RouteProtocolChat, "gpt-4o", "", "cache").Add(5)

	payload := ListAPIKeysPayload(map[string]*config.RouteConfig{
		route: {
			Protocol: config.RouteProtocolChat,
			APIKeys: map[string]config.SecretString{
				apiKey: "secret",
			},
		},
	})

	if len(payload) != 1 {
		t.Fatalf("payload length = %d, want 1", len(payload))
	}

	usage, ok := payload[0]["usage"]
	if !ok {
		t.Fatalf("payload missing usage: %#v", payload[0])
	}

	raw, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("marshal usage: %v", err)
	}

	var stats struct {
		TotalRequests        int64 `json:"total_requests"`
		SuccessRequests      int64 `json:"success_requests"`
		FailureRequests      int64 `json:"failure_requests"`
		PromptTokens         int64 `json:"prompt_tokens"`
		CompletionTokens     int64 `json:"completion_tokens"`
		CacheTokens          int64 `json:"cache_tokens"`
		ExactUsageRequests   int64 `json:"exact_usage_requests"`
		PartialUsageRequests int64 `json:"partial_usage_requests"`
		MissingUsageRequests int64 `json:"missing_usage_requests"`
	}
	if err := json.Unmarshal(raw, &stats); err != nil {
		t.Fatalf("unmarshal usage: %v", err)
	}
	if stats.PromptTokens != 11 || stats.CompletionTokens != 7 || stats.CacheTokens != 5 {
		t.Fatalf("unexpected usage stats: %+v", stats)
	}
}

func TestCollectMetricsDataIncludesRealtimeOutputAndFreshness(t *testing.T) {
	now := time.Now()
	base := now.Add(-2 * time.Second)
	store := NewDashboardMetricsStore(2*time.Second, 3)
	store.Update(DashboardCounterSample{
		Timestamp:        base,
		Requests:         10,
		Failures:         1,
		Tokens:           150,
		PromptTokens:     90,
		CompletionTokens: 60,
		CacheTokens:      12,
		CompletionByProv: map[string]float64{"provider-a": 60},
		RouteReqs:        map[string]float64{"/snapshot-metrics-test": 10},
		RouteFails:       map[string]float64{"/snapshot-metrics-test": 1},
		RouteCompletions: map[string]float64{"/snapshot-metrics-test": 60},
	})
	store.Update(DashboardCounterSample{
		Timestamp:        now,
		Requests:         12,
		Failures:         1,
		Tokens:           186,
		PromptTokens:     108,
		CompletionTokens: 78,
		CacheTokens:      18,
		CompletionByProv: map[string]float64{"provider-a": 78},
		RouteReqs:        map[string]float64{"/snapshot-metrics-test": 12},
		RouteFails:       map[string]float64{"/snapshot-metrics-test": 1},
		RouteCompletions: map[string]float64{"/snapshot-metrics-test": 78},
	})

	tracker := NewOutputRateTracker(8 * time.Second)
	labels := Labels{
		Route:         "/snapshot-metrics-test",
		Protocol:      config.RouteProtocolChat,
		Provider:      "provider-a",
		RouteModel:    "gpt-4o",
		ProviderModel: "gpt-4o",
		Endpoint:      "chat/completions",
	}
	tracker.Record(labels, "completion", 9, now)

	payload := CollectMetricsData(nil, tracker, store)

	realtimeRaw, ok := payload["realtime"]
	if !ok {
		t.Fatalf("payload missing realtime: %#v", payload)
	}
	realtime, ok := realtimeRaw.(DashboardRealtimeSnapshot)
	if !ok {
		t.Fatalf("realtime type = %T, want DashboardRealtimeSnapshot", realtimeRaw)
	}
	if len(realtime.Output) != 1 {
		t.Fatalf("realtime output points = %d, want 1", len(realtime.Output))
	}
	if got := realtime.Output[0].PromptTPS; got != 9 {
		t.Fatalf("prompt_tps = %.3f, want 9", got)
	}
	if got := realtime.Output[0].CompletionTPS; got != 9 {
		t.Fatalf("completion_tps = %.3f, want 9", got)
	}
	if got := realtime.Output[0].CacheTPS; got != 3 {
		t.Fatalf("cache_tps = %.3f, want 3", got)
	}
	if got := realtime.Output[0].Providers["provider-a"]; got != 9 {
		t.Fatalf("provider-a completion_tps = %.3f, want 9", got)
	}
	if got := realtime.Routes.Output[0].Routes["/snapshot-metrics-test"]; got != 9 {
		t.Fatalf("route completion_tps = %.3f, want 9", got)
	}

	providerRatesRaw, ok := payload["provider_token_rate"]
	if !ok {
		t.Fatalf("payload missing provider_token_rate: %#v", payload)
	}
	raw, err := json.Marshal(providerRatesRaw)
	if err != nil {
		t.Fatalf("marshal provider_token_rate: %v", err)
	}
	var rows []struct {
		Route          string  `json:"route,omitempty"`
		Protocol       string  `json:"protocol,omitempty"`
		RouteModel     string  `json:"route_model,omitempty"`
		MatchedPattern string  `json:"matched_pattern,omitempty"`
		Provider       string  `json:"provider,omitempty"`
		ProviderModel  string  `json:"provider_model,omitempty"`
		Model          string  `json:"model,omitempty"`
		Endpoint       string  `json:"endpoint"`
		Type           string  `json:"type"`
		Value          float64 `json:"value"`
		LastUpdatedMs  int64   `json:"last_updated_ms"`
		ExpiresAtMs    int64   `json:"expires_at_ms"`
		FreshForMs     int64   `json:"fresh_for_ms"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("unmarshal provider_token_rate: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("provider_token_rate is empty")
	}
	var matched bool
	for _, row := range rows {
		if row.Provider != "provider-a" || row.Type != "completion" || row.Route != "/snapshot-metrics-test" {
			continue
		}
		matched = true
		if row.LastUpdatedMs == 0 || row.ExpiresAtMs == 0 {
			t.Fatalf("freshness timestamps missing: %+v", row)
		}
		if row.ExpiresAtMs <= row.LastUpdatedMs {
			t.Fatalf("expires_at_ms = %d, want > last_updated_ms %d", row.ExpiresAtMs, row.LastUpdatedMs)
		}
		if row.FreshForMs < 0 {
			t.Fatalf("fresh_for_ms = %d, want >= 0", row.FreshForMs)
		}
	}
	if !matched {
		t.Fatalf("provider_token_rate missing matching completion row: %+v", rows)
	}
}
