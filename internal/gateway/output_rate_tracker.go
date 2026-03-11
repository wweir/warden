package gateway

import (
	"sort"
	"sync"
	"time"
)

type outputRateKey struct {
	route    string
	provider string
	model    string
	endpoint string
	typ      string
}

type outputRateEntry struct {
	Route     string
	Provider  string
	Model     string
	Endpoint  string
	Type      string
	Value     float64
	UpdatedAt time.Time
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

func (t *outputRateTracker) Record(route, provider, model, endpoint, typ string, value float64, updatedAt time.Time) {
	if t == nil {
		return
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}

	key := outputRateKey{
		route:    route,
		provider: provider,
		model:    model,
		endpoint: endpoint,
		typ:      typ,
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.entries[key] = outputRateEntry{
		Route:     route,
		Provider:  provider,
		Model:     model,
		Endpoint:  endpoint,
		Type:      typ,
		Value:     value,
		UpdatedAt: updatedAt,
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
		if entries[i].Model != entries[j].Model {
			return entries[i].Model < entries[j].Model
		}
		return entries[i].Endpoint < entries[j].Endpoint
	})

	return entries
}
