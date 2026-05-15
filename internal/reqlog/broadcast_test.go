package reqlog

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
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

func TestBroadcasterPublishKeepsSameAgentPrefixSessions(t *testing.T) {
	t.Parallel()

	b := NewBroadcaster()
	first := Record{
		Timestamp:   time.Unix(100, 0),
		RequestID:   "req_1",
		Route:       "chat/completions",
		Fingerprint: "abcdef1111",
		Request:     json.RawMessage(`{"messages":[{"role":"system","content":"same agent prefix"},{"role":"user","content":"first task"}]}`),
		Model:       "gpt-4o",
	}
	second := Record{
		Timestamp:   time.Unix(101, 0),
		RequestID:   "req_2",
		Route:       "chat/completions",
		Fingerprint: "abcdef2222",
		Request:     json.RawMessage(`{"messages":[{"role":"system","content":"same agent prefix"},{"role":"user","content":"second task"}]}`),
		Model:       "gpt-4o-mini",
	}

	b.Publish(first)
	b.Publish(second)

	recent := b.Recent()
	if len(recent) != 2 {
		t.Fatalf("recent count = %d, want 2", len(recent))
	}
}

func TestBroadcasterPublishCapsSessionsPerRoute(t *testing.T) {
	t.Parallel()

	b := NewBroadcaster()
	for i := 0; i < maxSessionsPerRoute+5; i++ {
		b.Publish(Record{
			Timestamp: time.Unix(int64(100+i), 0),
			RequestID: fmt.Sprintf("req_%d", i),
			Route:     "chat/completions",
			Request:   json.RawMessage(fmt.Sprintf(`{"messages":[{"role":"user","content":"task-%02d"}]}`, i)),
		})
	}

	recent := b.Recent()
	if len(recent) != maxSessionsPerRoute {
		t.Fatalf("recent count = %d, want %d", len(recent), maxSessionsPerRoute)
	}
	if got := recent[0].RequestID; got != "req_5" {
		t.Fatalf("recent[0].RequestID = %q, want req_5", got)
	}
	if got := recent[len(recent)-1].RequestID; got != fmt.Sprintf("req_%d", maxSessionsPerRoute+4) {
		t.Fatalf("recent[last].RequestID = %q, want req_%d", got, maxSessionsPerRoute+4)
	}
}

func TestBroadcasterPublishCapsSessionsPerRouteIndependently(t *testing.T) {
	t.Parallel()

	b := NewBroadcaster()
	for i := 0; i < maxSessionsPerRoute+3; i++ {
		b.Publish(Record{
			Timestamp: time.Unix(int64(100+i), 0),
			RequestID: fmt.Sprintf("chat_%d", i),
			Route:     "chat/completions",
			Request:   json.RawMessage(fmt.Sprintf(`{"messages":[{"role":"user","content":"chat-%02d"}]}`, i)),
		})
		b.Publish(Record{
			Timestamp: time.Unix(int64(200+i), 0),
			RequestID: fmt.Sprintf("resp_%d", i),
			Route:     "responses",
			Request:   json.RawMessage(fmt.Sprintf(`{"input":"resp-%02d"}`, i)),
		})
	}

	recent := b.Recent()
	if len(recent) != maxSessionsPerRoute*2 {
		t.Fatalf("recent count = %d, want %d", len(recent), maxSessionsPerRoute*2)
	}
	for _, rec := range recent {
		if rec.Route == "chat/completions" && strings.HasPrefix(rec.RequestID, "chat_") {
			continue
		}
		if rec.Route == "responses" && strings.HasPrefix(rec.RequestID, "resp_") {
			continue
		}
		t.Fatalf("unexpected record in recent set: %#v", rec)
	}
}

func TestBroadcasterPublishKeepsPendingRecordsWhenCappingRouteSessions(t *testing.T) {
	t.Parallel()

	b := NewBroadcaster()
	pending := Record{
		Timestamp: time.Unix(50, 0),
		RequestID: "req_pending",
		Route:     "chat/completions",
		Pending:   true,
		Request:   json.RawMessage(`{"messages":[{"role":"user","content":"live"}]}`),
	}
	b.Publish(pending)
	for i := 0; i < maxSessionsPerRoute+5; i++ {
		b.Publish(Record{
			Timestamp: time.Unix(int64(100+i), 0),
			RequestID: fmt.Sprintf("req_%d", i),
			Route:     "chat/completions",
			Request:   json.RawMessage(fmt.Sprintf(`{"messages":[{"role":"user","content":"task-%02d"}]}`, i)),
		})
	}

	recent := b.Recent()
	foundPending := false
	for _, rec := range recent {
		if rec.RequestID == pending.RequestID {
			foundPending = true
			if !rec.Pending {
				t.Fatalf("pending record was replaced: %#v", rec)
			}
		}
	}
	if !foundPending {
		t.Fatalf("pending record was dropped from recent set")
	}
}

func TestBroadcasterPublishReplacesContinuedConversation(t *testing.T) {
	t.Parallel()

	b := NewBroadcaster()
	first := Record{
		Timestamp: time.Unix(100, 0),
		RequestID: "req_1",
		Route:     "chat/completions",
		Request:   json.RawMessage(`{"messages":[{"role":"system","content":"agent"},{"role":"user","content":"first task"}]}`),
		Model:     "gpt-4o",
	}
	second := Record{
		Timestamp: time.Unix(101, 0),
		RequestID: "req_2",
		Route:     "chat/completions",
		Request:   json.RawMessage(`{"messages":[{"role":"system","content":"agent"},{"role":"user","content":"first task"},{"role":"assistant","content":"answer"},{"role":"user","content":"follow up"}]}`),
		Model:     "gpt-4o-mini",
	}

	b.Publish(first)
	b.Publish(second)

	recent := b.Recent()
	if len(recent) != 1 {
		t.Fatalf("recent count = %d, want 1", len(recent))
	}
	if recent[0].RequestID != "req_2" {
		t.Fatalf("recent[0].RequestID = %q, want req_2", recent[0].RequestID)
	}
}

func TestFileLoggerKeepsDistinctRequests(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger, err := NewFileLogger(dir)
	if err != nil {
		t.Fatalf("NewFileLogger: %v", err)
	}

	first := Record{
		Timestamp:   time.Unix(100, 0),
		RequestID:   "req_1",
		Route:       "chat/completions",
		Fingerprint: "abcdef1111",
		Model:       "gpt-4o",
	}
	second := Record{
		Timestamp:   time.Unix(101, 0),
		RequestID:   "req_2",
		Route:       "chat/completions",
		Fingerprint: "abcdef2222",
		Model:       "gpt-4o-mini",
	}

	logger.Log(first)
	logger.Log(second)

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("file count = %d, want 2", len(files))
	}
	for _, file := range files {
		if !strings.Contains(file.Name(), "req_") {
			t.Fatalf("file name = %q, want request id in name", file.Name())
		}
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
