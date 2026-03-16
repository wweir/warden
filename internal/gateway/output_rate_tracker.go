package gateway

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

type outputRateEntry struct {
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
}

type outputRateTracker struct {
	mu         sync.RWMutex
	staleAfter time.Duration
	entries    map[outputRateKey]outputRateEntry
}

func newOutputRateTracker(staleAfter time.Duration) *outputRateTracker {
	return &outputRateTracker{
		staleAfter: staleAfter,
		entries:    make(map[outputRateKey]outputRateEntry),
	}
}

func (t *outputRateTracker) Record(labels requestMetricLabels, typ string, value float64, updatedAt time.Time) {
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

	t.entries[key] = outputRateEntry{
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

func (t *outputRateTracker) Snapshot(now time.Time) []outputRateEntry {
	if t == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}

	cutoff := now.Add(-t.staleAfter)

	t.mu.Lock()
	defer t.mu.Unlock()

	entries := make([]outputRateEntry, 0, len(t.entries))
	for key, entry := range t.entries {
		if entry.UpdatedAt.Before(cutoff) {
			delete(t.entries, key)
			continue
		}
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
