package reqlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func TestBroadcasterPublishReplacesRecentBySessionKey(t *testing.T) {
	t.Parallel()

	b := NewBroadcaster()
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

	b.Publish(first)
	b.Publish(second)

	recent := b.Recent()
	if len(recent) != 1 {
		t.Fatalf("recent count = %d, want 1", len(recent))
	}
	if recent[0].RequestID != "req_2" {
		t.Fatalf("recent[0].RequestID = %q, want req_2", recent[0].RequestID)
	}
	if recent[0].Model != "gpt-4o-mini" {
		t.Fatalf("recent[0].Model = %q, want gpt-4o-mini", recent[0].Model)
	}
}

func TestFileLoggerOverwritesBySessionKey(t *testing.T) {
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
	if len(files) != 1 {
		t.Fatalf("file count = %d, want 1", len(files))
	}
	if got, want := files[0].Name(), "chat_completions_abcdef.json"; got != want {
		t.Fatalf("file name = %q, want %q", got, want)
	}

	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got Record
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.RequestID != "req_2" {
		t.Fatalf("RequestID = %q, want req_2", got.RequestID)
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
