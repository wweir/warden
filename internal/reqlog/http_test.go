package reqlog

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPLoggerCloseCancelsInflightSend(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		close(finished)
	}))
	defer srv.Close()

	logger, err := NewHTTPLogger(HTTPLoggerConfig{
		URL:     srv.URL,
		Method:  http.MethodPost,
		Timeout: "5s",
	})
	if err != nil {
		t.Fatalf("NewHTTPLogger() error = %v", err)
	}

	logger.Log(Record{RequestID: "req-1"})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP logger did not start request")
	}

	closed := make(chan struct{})
	go func() {
		_ = logger.Close()
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Close() did not return promptly after cancellation")
	}

	close(release)
	select {
	case <-finished:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("test server handler did not finish")
	}
}
