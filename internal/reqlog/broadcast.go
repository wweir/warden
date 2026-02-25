package reqlog

import (
	"sync"
)

const (
	recentSize    = 50
	subscriberBuf = 64
)

// Broadcaster fans out Record to SSE subscribers and keeps a ring buffer of recent entries.
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan Record]struct{}
	recent      []Record
	pos         int  // ring buffer write position
	full        bool // ring buffer has wrapped
}

// NewBroadcaster creates a new Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[chan Record]struct{}),
		recent:      make([]Record, recentSize),
	}
}

// Publish stores a Record in the ring buffer and sends it to all subscribers (non-blocking).
func (b *Broadcaster) Publish(r Record) {
	b.mu.Lock()
	b.recent[b.pos] = r
	b.pos++
	if b.pos >= recentSize {
		b.pos = 0
		b.full = true
	}

	// fan-out: non-blocking send to all subscribers
	for ch := range b.subscribers {
		select {
		case ch <- r:
		default:
			// subscriber too slow, drop this event
		}
	}
	b.mu.Unlock()
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
	delete(b.subscribers, ch)
	b.mu.Unlock()
	close(ch)
}

// Recent returns the most recent entries in chronological order.
func (b *Broadcaster) Recent() []Record {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.full {
		result := make([]Record, b.pos)
		copy(result, b.recent[:b.pos])
		return result
	}

	result := make([]Record, recentSize)
	copy(result, b.recent[b.pos:])
	copy(result[recentSize-b.pos:], b.recent[:b.pos])
	return result
}
