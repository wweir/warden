package gateway

import (
	"math"
	"testing"
	"time"
)

func TestDashboardMetricsStoreUpdate(t *testing.T) {
	store := newDashboardMetricsStore(2*time.Second, 3)
	base := time.Unix(1700000000, 0)

	store.Update(dashboardCounterSample{
		Timestamp:    base,
		Requests:     100,
		Failures:     5,
		Tokens:       1000,
		OutputRate:   24,
		OutputByProv: map[string]float64{"openai": 14, "anthropic": 10},
		RouteReqs:    map[string]float64{"/openai": 70, "/claude": 30},
		RouteFails:   map[string]float64{"/openai": 3, "/claude": 2},
		RouteOutput:  map[string]float64{"/openai": 16, "/claude": 8},
		Failovers:    1,
		StreamErrors: 2,
	})
	store.Update(dashboardCounterSample{
		Timestamp:    base.Add(2 * time.Second),
		Requests:     104,
		Failures:     6,
		Tokens:       1120,
		OutputRate:   30,
		OutputByProv: map[string]float64{"openai": 18, "anthropic": 12},
		RouteReqs:    map[string]float64{"/openai": 72, "/claude": 32},
		RouteFails:   map[string]float64{"/openai": 3, "/claude": 3},
		RouteOutput:  map[string]float64{"/openai": 19, "/claude": 11},
		Failovers:    2,
		StreamErrors: 3,
	})

	snapshot := store.Snapshot()
	if len(snapshot.Usage) != 1 {
		t.Fatalf("expected 1 usage point, got %d", len(snapshot.Usage))
	}
	if len(snapshot.Errors) != 1 {
		t.Fatalf("expected 1 error point, got %d", len(snapshot.Errors))
	}
	if len(snapshot.Output) != 1 {
		t.Fatalf("expected 1 output point, got %d", len(snapshot.Output))
	}
	if len(snapshot.Routes.Requests) != 1 {
		t.Fatalf("expected 1 route request point, got %d", len(snapshot.Routes.Requests))
	}
	if len(snapshot.Routes.Output) != 1 {
		t.Fatalf("expected 1 route output point, got %d", len(snapshot.Routes.Output))
	}
	if len(snapshot.Routes.Errors) != 1 {
		t.Fatalf("expected 1 route error point, got %d", len(snapshot.Routes.Errors))
	}

	assertApprox(t, snapshot.Usage[0].ReqPerMin, 120)
	assertApprox(t, snapshot.Usage[0].TokPerMin, 3600)
	assertApprox(t, snapshot.Output[0].CompletionTPS, 30)
	assertApprox(t, snapshot.Output[0].Providers["openai"], 18)
	assertApprox(t, snapshot.Output[0].Providers["anthropic"], 12)
	assertApprox(t, snapshot.Errors[0].ErrorRate, 25)
	assertApprox(t, snapshot.Errors[0].FailoverPer1K, 250)
	assertApprox(t, snapshot.Errors[0].StreamErrPer1K, 250)
	assertApprox(t, snapshot.Routes.Requests[0].Routes["/openai"], 60)
	assertApprox(t, snapshot.Routes.Requests[0].Routes["/claude"], 60)
	assertApprox(t, snapshot.Routes.Output[0].Routes["/openai"], 19)
	assertApprox(t, snapshot.Routes.Output[0].Routes["/claude"], 11)
	assertApprox(t, snapshot.Routes.Errors[0].Routes["/openai"], 0)
	assertApprox(t, snapshot.Routes.Errors[0].Routes["/claude"], 50)
}

