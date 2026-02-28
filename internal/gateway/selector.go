package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wweir/warden/config"
)

const (
	// baseSuppressDuration is the initial suppression duration after first failure.
	baseSuppressDuration = 30 * time.Second
	// maxConsecutiveFailures caps the exponential backoff at 2^4 * 30s = 480s.
	maxConsecutiveFailures = 5
	// outcomeWindowSize is the number of recent outcomes to keep for sliding window stats.
	outcomeWindowSize = 1000
	// maxSuppressReasons is the max number of recent suppress reasons to keep.
	maxSuppressReasons = 20
	// suppressReasonTTL is the duration to keep suppress reasons.
	suppressReasonTTL = time.Hour
)

// outcome represents a single request outcome for sliding window statistics.
type outcome struct {
	timestamp   time.Time
	success     bool
	latencyMs   int64
	errorSource string // "pre_stream", "in_stream", or "" for non-stream
}

// SuppressReason records the cause and time of a provider suppression event.
type SuppressReason struct {
	Time   time.Time `json:"time"`
	Reason string    `json:"reason"`
}

// providerState tracks runtime health state for a single provider.
type providerState struct {
	consecutiveFailures int
	suppressUntil       time.Time
	availableModels     map[string]bool   // nil = unknown (fetch failed), don't filter
	rawModels           []json.RawMessage // raw model objects from GET /models

	// sliding window outcomes (ring buffer)
	outcomes     []outcome
	outcomeStart int // index of oldest entry
	outcomeCount int // total count (for ring buffer positioning)

	// recent suppress reasons (bounded, TTL-evicted)
	suppressReasons []SuppressReason

	// error source counters
	preStreamErrors  int64 // errors before first stream packet
	inStreamErrors   int64 // errors after first stream packet
	failoverCount    int64 // number of times this provider triggered a failover
}

// recordOutcome records an outcome in the sliding window.
func (s *providerState) recordOutcome(success bool, latencyMs int64, errorSource string) {
	if len(s.outcomes) < outcomeWindowSize {
		s.outcomes = append(s.outcomes, outcome{
			timestamp:   time.Now(),
			success:     success,
			latencyMs:   latencyMs,
			errorSource: errorSource,
		})
	} else {
		s.outcomes[s.outcomeStart] = outcome{
			timestamp:   time.Now(),
			success:     success,
			latencyMs:   latencyMs,
			errorSource: errorSource,
		}
		s.outcomeStart = (s.outcomeStart + 1) % outcomeWindowSize
	}
	s.outcomeCount++
}

// windowStats returns statistics for the sliding window.
func (s *providerState) windowStats() (total, success, failure int, avgLatencyMs float64) {
	total = len(s.outcomes)
	if total == 0 {
		return 0, 0, 0, 0
	}
	var totalLatency int64
	for _, o := range s.outcomes {
		if o.success {
			success++
		} else {
			failure++
		}
		totalLatency += o.latencyMs
	}
	avgLatencyMs = float64(totalLatency) / float64(total)
	return total, success, failure, avgLatencyMs
}

// Selector selects the best provider for a request based on config order,
// model matching, and failure suppression.
type Selector struct {
	mu     sync.RWMutex
	states map[string]*providerState // keyed by provider name
}

// NewSelector creates a new Selector and initializes state for all providers.
func NewSelector(cfg *config.ConfigStruct) *Selector {
	states := make(map[string]*providerState, len(cfg.Provider))
	for name := range cfg.Provider {
		states[name] = &providerState{}
	}
	return &Selector{states: states}
}

// Select returns the best provider for the given route and model.
// Selection priority:
//  1. Providers order in route config (first = highest precedence), skipping suppressed
//  2. If all suppressed, return the one whose suppression expires soonest
//
// When model is specified, candidates are filtered by availableModels (from GET /models).
// exclude contains provider names to skip (used for failover after retryable errors).
func (s *Selector) Select(cfg *config.ConfigStruct, route *config.RouteConfig, model string, exclude ...string) (*config.ProviderConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()

	// build candidates in route.Providers order (position = priority)
	type candidate struct {
		name    string
		provCfg *config.ProviderConfig
		state   *providerState
	}
	var candidates []candidate
	for _, provName := range route.Providers {
		if slices.Contains(exclude, provName) {
			continue
		}
		provCfg, exists := cfg.Provider[provName]
		if !exists {
			continue
		}
		st := s.states[provName]
		if st == nil {
			continue
		}
		if model != "" && st.availableModels != nil {
			// match both real model and aliases
			if !st.availableModels[model] && provCfg.ModelAliases[model] == "" {
				continue
			}
		}
		candidates = append(candidates, candidate{name: provName, provCfg: provCfg, state: st})
	}

	// first non-suppressed candidate by config order
	for _, c := range candidates {
		if now.After(c.state.suppressUntil) {
			return c.provCfg, nil
		}
	}

	// all suppressed — pick the one expiring soonest
	if len(candidates) > 0 {
		earliest := candidates[0]
		for _, c := range candidates[1:] {
			if c.state.suppressUntil.Before(earliest.state.suppressUntil) {
				earliest = c
			}
		}
		suppressedInfo := make([]any, 0, len(candidates)*4+2)
		suppressedInfo = append(suppressedInfo, "selected", earliest.name, "suppress_until", earliest.state.suppressUntil)
		for _, c := range candidates {
			suppressedInfo = append(suppressedInfo, c.name+"_failures", c.state.consecutiveFailures,
				c.name+"_suppress_until", c.state.suppressUntil)
		}
		slog.Warn("All providers suppressed, selecting earliest expiring", suppressedInfo...)
		return earliest.provCfg, nil
	}

	return nil, ErrProviderNotFound
}

