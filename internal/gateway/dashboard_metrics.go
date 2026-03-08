package gateway

import (
	"context"
	"sync"
	"time"
)

type dashboardCounterSample struct {
	Timestamp    time.Time
	Requests     float64
	Failures     float64
	Tokens       float64
	OutputRate   float64
	OutputByProv map[string]float64
	RouteReqs    map[string]float64
	RouteFails   map[string]float64
	RouteOutput  map[string]float64
	Failovers    float64
	StreamErrors float64
}

type dashboardUsagePoint struct {
	TS        int64   `json:"ts"`
	ReqPerMin float64 `json:"req_per_min"`
	TokPerMin float64 `json:"tok_per_min"`
}

type dashboardErrorPoint struct {
	TS             int64   `json:"ts"`
	ErrorRate      float64 `json:"error_rate"`
	FailoverPer1K  float64 `json:"failover_per_1k"`
	StreamErrPer1K float64 `json:"stream_err_per_1k"`
}

type dashboardOutputPoint struct {
	TS            int64              `json:"ts"`
	CompletionTPS float64            `json:"completion_tps"`
	Providers     map[string]float64 `json:"providers,omitempty"`
}

type dashboardRoutePoint struct {
	TS     int64              `json:"ts"`
	Routes map[string]float64 `json:"routes,omitempty"`
}

type dashboardRouteRealtimeSnapshot struct {
	Requests []dashboardRoutePoint `json:"requests"`
	Output   []dashboardRoutePoint `json:"output"`
	Errors   []dashboardRoutePoint `json:"errors"`
}

type dashboardRealtimeSnapshot struct {
	SampleIntervalMs int                            `json:"sample_interval_ms"`
	WindowSeconds    int                            `json:"window_seconds"`
	Usage            []dashboardUsagePoint          `json:"usage"`
	Output           []dashboardOutputPoint         `json:"output"`
	Errors           []dashboardErrorPoint          `json:"errors"`
	Routes           dashboardRouteRealtimeSnapshot `json:"routes"`
}

type dashboardMetricsStore struct {
	mu             sync.RWMutex
	sampleInterval time.Duration
	historyLimit   int
	baseline       *dashboardCounterSample
	usage          []dashboardUsagePoint
	output         []dashboardOutputPoint
	errors         []dashboardErrorPoint
	routeRequests  []dashboardRoutePoint
	routeOutput    []dashboardRoutePoint
	routeErrors    []dashboardRoutePoint
}

func newDashboardMetricsStore(sampleInterval time.Duration, historyLimit int) *dashboardMetricsStore {
	return &dashboardMetricsStore{
		sampleInterval: sampleInterval,
		historyLimit:   historyLimit,
	}
}