func TestDashboardMetricsStoreResetOnCounterRollback(t *testing.T) {
	store := newDashboardMetricsStore(2*time.Second, 3)
	base := time.Unix(1700000000, 0)

	store.Update(dashboardCounterSample{
		Timestamp: base,
		Requests:  10,
		Tokens:    100,
		RouteReqs: map[string]float64{"/openai": 10},
	})
	store.Update(dashboardCounterSample{
		Timestamp: base.Add(2 * time.Second),
		Requests:  12,
		Tokens:    140,
		RouteReqs: map[string]float64{"/openai": 12},
	})
	if got := len(store.Snapshot().Usage); got != 1 {
		t.Fatalf("expected history before reset, got %d", got)
	}

	store.Update(dashboardCounterSample{
		Timestamp: base.Add(4 * time.Second),
		Requests:  3,
		Tokens:    20,
		RouteReqs: map[string]float64{"/openai": 3},
	})
	snapshot := store.Snapshot()
	if len(snapshot.Usage) != 0 {
		t.Fatalf("expected usage history to be cleared after rollback, got %d", len(snapshot.Usage))
	}
	if len(snapshot.Output) != 0 {
		t.Fatalf("expected output history to be cleared after rollback, got %d", len(snapshot.Output))
	}
	if len(snapshot.Errors) != 0 {
		t.Fatalf("expected error history to be cleared after rollback, got %d", len(snapshot.Errors))
	}
	if len(snapshot.Routes.Requests) != 0 {
		t.Fatalf("expected route request history to be cleared after rollback, got %d", len(snapshot.Routes.Requests))
	}
}

func TestDashboardMetricsStoreResetOnRouteCounterRollback(t *testing.T) {
	store := newDashboardMetricsStore(2*time.Second, 3)
	base := time.Unix(1700000000, 0)

	store.Update(dashboardCounterSample{
		Timestamp: base,
		Requests:  20,
		RouteReqs: map[string]float64{"/openai": 20},
	})
	store.Update(dashboardCounterSample{
		Timestamp: base.Add(2 * time.Second),
		Requests:  22,
		RouteReqs: map[string]float64{"/openai": 22},
	})
	if got := len(store.Snapshot().Routes.Requests); got != 1 {
		t.Fatalf("expected route history before reset, got %d", got)
	}

	store.Update(dashboardCounterSample{
		Timestamp: base.Add(4 * time.Second),
		Requests:  25,
		RouteReqs: map[string]float64{"/openai": 5},
	})
	snapshot := store.Snapshot()
	if len(snapshot.Routes.Requests) != 0 {
		t.Fatalf("expected route history to be cleared after route rollback, got %d", len(snapshot.Routes.Requests))
	}
}

func TestDashboardMetricsStoreHistoryLimit(t *testing.T) {
	store := newDashboardMetricsStore(2*time.Second, 2)
	base := time.Unix(1700000000, 0)

	store.Update(dashboardCounterSample{Timestamp: base, Requests: 10, Tokens: 100, RouteReqs: map[string]float64{"/openai": 10}})
	store.Update(dashboardCounterSample{Timestamp: base.Add(2 * time.Second), Requests: 11, Tokens: 120, RouteReqs: map[string]float64{"/openai": 11}})
	store.Update(dashboardCounterSample{Timestamp: base.Add(4 * time.Second), Requests: 12, Tokens: 150, RouteReqs: map[string]float64{"/openai": 12}})
	store.Update(dashboardCounterSample{Timestamp: base.Add(6 * time.Second), Requests: 13, Tokens: 190, RouteReqs: map[string]float64{"/openai": 13}})

	snapshot := store.Snapshot()
	if len(snapshot.Usage) != 2 {
		t.Fatalf("expected usage history to be capped at 2, got %d", len(snapshot.Usage))
	}
	if len(snapshot.Routes.Requests) != 2 {
		t.Fatalf("expected route request history to be capped at 2, got %d", len(snapshot.Routes.Requests))
	}
	if snapshot.Usage[0].TS != base.Add(4*time.Second).UnixMilli() {
		t.Fatalf("expected oldest point to be trimmed, got %d", snapshot.Usage[0].TS)
	}
}

func assertApprox(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.001 {
		t.Fatalf("expected %.3f, got %.3f", want, got)
	}
}
