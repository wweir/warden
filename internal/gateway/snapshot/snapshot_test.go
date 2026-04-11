package snapshot

import (
	"encoding/json"
	"testing"

	"github.com/wweir/warden/config"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
)

func TestListAPIKeysPayloadIncludesCacheTokens(t *testing.T) {
	route := "/snapshot-cache-test"
	apiKey := "cache-client"

	telemetrypkg.APIKeyRequestCounter.WithLabelValues(apiKey, route, config.RouteProtocolChat, "gpt-4o", "", "chat/completions", "success").Add(2)
	telemetrypkg.APIKeyTokenCounter.WithLabelValues(apiKey, route, config.RouteProtocolChat, "gpt-4o", "", "prompt").Add(11)
	telemetrypkg.APIKeyTokenCounter.WithLabelValues(apiKey, route, config.RouteProtocolChat, "gpt-4o", "", "completion").Add(7)
	telemetrypkg.APIKeyTokenCounter.WithLabelValues(apiKey, route, config.RouteProtocolChat, "gpt-4o", "", "cache").Add(5)

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