func (s *dashboardMetricsStore) Start(ctx context.Context, collect func() dashboardCounterSample) {
	s.Update(collect())

	go func() {
		ticker := time.NewTicker(s.sampleInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.Update(collect())
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (s *dashboardMetricsStore) Update(sample dashboardCounterSample) {
	if sample.Timestamp.IsZero() {
		sample.Timestamp = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.baseline == nil {
		s.resetBaselineLocked(sample)
		return
	}

	elapsed := sample.Timestamp.Sub(s.baseline.Timestamp)
	if elapsed < s.sampleInterval {
		return
	}
	if elapsed <= 0 {
		s.clearLocked()
		s.resetBaselineLocked(sample)
		return
	}

	deltaRequests := sample.Requests - s.baseline.Requests
	deltaFailures := sample.Failures - s.baseline.Failures
	deltaTokens := sample.Tokens - s.baseline.Tokens
	deltaFailovers := sample.Failovers - s.baseline.Failovers
	deltaStreamErrors := sample.StreamErrors - s.baseline.StreamErrors
	if deltaRequests < 0 || deltaFailures < 0 || deltaTokens < 0 || deltaFailovers < 0 || deltaStreamErrors < 0 {
		s.clearLocked()
		s.resetBaselineLocked(sample)
		return
	}

	deltaRouteReqs, rollback := diffCounterMap(sample.RouteReqs, s.baseline.RouteReqs)
	if rollback {
		s.clearLocked()
		s.resetBaselineLocked(sample)
		return
	}

	deltaRouteFails, rollback := diffCounterMap(sample.RouteFails, s.baseline.RouteFails)
	if rollback {
		s.clearLocked()
		s.resetBaselineLocked(sample)
		return
	}

	scale := time.Minute.Seconds() / elapsed.Seconds()
	usage := dashboardUsagePoint{
		TS:        sample.Timestamp.UnixMilli(),
		ReqPerMin: deltaRequests * scale,
		TokPerMin: deltaTokens * scale,
	}
	errors := dashboardErrorPoint{
		TS:             sample.Timestamp.UnixMilli(),
		ErrorRate:      ratioPercent(deltaFailures, deltaRequests),
		FailoverPer1K:  ratioPer1K(deltaFailovers, deltaRequests),
		StreamErrPer1K: ratioPer1K(deltaStreamErrors, deltaRequests),
	}
	output := dashboardOutputPoint{
		TS:            sample.Timestamp.UnixMilli(),
		CompletionTPS: sample.OutputRate,
		Providers:     cloneFloatMap(sample.OutputByProv),
	}
	routeRequests := dashboardRoutePoint{
		TS:     sample.Timestamp.UnixMilli(),
		Routes: scaleFloatMap(deltaRouteReqs, scale),
	}
	routeOutput := dashboardRoutePoint{
		TS:     sample.Timestamp.UnixMilli(),
		Routes: cloneFloatMap(sample.RouteOutput),
	}
	routeErrors := dashboardRoutePoint{
		TS:     sample.Timestamp.UnixMilli(),
		Routes: ratioPercentMap(deltaRouteFails, deltaRouteReqs),
	}

	s.usage = appendWithLimit(s.usage, usage, s.historyLimit)
	s.output = appendWithLimit(s.output, output, s.historyLimit)
	s.errors = appendWithLimit(s.errors, errors, s.historyLimit)
	s.routeRequests = appendWithLimit(s.routeRequests, routeRequests, s.historyLimit)
	s.routeOutput = appendWithLimit(s.routeOutput, routeOutput, s.historyLimit)
	s.routeErrors = appendWithLimit(s.routeErrors, routeErrors, s.historyLimit)
	s.resetBaselineLocked(sample)
}

func (s *dashboardMetricsStore) Snapshot() dashboardRealtimeSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	usage := make([]dashboardUsagePoint, len(s.usage))
	copy(usage, s.usage)

	output := make([]dashboardOutputPoint, len(s.output))
	copy(output, s.output)
	for i := range output {
		output[i].Providers = cloneFloatMap(output[i].Providers)
	}

	errors := make([]dashboardErrorPoint, len(s.errors))
	copy(errors, s.errors)

	routeRequests := cloneRoutePoints(s.routeRequests)
	routeOutput := cloneRoutePoints(s.routeOutput)
	routeErrors := cloneRoutePoints(s.routeErrors)

	return dashboardRealtimeSnapshot{
		SampleIntervalMs: int(s.sampleInterval.Milliseconds()),
		WindowSeconds:    int(s.sampleInterval.Seconds()) * s.historyLimit,
		Usage:            usage,
		Output:           output,
		Errors:           errors,
		Routes: dashboardRouteRealtimeSnapshot{
			Requests: routeRequests,
			Output:   routeOutput,
			Errors:   routeErrors,
		},
	}
}

func (s *dashboardMetricsStore) clearLocked() {
	s.usage = nil
	s.output = nil
	s.errors = nil
	s.routeRequests = nil
	s.routeOutput = nil
	s.routeErrors = nil
}

func (s *dashboardMetricsStore) resetBaselineLocked(sample dashboardCounterSample) {
	s.baseline = cloneCounterSample(sample)
}

func ratioPercent(part, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return part / total * 100
}

func ratioPer1K(part, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return part / total * 1000
}

func appendWithLimit[T any](items []T, item T, limit int) []T {
	items = append(items, item)
	if len(items) <= limit {
		return items
	}
	return items[len(items)-limit:]
}

func cloneFloatMap(src map[string]float64) map[string]float64 {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]float64, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func cloneCounterSample(sample dashboardCounterSample) *dashboardCounterSample {
	cp := sample
	cp.OutputByProv = cloneFloatMap(sample.OutputByProv)
	cp.RouteReqs = cloneFloatMap(sample.RouteReqs)
	cp.RouteFails = cloneFloatMap(sample.RouteFails)
	cp.RouteOutput = cloneFloatMap(sample.RouteOutput)
	return &cp
}

func diffCounterMap(current, baseline map[string]float64) (map[string]float64, bool) {
	if len(current) == 0 && len(baseline) == 0 {
		return nil, false
	}

	out := make(map[string]float64, len(current))
	for key, value := range current {
		delta := value - baseline[key]
		if delta < 0 {
			return nil, true
		}
		if delta > 0 {
			out[key] = delta
		}
	}

	for key, value := range baseline {
		if _, ok := current[key]; ok {
			continue
		}
		if value > 0 {
			return nil, true
		}
	}

	if len(out) == 0 {
		return nil, false
	}
	return out, false
}

func scaleFloatMap(src map[string]float64, scale float64) map[string]float64 {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]float64, len(src))
	for key, value := range src {
		dst[key] = value * scale
	}
	return dst
}

func ratioPercentMap(parts, totals map[string]float64) map[string]float64 {
	if len(totals) == 0 {
		return nil
	}
	dst := make(map[string]float64, len(totals))
	for key, total := range totals {
		dst[key] = ratioPercent(parts[key], total)
	}
	return dst
}

func cloneRoutePoints(src []dashboardRoutePoint) []dashboardRoutePoint {
	if len(src) == 0 {
		return nil
	}
	dst := make([]dashboardRoutePoint, len(src))
	copy(dst, src)
	for i := range dst {
		dst[i].Routes = cloneFloatMap(dst[i].Routes)
	}
	return dst
}