// RecordOutcome records the result of an upstream request.
// Only retryable errors (5xx, 429, connection failures) trigger suppression.
// Client errors (4xx except 429) and successes reset the failure counter.
func (s *Selector) RecordOutcome(name string, err error, latency time.Duration) {
	s.RecordOutcomeWithSource(name, err, latency, "")
}

// RecordOutcomeWithSource records the result of an upstream request with error source tracking.
// errorSource can be "pre_stream", "in_stream", or "" for non-stream requests.
func (s *Selector) RecordOutcomeWithSource(name string, err error, latency time.Duration, errorSource string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, exists := s.states[name]
	if !exists {
		return
	}

	latencyMs := latency.Milliseconds()

	if err == nil {
		st.recordOutcome(true, latencyMs, errorSource)
		st.consecutiveFailures = 0
		st.suppressUntil = time.Time{}
		return
	}

	// only suppress on retryable errors
	if ue, ok := err.(*UpstreamError); ok && !ue.IsRetryable() {
		// 4xx client errors don't count as success or failure - just don't suppress the provider
		return
	}

	st.recordOutcome(false, latencyMs, errorSource)
	st.consecutiveFailures++
	if st.consecutiveFailures > maxConsecutiveFailures {
		st.consecutiveFailures = maxConsecutiveFailures
	}

	// track error source
	switch errorSource {
	case "pre_stream":
		st.preStreamErrors++
	case "in_stream":
		st.inStreamErrors++
	}

	// exponential backoff: 30s, 60s, 120s, 240s, 480s
	duration := baseSuppressDuration << (st.consecutiveFailures - 1)
	st.suppressUntil = time.Now().Add(duration)

	// record suppress reason
	reason := err.Error()
	if ue, ok := err.(*UpstreamError); ok {
		body := ue.Body
		if len(body) > 200 {
			body = body[:200]
		}
		reason = fmt.Sprintf("HTTP %d: %s", ue.Code, body)
	}
	now := time.Now()
	cutoff := now.Add(-suppressReasonTTL)
	// evict expired entries
	n := 0
	for _, r := range st.suppressReasons {
		if r.Time.After(cutoff) {
			st.suppressReasons[n] = r
			n++
		}
	}
	st.suppressReasons = st.suppressReasons[:n]
	// append and trim to max
	st.suppressReasons = append(st.suppressReasons, SuppressReason{Time: now, Reason: reason})
	if len(st.suppressReasons) > maxSuppressReasons {
		st.suppressReasons = st.suppressReasons[len(st.suppressReasons)-maxSuppressReasons:]
	}

	attrs := []any{
		"name", name,
		"consecutive_failures", st.consecutiveFailures,
		"suppress_duration", duration,
		"error_source", errorSource,
	}
	if ue, ok := err.(*UpstreamError); ok {
		body := ue.Body
		if len(body) > 200 {
			body = body[:200] + "..."
		}
		attrs = append(attrs, "status", ue.Code, "body", body)
	} else {
		attrs = append(attrs, "error", err)
	}
	slog.Warn("Provider suppressed", attrs...)
}

// RefreshModels queries GET /models for all providers in parallel
// and populates availableModels. Failures are logged but non-fatal
// (availableModels stays nil, meaning no filtering for that provider).
func (s *Selector) RefreshModels(cfg *config.ConfigStruct) {
	var wg sync.WaitGroup
	for name, provCfg := range cfg.Provider {
		// use statically configured models if available
		if len(provCfg.Models) > 0 {
			models := make(map[string]bool, len(provCfg.Models))
			rawModels := make([]json.RawMessage, 0, len(provCfg.Models))
			for _, id := range provCfg.Models {
				models[id] = true
				rawModels = append(rawModels, mustMarshal(map[string]string{
					"id": id, "object": "model", "owned_by": name,
				}))
			}
			s.mu.Lock()
			if st, ok := s.states[name]; ok {
				st.availableModels = models
				st.rawModels = rawModels
			}
			s.mu.Unlock()
			slog.Info("Models loaded from config", "provider", name, "count", len(models))
			continue
		}

		wg.Add(1)
		go func(name string, provCfg *config.ProviderConfig) {
			defer wg.Done()

			models, rawModels, err := fetchModels(provCfg)
			if err != nil {
				slog.Warn("Models discovery failed, model filter disabled for this provider; set 'models' in config to suppress",
					"provider", name, "error", err)
				return
			}

			s.mu.Lock()
			if st, ok := s.states[name]; ok {
				st.availableModels = models
				st.rawModels = rawModels
			}
			s.mu.Unlock()

			slog.Info("Models discovered from upstream",
				"provider", name, "count", len(models))
		}(name, provCfg)
	}
	wg.Wait()
}

