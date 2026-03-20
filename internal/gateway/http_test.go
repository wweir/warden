package gateway

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/selector"
)

func TestSendRequestForwardsSanitizedClientHeaders(t *testing.T) {
	t.Parallel()

	const (
		wantUserAgent   = "curl/8.7.1"
		wantTraceID     = "trace-123"
		wantForwardedIP = "10.10.1.23"
	)
	var gotHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	provCfg := &config.ProviderConfig{
		URL:      server.URL,
		Protocol: "openai",
		APIKey:   config.SecretString("provider-token"),
	}

	clientReq := httptest.NewRequest(http.MethodPost, "http://gateway.local/openai/chat/completions", nil)
	clientReq.Header.Set("User-Agent", wantUserAgent)
	clientReq.Header.Set("X-Trace-Id", wantTraceID)
	clientReq.Header.Set("Authorization", "Bearer client-token")
	clientReq.Header.Set("Cookie", "sid=abc")
	clientReq.Header.Set("Forwarded", "for=1.2.3.4;proto=https")
	clientReq.Header.Set("X-Forwarded-For", "1.2.3.4")
	clientReq.Header.Set("Accept-Encoding", "zstd, br, gzip")
	clientReq.RemoteAddr = wantForwardedIP + ":53001"
	clientReq.Host = "gateway.local:8080"

	ctx := withClientRequest(context.Background(), clientReq)

	_, _, err := sendRequest(ctx, provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[]}`), false)
	if err != nil {
		t.Fatalf("sendRequest() error = %v", err)
	}

	if gotHeaders.Get("User-Agent") != wantUserAgent {
		t.Fatalf("User-Agent = %q, want %q", gotHeaders.Get("User-Agent"), wantUserAgent)
	}
	if gotHeaders.Get("X-Trace-Id") != wantTraceID {
		t.Fatalf("X-Trace-Id = %q, want %q", gotHeaders.Get("X-Trace-Id"), wantTraceID)
	}
	if gotHeaders.Get("Authorization") != "Bearer provider-token" {
		t.Fatalf("Authorization = %q", gotHeaders.Get("Authorization"))
	}
	if gotHeaders.Get("X-Forwarded-For") != wantForwardedIP {
		t.Fatalf("X-Forwarded-For = %q, want %q", gotHeaders.Get("X-Forwarded-For"), wantForwardedIP)
	}
	if gotHeaders.Get("X-Forwarded-Host") != "gateway.local:8080" {
		t.Fatalf("X-Forwarded-Host = %q", gotHeaders.Get("X-Forwarded-Host"))
	}
	if gotHeaders.Get("Cookie") != "" {
		t.Fatalf("Cookie should be removed, got %q", gotHeaders.Get("Cookie"))
	}
	if gotHeaders.Get("Forwarded") != "" {
		t.Fatalf("Forwarded should be removed, got %q", gotHeaders.Get("Forwarded"))
	}
	if strings.Contains(gotHeaders.Get("Accept-Encoding"), "zstd") || strings.Contains(gotHeaders.Get("Accept-Encoding"), "br") {
		t.Fatalf("Accept-Encoding should not forward zstd/br, got %q", gotHeaders.Get("Accept-Encoding"))
	}
}

func TestSendRequestWithoutClientRequestContext(t *testing.T) {
	t.Parallel()

	var gotAuthorization string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	provCfg := &config.ProviderConfig{
		URL:      server.URL,
		Protocol: "openai",
		APIKey:   config.SecretString("provider-token"),
	}

	_, _, err := sendRequest(context.Background(), provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[]}`), false)
	if err != nil {
		t.Fatalf("sendRequest() error = %v", err)
	}

	if gotAuthorization != "Bearer provider-token" {
		t.Fatalf("Authorization = %q", gotAuthorization)
	}
}

func TestWriteUpstreamAwareError_PreservesUpstreamStatusAndJSONBody(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := &selector.UpstreamError{
		Code: http.StatusUnauthorized,
		Body: `{"error":{"type":"key_model_access_denied","message":"denied"}}`,
	}

	writeUpstreamAwareError(rec, err)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != err.Body {
		t.Fatalf("body = %q, want %q", body, err.Body)
	}
}

func TestWriteUpstreamAwareError_WrappedNonUpstreamFallsBackTo502(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := fmt.Errorf("wrapped: %w", context.DeadlineExceeded)

	writeUpstreamAwareError(rec, err)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if body := rec.Body.String(); !strings.Contains(body, "wrapped: context deadline exceeded") {
		t.Fatalf("body = %q", body)
	}
}

func TestSendStreamingRequestRejectsHTTP200JSONErrorBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":{"type":"key_model_access_denied","message":"denied"}}`))
	}))
	defer server.Close()

	provCfg := &config.ProviderConfig{
		URL:      server.URL,
		Protocol: "openai",
		APIKey:   config.SecretString("provider-token"),
	}

	reader, _, err := sendStreamingRequest(context.Background(), provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[],"stream":true}`))
	if reader != nil {
		t.Fatal("reader should be nil on HTTP 200 error body")
	}

	var upErr *selector.UpstreamError
	if !errors.As(err, &upErr) {
		t.Fatalf("error = %v, want UpstreamError", err)
	}
	if upErr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", upErr.Code, http.StatusOK)
	}
	if !strings.Contains(upErr.Body, "key_model_access_denied") {
		t.Fatalf("body = %q", upErr.Body)
	}
}

func TestSendStreamingRequestRejectsHTTP200HTMLBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body>bad gateway</body></html>`))
	}))
	defer server.Close()

	provCfg := &config.ProviderConfig{
		URL:      server.URL,
		Protocol: "openai",
		APIKey:   config.SecretString("provider-token"),
	}

	reader, _, err := sendStreamingRequest(context.Background(), provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[],"stream":true}`))
	if reader != nil {
		t.Fatal("reader should be nil on HTML error body")
	}

	var upErr *selector.UpstreamError
	if !errors.As(err, &upErr) {
		t.Fatalf("error = %v, want UpstreamError", err)
	}
	if upErr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", upErr.Code, http.StatusOK)
	}
	if !strings.Contains(upErr.Body, "<!DOCTYPE html>") {
		t.Fatalf("body = %q", upErr.Body)
	}
}

func TestSendStreamingRequestDecompressesGzipWithoutHeader(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		var body strings.Builder
		gz := gzip.NewWriter(&body)
		_, _ = gz.Write([]byte("data: hello\n\n"))
		_ = gz.Close()
		_, _ = w.Write([]byte(body.String()))
	}))
	defer server.Close()

	provCfg := &config.ProviderConfig{
		URL:      server.URL,
		Protocol: "openai",
		APIKey:   config.SecretString("provider-token"),
	}

	reader, _, err := sendStreamingRequest(context.Background(), provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[],"stream":true}`))
	if err != nil {
		t.Fatalf("sendStreamingRequest() error = %v", err)
	}
	defer reader.Close()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "data: hello\n\n" {
		t.Fatalf("body = %q, want decompressed SSE", string(body))
	}
}
