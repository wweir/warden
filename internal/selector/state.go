package selector

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
)

// RecordOutcome records the result of an upstream request.
func (s *Selector) RecordOutcome(name string, err error, latency time.Duration) {
	s.RecordOutcomeWithSource(name, err, latency, "")
}

// RecordOutcomeWithSource records the result of an upstream request with error source tracking.
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
	if ue, ok := err.(*UpstreamError); ok && !ue.IsRetryable() {
		return
	}

	st.recordOutcome(false, latencyMs, errorSource)
	st.consecutiveFailures++
	if st.consecutiveFailures > maxConsecutiveFailures {
		st.consecutiveFailures = maxConsecutiveFailures
	}
	switch errorSource {
	case "pre_stream":
		st.preStreamErrors++
	case "in_stream":
		st.inStreamErrors++
	}

	duration := baseSuppressDuration << (st.consecutiveFailures - 1)
	st.suppressUntil = time.Now().Add(duration)
	st.addSuppressReason(err)

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

func (s *providerState) addSuppressReason(err error) {
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
	n := 0
	for _, r := range s.suppressReasons {
		if r.Time.After(cutoff) {
			s.suppressReasons[n] = r
			n++
		}
	}
	s.suppressReasons = s.suppressReasons[:n]
	s.suppressReasons = append(s.suppressReasons, SuppressReason{Time: now, Reason: reason})
	if len(s.suppressReasons) > maxSuppressReasons {
		s.suppressReasons = s.suppressReasons[len(s.suppressReasons)-maxSuppressReasons:]
	}
}

func (s *Selector) SetDisplayProtocols(name string, protocols []string, probe *ProtocolProbe) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, exists := s.states[name]
	if !exists {
		return false
	}
	st.displayProtocols = append([]string(nil), protocols...)
	st.lastProtocolProbe = probe
	return true
}

func (s *Selector) ModelProtocolProbes(name string) []ModelProtocolProbe {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st, exists := s.states[name]
	if !exists || len(st.modelProtocolProbes) == 0 {
		return nil
	}
	var out []ModelProtocolProbe
	for _, byProtocol := range st.modelProtocolProbes {
		for _, probe := range byProtocol {
			out = append(out, probe)
		}
	}
	slices.SortFunc(out, func(a, b ModelProtocolProbe) int {
		if cmp := strings.Compare(a.Model, b.Model); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Protocol, b.Protocol)
	})
	return out
}

func (s *Selector) UpsertModelProtocolProbe(name string, probe ModelProtocolProbe) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, exists := s.states[name]
	if !exists {
		return false
	}
	if st.modelProtocolProbes == nil {
		st.modelProtocolProbes = make(map[string]map[string]ModelProtocolProbe)
	}
	if st.modelProtocolProbes[probe.Model] == nil {
		st.modelProtocolProbes[probe.Model] = make(map[string]ModelProtocolProbe)
	}
	st.modelProtocolProbes[probe.Model][probe.Protocol] = probe
	return true
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

// SetManualSuppress sets or clears manual suppression for a provider.
// Returns true if the provider exists, false otherwise.
func (s *Selector) SetManualSuppress(name string, suppress bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, exists := s.states[name]
	if !exists {
		return false
	}
	st.manualSuppress = suppress
	if suppress {
		slog.Info("Provider manually suppressed", "name", name)
	} else {
		slog.Info("Provider manual suppression cleared", "name", name)
	}
	return true
}
