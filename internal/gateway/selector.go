package gateway

import (
	"encoding/json"
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
)

// providerState tracks runtime health state for a single provider.
type providerState struct {
	consecutiveFailures int
	suppressUntil       time.Time
	availableModels     map[string]bool   // nil = unknown (fetch failed), don't filter
	rawModels           []json.RawMessage // raw model objects from GET /models

	totalRequests  int64
	successCount   int64
	failureCount   int64
	totalLatencyMs int64
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
		slog.Warn("All providers suppressed, selecting earliest expiring",
			"name", earliest.name, "suppress_until", earliest.state.suppressUntil)
		return earliest.provCfg, nil
	}

	return nil, ErrProviderNotFound
}

// RecordOutcome records the result of an upstream request.
// Only retryable errors (5xx, 429, connection failures) trigger suppression.
// Client errors (4xx except 429) and successes reset the failure counter.
func (s *Selector) RecordOutcome(name string, err error, latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, exists := s.states[name]
	if !exists {
		return
	}

	st.totalRequests++
	st.totalLatencyMs += latency.Milliseconds()

	if err == nil {
		st.successCount++
		st.consecutiveFailures = 0
		st.suppressUntil = time.Time{}
		return
	}

	// only suppress on retryable errors
	if ue, ok := err.(*UpstreamError); ok && !ue.IsRetryable() {
		st.successCount++ // 4xx (except 429) counts as success
		return
	}

	st.failureCount++
	st.consecutiveFailures++
	if st.consecutiveFailures > maxConsecutiveFailures {
		st.consecutiveFailures = maxConsecutiveFailures
	}

	// exponential backoff: 30s, 60s, 120s, 240s, 480s
	duration := baseSuppressDuration << (st.consecutiveFailures - 1)
	st.suppressUntil = time.Now().Add(duration)

	slog.Warn("Provider suppressed",
		"name", name,
		"consecutive_failures", st.consecutiveFailures,
		"suppress_duration", duration,
	)
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
	Name                string    `json:"name"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	SuppressUntil       time.Time `json:"suppress_until,omitzero"`
	Suppressed          bool      `json:"suppressed"`
	ModelCount          int       `json:"model_count"`
	TotalRequests       int64     `json:"total_requests"`
	SuccessCount        int64     `json:"success_count"`
	FailureCount        int64     `json:"failure_count"`
	AvgLatencyMs        float64   `json:"avg_latency_ms"`
}

// ProviderStatuses returns a snapshot of all provider health states.
func (s *Selector) ProviderStatuses() []ProviderStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	result := make([]ProviderStatus, 0, len(s.states))
	for name, st := range s.states {
		ps := ProviderStatus{
			Name:                name,
			ConsecutiveFailures: st.consecutiveFailures,
			SuppressUntil:       st.suppressUntil,
			Suppressed:          now.Before(st.suppressUntil),
			TotalRequests:       st.totalRequests,
			SuccessCount:        st.successCount,
			FailureCount:        st.failureCount,
		}
		if st.availableModels != nil {
			ps.ModelCount = len(st.availableModels)
		}
		if st.totalRequests > 0 {
			ps.AvgLatencyMs = float64(st.totalLatencyMs) / float64(st.totalRequests)
		}
		result = append(result, ps)
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
	now := time.Now()
	ps := &ProviderStatus{
		Name:                name,
		ConsecutiveFailures: st.consecutiveFailures,
		SuppressUntil:       st.suppressUntil,
		Suppressed:          now.Before(st.suppressUntil),
		TotalRequests:       st.totalRequests,
		SuccessCount:        st.successCount,
		FailureCount:        st.failureCount,
	}
	if st.availableModels != nil {
		ps.ModelCount = len(st.availableModels)
	}
	if st.totalRequests > 0 {
		ps.AvgLatencyMs = float64(st.totalLatencyMs) / float64(st.totalRequests)
	}
	return ps
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
