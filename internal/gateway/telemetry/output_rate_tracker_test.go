package telemetry_test

import (
	"testing"
	"time"

	"github.com/wweir/warden/config"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
)

func TestOutputRateTrackerSnapshotDropsStaleEntries(t *testing.T) {
	tracker := telemetrypkg.NewOutputRateTracker(2 * time.Second)
	base := time.Unix(1700000000, 0)
	labels := telemetrypkg.Labels{Route: "/openai", Protocol: config.RouteProtocolChat, Provider: "p1", RouteModel: "gpt", ProviderModel: "gpt", Endpoint: "chat/completions"}

	tracker.Record(labels, "completion", 12, base)

	entries := tracker.Snapshot(base.Add(2 * time.Second))
	if len(entries) != 1 {
		t.Fatalf("expected entry at freshness boundary, got %d", len(entries))
	}

	entries = tracker.Snapshot(base.Add(2*time.Second + time.Millisecond))
	if len(entries) != 0 {
		t.Fatalf("expected stale entry to be dropped, got %d", len(entries))
	}
}

func TestCollectDashboardCountersDropsIdleOutputRate(t *testing.T) {
	base := telemetrypkg.CollectDashboardCounters()
	route := "/snapshot-output-test"
	provider := "snapshot-provider"

	telemetrypkg.RouteTokenCounter.WithLabelValues(route, config.RouteProtocolChat, "gpt-4o", "", "prompt").Add(12)
	telemetrypkg.RouteTokenCounter.WithLabelValues(route, config.RouteProtocolChat, "gpt-4o", "", "completion").Add(8)
	telemetrypkg.RouteTokenCounter.WithLabelValues(route, config.RouteProtocolChat, "gpt-4o", "", "cache").Add(5)
	telemetrypkg.ProviderTokenCounter.WithLabelValues(provider, "gpt-4o", route, "gpt-4o", "", "completion").Add(8)

	sample := telemetrypkg.CollectDashboardCounters()
	assertApprox(t, sample.PromptTokens-base.PromptTokens, 12)
	assertApprox(t, sample.CompletionTokens-base.CompletionTokens, 8)
	assertApprox(t, sample.CacheTokens-base.CacheTokens, 5)
	assertApprox(t, sample.Tokens-base.Tokens, 20)
	assertApprox(t, sample.CompletionByProv[provider]-base.CompletionByProv[provider], 8)
	assertApprox(t, sample.RouteCompletions[route]-base.RouteCompletions[route], 8)
}

func TestCollectDashboardCountersExcludesCacheTokensFromUsageRate(t *testing.T) {
	base := telemetrypkg.CollectDashboardCounters()

	telemetrypkg.RouteTokenCounter.WithLabelValues(
		"/snapshot-cache-test",
		config.RouteProtocolChat,
		"gpt-4o",
		"",
		"prompt",
	).Add(11)
	telemetrypkg.RouteTokenCounter.WithLabelValues(
		"/snapshot-cache-test",
		config.RouteProtocolChat,
		"gpt-4o",
		"",
		"completion",
	).Add(7)
	telemetrypkg.RouteTokenCounter.WithLabelValues(
		"/snapshot-cache-test",
		config.RouteProtocolChat,
		"gpt-4o",
		"",
		"cache",
	).Add(5)

	sample := telemetrypkg.CollectDashboardCounters()
	if got := sample.Tokens - base.Tokens; got != 18 {
		t.Fatalf("token delta = %.0f, want 18", got)
	}
}
