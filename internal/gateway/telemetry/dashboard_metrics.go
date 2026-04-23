package telemetry

import (
	"context"
	"sync"
	"time"
)

type DashboardCounterSample struct {
	Timestamp        time.Time
	Requests         float64
	Failures         float64
	Tokens           float64
	PromptTokens     float64
	CompletionTokens float64
	CacheTokens      float64
	CompletionByProv map[string]float64
	RouteReqs        map[string]float64
	RouteFails       map[string]float64
	RouteCompletions map[string]float64
	Failovers        float64
	StreamErrors     float64
}

type DashboardUsagePoint struct {
	TS        int64   `json:"ts"`
	ReqPerMin float64 `json:"req_per_min"`
	TokPerMin float64 `json:"tok_per_min"`
}

type DashboardErrorPoint struct {
	TS             int64   `json:"ts"`
	ErrorRate      float64 `json:"error_rate"`
	FailoverPer1K  float64 `json:"failover_per_1k"`
	StreamErrPer1K float64 `json:"stream_err_per_1k"`
}

type DashboardOutputPoint struct {
	TS            int64              `json:"ts"`
	PromptTPS     float64            `json:"prompt_tps"`
	CompletionTPS float64            `json:"completion_tps"`
	CacheTPS      float64            `json:"cache_tps"`
	Providers     map[string]float64 `json:"providers,omitempty"`
}

type DashboardRoutePoint struct {
	TS     int64              `json:"ts"`
	Routes map[string]float64 `json:"routes,omitempty"`
}

type DashboardRouteRealtimeSnapshot struct {
	Requests []DashboardRoutePoint `json:"requests"`
	Output   []DashboardRoutePoint `json:"output"`
	Errors   []DashboardRoutePoint `json:"errors"`
}

type DashboardRealtimeSnapshot struct {
	SampleIntervalMs int                            `json:"sample_interval_ms"`
	WindowSeconds    int                            `json:"window_seconds"`
	Usage            []DashboardUsagePoint          `json:"usage"`
	Output           []DashboardOutputPoint         `json:"output"`
	Errors           []DashboardErrorPoint          `json:"errors"`
	Routes           DashboardRouteRealtimeSnapshot `json:"routes"`
}

type DashboardMetricsStore struct {
	mu             sync.RWMutex
	sampleInterval time.Duration
	historyLimit   int
	baseline       *DashboardCounterSample
	usage          []DashboardUsagePoint
	output         []DashboardOutputPoint
	errors         []DashboardErrorPoint
	routeRequests  []DashboardRoutePoint
	routeOutput    []DashboardRoutePoint
	routeErrors    []DashboardRoutePoint
}

func NewDashboardMetricsStore(sampleInterval time.Duration, historyLimit int) *DashboardMetricsStore {
	return &DashboardMetricsStore{
		sampleInterval: sampleInterval,
		historyLimit:   historyLimit,
	}
}

