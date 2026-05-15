package reqlog

import (
	"sort"
	"sync"
)

const (
	maxSessionsPerRoute = 20
	subscriberBuf       = 64
)

// Broadcaster fans out Record to SSE subscribers and keeps a per-route session window.
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan Record]struct{}
	routeRecent map[string][]Record
}

// NewBroadcaster creates a new Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[chan Record]struct{}),
		routeRecent: make(map[string][]Record),
	}
}

// Publish stores a Record in the per-route session window and sends it to all subscribers (non-blocking).
func (b *Broadcaster) Publish(r Record) {
	b.mu.Lock()
	defer b.mu.Unlock()

	routeKey := normalizeRouteKey(r.Route)
	bucket := b.routeRecent[routeKey]

	if idx, ok := findRequestIndex(bucket, r.RequestID); ok {
		bucket[idx] = r
		b.routeRecent[routeKey] = bucket
		b.fanOutLocked(r)
		return
	}

	if idx, ok := findContinuationIndex(bucket, r); ok {
		bucket[idx] = r
		b.routeRecent[routeKey] = bucket
		b.fanOutLocked(r)
		return
	}

	bucket = append(bucket, r)
	if len(bucket) > maxSessionsPerRoute {
		pending := make([]Record, 0, len(bucket))
		completed := make([]Record, 0, len(bucket))
		for _, rec := range bucket {
			if rec.Pending {
				pending = append(pending, rec)
				continue
			}
			completed = append(completed, rec)
		}
		keepCompleted := maxSessionsPerRoute - len(pending)
		if keepCompleted < 0 {
			keepCompleted = 0
		}
		if len(completed) > keepCompleted {
			completed = completed[len(completed)-keepCompleted:]
		}
		bucket = append(pending, completed...)
	}
	b.routeRecent[routeKey] = bucket

	b.fanOutLocked(r)
}

func (b *Broadcaster) fanOutLocked(r Record) {
	for ch := range b.subscribers {
		select {
		case ch <- r:
		default:
			// subscriber too slow, drop this event
		}
	}
}

func findRequestIndex(bucket []Record, requestID string) (int, bool) {
	if requestID == "" {
		return 0, false
	}
	for i := range bucket {
		if bucket[i].RequestID == requestID {
			return i, true
		}
	}
	return 0, false
}

func findContinuationIndex(bucket []Record, r Record) (int, bool) {
	for i := range bucket {
		if r.Continues(bucket[i]) {
			return i, true
		}
	}
	return 0, false
}

func normalizeRouteKey(route string) string {
	if route == "" {
		return "(unknown)"
	}
	return route
}

// Subscribe returns a channel that receives Record events.
func (b *Broadcaster) Subscribe() chan Record {
	ch := make(chan Record, subscriberBuf)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
func (b *Broadcaster) Unsubscribe(ch chan Record) {
	b.mu.Lock()
	if _, ok := b.subscribers[ch]; ok {
		delete(b.subscribers, ch)
		close(ch)
	}
	b.mu.Unlock()
}

// Recent returns the most recent entries in chronological order.
func (b *Broadcaster) Recent() []Record {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]Record, 0)
	for _, bucket := range b.routeRecent {
		result = append(result, bucket...)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Timestamp.Equal(result[j].Timestamp) {
			return result[i].RequestID < result[j].RequestID
		}
		return result[i].Timestamp.Before(result[j].Timestamp)
	})
	return result
}
