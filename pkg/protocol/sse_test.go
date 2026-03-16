package protocol

import (
	"testing"
)

func TestParseEventsSupportsStandardFieldsAndComments(t *testing.T) {
	raw := []byte(": keepalive\r\nid: 42\r\nevent:update\r\ndata:first\r\ndata: second\r\nretry: 1500\r\n\r\n")

	events := ParseEvents(raw)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if len(evt.Comments) != 1 || evt.Comments[0] != " keepalive" {
		t.Fatalf("unexpected comments: %#v", evt.Comments)
	}
	if evt.ID != "42" || !evt.HasID {
		t.Fatalf("unexpected id: %q hasID=%v", evt.ID, evt.HasID)
	}
	if evt.EventType != "update" {
		t.Fatalf("unexpected event type: %q", evt.EventType)
	}
	if evt.Data != "first\nsecond" || !evt.HasData {
		t.Fatalf("unexpected data: %q hasData=%v", evt.Data, evt.HasData)
	}
	if evt.Retry == nil || *evt.Retry != 1500 {
		t.Fatalf("unexpected retry: %v", evt.Retry)
	}

	replayed := string(ReplayEvents(events))
	want := ": keepalive\nid: 42\nevent:update\ndata:first\ndata: second\nretry: 1500\n\n"
	if replayed != want {
		t.Fatalf("unexpected replay:\nwant:\n%q\ngot:\n%q", want, replayed)
	}
}

func TestParseEventsKeepsCommentOnlyAndHeaderOnlyFrames(t *testing.T) {
	raw := []byte(": ping\n\nid:\nretry: abc\n\n")

	events := ParseEvents(raw)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if len(events[0].Comments) != 1 || events[0].Comments[0] != " ping" {
		t.Fatalf("unexpected first event comments: %#v", events[0].Comments)
	}
	if events[1].ID != "" || !events[1].HasID {
		t.Fatalf("unexpected second event id state: id=%q hasID=%v", events[1].ID, events[1].HasID)
	}
	if events[1].Retry != nil {
		t.Fatalf("invalid retry should be ignored, got %v", *events[1].Retry)
	}

	replayed := string(ReplayEvents(events))
	if replayed != string(raw) {
		t.Fatalf("unexpected replay:\nwant:\n%q\ngot:\n%q", string(raw), replayed)
	}
}

func TestReplayEventsSerializesStructuredFields(t *testing.T) {
	retry := 0
	events := []Event{
		{
			Comments:  []string{" keepalive", ""},
			EventType: "message",
			ID:        "",
			HasID:     true,
			Retry:     &retry,
			Data:      "line1\nline2",
			HasData:   true,
		},
	}

	got := string(ReplayEvents(events))
	want := ": keepalive\n:\nevent: message\nid:\nretry: 0\ndata: line1\ndata: line2\n\n"
	if got != want {
		t.Fatalf("unexpected replay:\nwant:\n%q\ngot:\n%q", want, got)
	}
}