func (s *DashboardMetricsStore) Start(ctx context.Context, collect func() DashboardCounterSample) {
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

func (s *DashboardMetricsStore) Update(sample DashboardCounterSample) {
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
	deltaPromptTokens := sample.PromptTokens - s.baseline.PromptTokens
	deltaCompletionTokens := sample.CompletionTokens - s.baseline.CompletionTokens
	deltaCacheTokens := sample.CacheTokens - s.baseline.CacheTokens
	deltaFailovers := sample.Failovers - s.baseline.Failovers
	deltaStreamErrors := sample.StreamErrors - s.baseline.StreamErrors
	if deltaRequests < 0 || deltaFailures < 0 || deltaTokens < 0 || deltaPromptTokens < 0 || deltaCompletionTokens < 0 || deltaCacheTokens < 0 || deltaFailovers < 0 || deltaStreamErrors < 0 {
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

	deltaCompletionByProv, rollback := diffCounterMap(sample.CompletionByProv, s.baseline.CompletionByProv)
	if rollback {
		s.clearLocked()
		s.resetBaselineLocked(sample)
		return
	}

	deltaRouteCompletions, rollback := diffCounterMap(sample.RouteCompletions, s.baseline.RouteCompletions)
	if rollback {
		s.clearLocked()
		s.resetBaselineLocked(sample)
		return
	}

	scale := time.Minute.Seconds() / elapsed.Seconds()
	tpsScale := 1 / elapsed.Seconds()
	usage := DashboardUsagePoint{
		TS:        sample.Timestamp.UnixMilli(),
		ReqPerMin: deltaRequests * scale,
		TokPerMin: deltaTokens * scale,
	}
	errors := DashboardErrorPoint{
		TS:             sample.Timestamp.UnixMilli(),
		ErrorRate:      ratioPercent(deltaFailures, deltaRequests),
		FailoverPer1K:  ratioPer1K(deltaFailovers, deltaRequests),
		StreamErrPer1K: ratioPer1K(deltaStreamErrors, deltaRequests),
	}
	output := DashboardOutputPoint{
		TS:            sample.Timestamp.UnixMilli(),
		PromptTPS:     deltaPromptTokens * tpsScale,
		CompletionTPS: deltaCompletionTokens * tpsScale,
		CacheTPS:      deltaCacheTokens * tpsScale,
		Providers:     scaleFloatMap(deltaCompletionByProv, tpsScale),
	}
	routeRequests := DashboardRoutePoint{
		TS:     sample.Timestamp.UnixMilli(),
		Routes: scaleFloatMap(deltaRouteReqs, scale),
	}
	routeOutput := DashboardRoutePoint{
		TS:     sample.Timestamp.UnixMilli(),
		Routes: scaleFloatMap(deltaRouteCompletions, tpsScale),
	}
	routeErrors := DashboardRoutePoint{
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

func (s *DashboardMetricsStore) Snapshot() DashboardRealtimeSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	usage := make([]DashboardUsagePoint, len(s.usage))
	copy(usage, s.usage)

	output := make([]DashboardOutputPoint, len(s.output))
	copy(output, s.output)
	for i := range output {
		output[i].Providers = cloneFloatMap(output[i].Providers)
	}

	errors := make([]DashboardErrorPoint, len(s.errors))
	copy(errors, s.errors)

	routeRequests := cloneRoutePoints(s.routeRequests)
	routeOutput := cloneRoutePoints(s.routeOutput)
	routeErrors := cloneRoutePoints(s.routeErrors)

	return DashboardRealtimeSnapshot{
		SampleIntervalMs: int(s.sampleInterval.Milliseconds()),
		WindowSeconds:    int(s.sampleInterval.Seconds()) * s.historyLimit,
		Usage:            usage,
		Output:           output,
		Errors:           errors,
		Routes: DashboardRouteRealtimeSnapshot{
			Requests: routeRequests,
			Output:   routeOutput,
			Errors:   routeErrors,
		},
	}
}

func (s *DashboardMetricsStore) clearLocked() {
	s.usage = nil
	s.output = nil
	s.errors = nil
	s.routeRequests = nil
	s.routeOutput = nil
	s.routeErrors = nil
}

func (s *DashboardMetricsStore) resetBaselineLocked(sample DashboardCounterSample) {
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
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items
}

func cloneFloatMap(src map[string]float64) map[string]float64 {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]float64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneCounterSample(sample DashboardCounterSample) *DashboardCounterSample {
	cloned := sample
	cloned.CompletionByProv = cloneFloatMap(sample.CompletionByProv)
	cloned.RouteReqs = cloneFloatMap(sample.RouteReqs)
	cloned.RouteFails = cloneFloatMap(sample.RouteFails)
	cloned.RouteCompletions = cloneFloatMap(sample.RouteCompletions)
	return &cloned
}

func diffCounterMap(current, baseline map[string]float64) (map[string]float64, bool) {
	if len(current) == 0 && len(baseline) == 0 {
		return nil, false
	}
	diff := make(map[string]float64, len(current))
	for key, value := range current {
		base := baseline[key]
		delta := value - base
		if delta < 0 {
			return nil, true
		}
		if delta > 0 {
			diff[key] = delta
		}
	}
	for key, base := range baseline {
		if _, ok := current[key]; !ok && base > 0 {
			return nil, true
		}
	}
	return diff, false
}

func scaleFloatMap(src map[string]float64, scale float64) map[string]float64 {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]float64, len(src))
	for k, v := range src {
		dst[k] = v * scale
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

func cloneRoutePoints(src []DashboardRoutePoint) []DashboardRoutePoint {
	if len(src) == 0 {
		return nil
	}
	dst := make([]DashboardRoutePoint, len(src))
	copy(dst, src)
	for i := range dst {
		dst[i].Routes = cloneFloatMap(dst[i].Routes)
	}
	return dst
}
