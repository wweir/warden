package reqlog

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestBroadcasterPublishReplacesRecentByRequestID(t *testing.T) {
	t.Parallel()

	b := NewBroadcaster()
	first := Record{
		Timestamp: time.Unix(100, 0),
		RequestID: "req_1",
		Model:     "gpt-4o",
		Pending:   true,
	}
	final := Record{
		Timestamp:  time.Unix(100, 0),
		RequestID:  "req_1",
		Model:      "gpt-4o",
		Provider:   "openai",
		DurationMs: 123,
	}

	b.Publish(first)
	b.Publish(final)

	recent := b.Recent()
	if len(recent) != 1 {
		t.Fatalf("recent count = %d, want 1", len(recent))
	}
	if recent[0].Pending {
		t.Fatalf("recent[0].Pending = true, want false")
	}
	if recent[0].DurationMs != 123 {
		t.Fatalf("recent[0].DurationMs = %d, want 123", recent[0].DurationMs)
	}
	if recent[0].Provider != "openai" {
		t.Fatalf("recent[0].Provider = %q, want openai", recent[0].Provider)
	}
}

func TestBroadcasterUnsubscribeIsIdempotent(t *testing.T) {
	t.Parallel()

	b := NewBroadcaster()
	ch := b.Subscribe()

	b.Unsubscribe(ch)
	b.Unsubscribe(ch)
}

func TestBroadcasterPublishConcurrentWithSubscribeAndUnsubscribe(t *testing.T) {
	t.Parallel()

	b := NewBroadcaster()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			b.Publish(Record{RequestID: fmt.Sprintf("req_%d", i)})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			ch := b.Subscribe()
			b.Unsubscribe(ch)
		}
	}()

	wg.Wait()
}
