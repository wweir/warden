package telemetry

import (
	"sort"
	"sync"
	"time"
)

type outputRateKey struct {
	route          string
	protocol       string
	provider       string
	routeModel     string
	providerModel  string
	matchedPattern string
	endpoint       string
	typ            string
}

type OutputRateEntry struct {
	Route          string
	Protocol       string
	Provider       string
	RouteModel     string
	ProviderModel  string
	MatchedPattern string
	Endpoint       string
	Type           string
	Value          float64
	UpdatedAt      time.Time
	ExpiresAt      time.Time
}

type OutputRateTracker struct {
	mu         sync.RWMutex
	staleAfter time.Duration
	entries    map[outputRateKey]OutputRateEntry
}

func NewOutputRateTracker(staleAfter time.Duration) *OutputRateTracker {
	return &OutputRateTracker{
		staleAfter: staleAfter,
		entries:    make(map[outputRateKey]OutputRateEntry),
	}
}

func (t *OutputRateTracker) Record(labels Labels, typ string, value float64, updatedAt time.Time) {
	if t == nil {
		return
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}

	key := outputRateKey{
		route:          labels.Route,
		protocol:       labels.Protocol,
		provider:       labels.Provider,
		routeModel:     labels.RouteModel,
		providerModel:  labels.ProviderModel,
		matchedPattern: labels.MatchedPattern,
		endpoint:       labels.Endpoint,
		typ:            typ,
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.entries[key] = OutputRateEntry{
		Route:          labels.Route,
		Protocol:       labels.Protocol,
		Provider:       labels.Provider,
		RouteModel:     labels.RouteModel,
		ProviderModel:  labels.ProviderModel,
		MatchedPattern: labels.MatchedPattern,
		Endpoint:       labels.Endpoint,
		Type:           typ,
		Value:          value,
		UpdatedAt:      updatedAt,
	}
}

func (t *OutputRateTracker) Snapshot(now time.Time) []OutputRateEntry {
	if t == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}

	cutoff := now.Add(-t.staleAfter)

	t.mu.Lock()
	defer t.mu.Unlock()

	entries := make([]OutputRateEntry, 0, len(t.entries))
	for key, entry := range t.entries {
		if entry.UpdatedAt.Before(cutoff) {
			delete(t.entries, key)
			continue
		}
		entry.ExpiresAt = entry.UpdatedAt.Add(t.staleAfter)
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type < entries[j].Type
		}
		if entries[i].Route != entries[j].Route {
			return entries[i].Route < entries[j].Route
		}
		if entries[i].Provider != entries[j].Provider {
			return entries[i].Provider < entries[j].Provider
		}
		if entries[i].RouteModel != entries[j].RouteModel {
			return entries[i].RouteModel < entries[j].RouteModel
		}
		if entries[i].ProviderModel != entries[j].ProviderModel {
			return entries[i].ProviderModel < entries[j].ProviderModel
		}
		return entries[i].Endpoint < entries[j].Endpoint
	})

	return entries
}
