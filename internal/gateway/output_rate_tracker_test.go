package gateway

import (
	"testing"
	"time"

	"github.com/wweir/warden/config"
)

func TestOutputRateTrackerSnapshotDropsStaleEntries(t *testing.T) {
	tracker := newOutputRateTracker(2 * time.Second)
	base := time.Unix(1700000000, 0)
	labels := requestMetricLabels{Route: "/openai", Protocol: config.RouteProtocolChat, Provider: "p1", RouteModel: "gpt", ProviderModel: "gpt", Endpoint: "chat/completions"}

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
	g := &Gateway{outputRates: newOutputRateTracker(2 * time.Second)}
	now := time.Now()
	labels := requestMetricLabels{Route: "/openai", Protocol: config.RouteProtocolChat, Provider: "p1", RouteModel: "gpt", ProviderModel: "gpt", Endpoint: "chat/completions"}

	g.outputRates.Record(labels, "completion", 18, now)
	g.outputRates.Record(labels, "prompt", 9, now)

	sample := g.collectDashboardCounters()
	assertApprox(t, sample.OutputRate, 18)
	assertApprox(t, sample.OutputByProv["p1"], 18)
	assertApprox(t, sample.RouteOutput["/openai"], 18)

	g.outputRates.Record(labels, "completion", 18, now.Add(-3*time.Second))
	g.outputRates.Record(labels, "prompt", 9, now.Add(-3*time.Second))

	sample = g.collectDashboardCounters()
	assertApprox(t, sample.OutputRate, 0)
	if _, ok := sample.OutputByProv["p1"]; ok {
		t.Fatalf("expected stale provider output to be dropped")
	}
	if _, ok := sample.RouteOutput["/openai"]; ok {
		t.Fatalf("expected stale route output to be dropped")
	}
}