// Models returns aggregated raw model objects from all providers in the route,
// deduplicated by model ID. Each model's owned_by field is set to the provider name.
// Model aliases are included as additional model entries.
func (s *Selector) Models(cfg *config.ConfigStruct, route *config.RouteConfig) []json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]bool)
	var result []json.RawMessage

	for _, provName := range route.Providers {
		provCfg, exists := cfg.Provider[provName]
		if !exists {
			continue
		}
		st := s.states[provName]
		if st == nil {
			continue
		}
		for _, raw := range st.rawModels {
			var entry map[string]json.RawMessage
			if err := json.Unmarshal(raw, &entry); err != nil {
				continue
			}
			var id string
			if idRaw, ok := entry["id"]; ok {
				json.Unmarshal(idRaw, &id)
			}
			if id == "" {
				continue
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			entry["owned_by"] = mustMarshal(provName)
			out, _ := json.Marshal(entry)
			result = append(result, out)
		}

		// add alias models
		for alias, real := range provCfg.ModelAliases {
			if seen[alias] {
				continue
			}
			seen[alias] = true
			result = append(result, mustMarshal(map[string]string{
				"id":       alias,
				"object":   "model",
				"owned_by": provName,
				"aliased":  real,
			}))
		}
	}

	return result
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// ProviderStatus exposes runtime health state for monitoring.
type ProviderStatus struct {
	Name                string           `json:"name"`
	ConsecutiveFailures int              `json:"consecutive_failures"`
	SuppressUntil       time.Time        `json:"suppress_until,omitzero"`
	Suppressed          bool             `json:"suppressed"`
	SuppressReasons     []SuppressReason `json:"suppress_reasons,omitempty"`
	ModelCount          int              `json:"model_count"`
	TotalRequests       int64            `json:"total_requests"`
	SuccessCount        int64            `json:"success_count"`
	FailureCount        int64            `json:"failure_count"`
	AvgLatencyMs        float64          `json:"avg_latency_ms"`
	// New metrics for stream error tracking
	PreStreamErrors int64 `json:"pre_stream_errors"` // errors before first stream packet
	InStreamErrors  int64 `json:"in_stream_errors"`  // errors after first stream packet
	FailoverCount   int64 `json:"failover_count"`    // times this provider triggered a failover
}

// recentSuppressReasons returns suppress reasons within TTL, caller must hold lock.
func (s *providerState) recentSuppressReasons() []SuppressReason {
	if len(s.suppressReasons) == 0 {
		return nil
	}
	cutoff := time.Now().Add(-suppressReasonTTL)
	var result []SuppressReason
	for _, r := range s.suppressReasons {
		if r.Time.After(cutoff) {
			result = append(result, r)
		}
	}
	return result
}

// buildStatus constructs a ProviderStatus snapshot. Caller must hold at least a read lock.
func (s *providerState) buildStatus(name string) ProviderStatus {
	now := time.Now()
	total, success, failure, avgLatency := s.windowStats()
	ps := ProviderStatus{
		Name:                name,
		ConsecutiveFailures: s.consecutiveFailures,
		SuppressUntil:       s.suppressUntil,
		Suppressed:          now.Before(s.suppressUntil),
		SuppressReasons:     s.recentSuppressReasons(),
		TotalRequests:       int64(total),
		SuccessCount:        int64(success),
		FailureCount:        int64(failure),
		AvgLatencyMs:        avgLatency,
		PreStreamErrors:     s.preStreamErrors,
		InStreamErrors:      s.inStreamErrors,
		FailoverCount:       s.failoverCount,
	}
	if s.availableModels != nil {
		ps.ModelCount = len(s.availableModels)
	}
	return ps
}

// ProviderStatuses returns a snapshot of all provider health states.
func (s *Selector) ProviderStatuses() []ProviderStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ProviderStatus, 0, len(s.states))
	for name, st := range s.states {
		result = append(result, st.buildStatus(name))
	}
	slices.SortFunc(result, func(a, b ProviderStatus) int {
		return strings.Compare(a.Name, b.Name)
	})
	return result
}

// ProviderDetail returns a single provider's status. Returns nil if not found.
func (s *Selector) ProviderDetail(name string) *ProviderStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st, exists := s.states[name]
	if !exists {
		return nil
	}
	ps := st.buildStatus(name)
	return &ps
}

// RecordFailover increments the failover counter for a provider.
func (s *Selector) RecordFailover(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if st, exists := s.states[name]; exists {
		st.failoverCount++
	}
}

// ProviderModels returns raw model objects for a single provider.
func (s *Selector) ProviderModels(name string) []json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st, exists := s.states[name]
	if !exists {
		return nil
	}
	return st.rawModels
}
