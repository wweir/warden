package gateway

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminpkg "github.com/wweir/warden/internal/gateway/admin"
	"github.com/wweir/warden/internal/reqlog"
)

func TestHandleLogStreamDisablesProxyBufferingAndFlushesPrelude(t *testing.T) {
	t.Parallel()

	gw := &Gateway{broadcaster: reqlog.NewBroadcaster()}
	handler := adminpkg.NewHandler(adminpkg.Deps{Broadcaster: gw.broadcaster})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.HandleLogStream(w, r, nil)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("get log stream: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream; charset=utf-8" {
		t.Fatalf("content-type = %q, want text/event-stream; charset=utf-8", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache, no-transform" {
		t.Fatalf("cache-control = %q, want no-cache, no-transform", got)
	}
	if got := resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("x-accel-buffering = %q, want no", got)
	}

	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read prelude: %v", err)
	}
	if got := strings.TrimSpace(line); got != ": stream-open" {
		t.Fatalf("first stream line = %q, want : stream-open", got)
	}

	blank, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read prelude separator: %v", err)
	}
	if blank != "\n" {
		t.Fatalf("prelude separator = %q, want blank line", blank)
	}

	gw.broadcaster.Publish(reqlog.Record{
		RequestID: "req_1",
		Route:     "/chat",
		Request:   json.RawMessage(`{"input":"hello"}`),
	})

	dataLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	if !strings.HasPrefix(dataLine, "data: ") {
		t.Fatalf("event line = %q, want data frame", dataLine)
	}
	if !strings.Contains(dataLine, `"request_id":"req_1"`) {
		t.Fatalf("event line = %q, missing request id", dataLine)
	}
}
