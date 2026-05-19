package upstream

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
	"time"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/providerauth"
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
		Format: "openai",
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

	_, _, err := SendRequest(context.Background(), clientReq, provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[]}`), false)
	if err != nil {
		t.Fatalf("SendRequest() error = %v", err)
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

func TestSendRequestSanitizesCLIProxyFeatureHeaders(t *testing.T) {
	t.Parallel()

	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	provCfg := &config.ProviderConfig{
		URL:      server.URL,
		Format: "openai",
		Backend:  config.ProviderBackendCLIProxy,
		APIKey:   config.SecretString("provider-token"),
	}

	clientReq := httptest.NewRequest(http.MethodPost, "http://gateway.local/openai/chat/completions", nil)
	clientReq.Header.Set("User-Agent", "codex-cli")
	clientReq.Header.Set("X-Codex-Beta-Features", "client-feature")
	clientReq.Header.Set("X-OpenAI-Client-User-Agent", "openai-client")
	clientReq.Header.Set("X-Forwarded-For", "1.2.3.4")
	clientReq.Header.Set("X-Warden-Trace", "trace-123")
	clientReq.RemoteAddr = "10.10.1.23:53001"
	clientReq.Host = "gateway.local:8080"

	_, _, err := SendRequest(context.Background(), clientReq, provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[]}`), false)
	if err != nil {
		t.Fatalf("SendRequest() error = %v", err)
	}

	for _, headerName := range []string{
		"X-Codex-Beta-Features",
		"X-OpenAI-Client-User-Agent",
		"X-Forwarded-For",
		"X-Forwarded-Host",
		"X-Forwarded-Proto",
		"X-Real-Ip",
	} {
		if got := gotHeaders.Get(headerName); got != "" {
			t.Fatalf("%s should be removed for cliproxy provider, got %q", headerName, got)
		}
	}
	if got := gotHeaders.Get("User-Agent"); got == "codex-cli" {
		t.Fatalf("User-Agent should not forward client fingerprint, got %q", got)
	}
	if gotHeaders.Get("Authorization") != "Bearer provider-token" {
		t.Fatalf("Authorization = %q", gotHeaders.Get("Authorization"))
	}
	if gotHeaders.Get("X-Warden-Trace") != "trace-123" {
		t.Fatalf("X-Warden-Trace = %q", gotHeaders.Get("X-Warden-Trace"))
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
		Format: "openai",
		APIKey:   config.SecretString("provider-token"),
	}

	_, _, err := SendRequest(context.Background(), nil, provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[]}`), false)
	if err != nil {
		t.Fatalf("SendRequest() error = %v", err)
	}

	if gotAuthorization != "Bearer provider-token" {
		t.Fatalf("Authorization = %q", gotAuthorization)
	}
}

func TestSendRequestSanitizesProviderAuthErrors(t *testing.T) {
	t.Parallel()

	provCfg := &config.ProviderConfig{
		Name:          "secret-provider",
		URL:           "https://upstream.example.test",
		Format:      "openai",
		APIKeyCommand: "exit 7",
	}

	_, _, err := SendRequest(context.Background(), nil, provCfg, "/v1/chat/completions", []byte(`{}`), false)
	var upErr *selector.UpstreamError
	if !errors.As(err, &upErr) {
		t.Fatalf("error = %v, want UpstreamError", err)
	}
	if upErr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", upErr.Code, http.StatusUnauthorized)
	}
	if upErr.Body != providerauth.ClientAuthFailureBody {
		t.Fatalf("body = %q, want sanitized auth failure", upErr.Body)
	}
	if strings.Contains(upErr.Body, provCfg.Name) {
		t.Fatalf("body leaked provider name: %q", upErr.Body)
	}
}

func TestSendRequestDoesNotReplayPostOnTransportError(t *testing.T) {
	t.Parallel()

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("response writer does not support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack connection: %v", err)
		}
		_ = conn.Close()
	}))
	defer server.Close()

	provCfg := &config.ProviderConfig{
		URL:      server.URL,
		Format: "openai",
		APIKey:   config.SecretString("provider-token"),
	}

	_, _, err := SendRequest(context.Background(), nil, provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[]}`), false)
	if err == nil {
		t.Fatal("SendRequest() error = nil, want transport error")
	}
	if hits != 1 {
		t.Fatalf("upstream hits = %d, want 1", hits)
	}
}

func TestWriteUpstreamAwareError_PreservesUpstreamStatusAndJSONBody(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := &selector.UpstreamError{
		Code: http.StatusUnauthorized,
		Body: `{"error":{"type":"key_model_access_denied","message":"denied"}}`,
	}

	WriteUpstreamAwareError(rec, err)

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

	WriteUpstreamAwareError(rec, err)

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
		Format: "openai",
		APIKey:   config.SecretString("provider-token"),
	}

	reader, _, err := SendStreamingRequest(context.Background(), nil, provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[],"stream":true}`))
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
		Format: "openai",
		APIKey:   config.SecretString("provider-token"),
	}

	reader, _, err := SendStreamingRequest(context.Background(), nil, provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[],"stream":true}`))
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

func TestSendStreamingRequestAcceptsSSEBodyWithoutContentType(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("data: {\"id\":\"chunk\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provCfg := &config.ProviderConfig{
		URL:      server.URL,
		Format: "openai",
		APIKey:   config.SecretString("provider-token"),
	}

	reader, _, err := SendStreamingRequest(context.Background(), nil, provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[],"stream":true}`))
	if err != nil {
		t.Fatalf("SendStreamingRequest() error = %v", err)
	}
	defer reader.Close()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got := string(body); got != "data: {\"id\":\"chunk\"}\n\ndata: [DONE]\n\n" {
		t.Fatalf("body = %q", got)
	}
}

func TestSendStreamingRequestLatencyWaitsForFirstToken(t *testing.T) {
	t.Parallel()

	const firstTokenDelay = 80 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		time.Sleep(firstTokenDelay)
		_, _ = w.Write([]byte("data: {\"id\":\"chunk\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	provCfg := &config.ProviderConfig{
		URL:      server.URL,
		Format: "openai",
		APIKey:   config.SecretString("provider-token"),
		Timeout:  "2s",
	}

	reader, latency, err := SendStreamingRequest(context.Background(), nil, provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[],"stream":true}`))
	if err != nil {
		t.Fatalf("SendStreamingRequest() error = %v", err)
	}
	defer reader.Close()

	if latency < firstTokenDelay/2 {
		t.Fatalf("latency = %s, want it to include first-token wait around %s", latency, firstTokenDelay)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got := string(body); !strings.Contains(got, `"chunk"`) || !strings.Contains(got, "[DONE]") {
		t.Fatalf("body = %q", got)
	}
}

func TestSendStreamingRequestTimesOutWaitingForFirstTokenAfterHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("data: late\n\n"))
	}))
	defer server.Close()

	provCfg := &config.ProviderConfig{
		URL:      server.URL,
		Format: "openai",
		APIKey:   config.SecretString("provider-token"),
		Timeout:  "30ms",
	}

	reader, _, err := SendStreamingRequest(context.Background(), nil, provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[],"stream":true}`))
	if reader != nil {
		t.Fatal("reader should be nil on first-token timeout")
	}
	var timeoutErr *FirstTokenTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("error = %v, want FirstTokenTimeoutError", err)
	}
	if !selector.IsRetryableError(err) {
		t.Fatalf("first-token timeout should be retryable, got %v", err)
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
		Format: "openai",
		APIKey:   config.SecretString("provider-token"),
	}

	reader, _, err := SendStreamingRequest(context.Background(), nil, provCfg, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[],"stream":true}`))
	if err != nil {
		t.Fatalf("SendStreamingRequest() error = %v", err)
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

func TestJoinBaseURLPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		base string
		path string
		want string
	}{
		{name: "root path", base: "https://api.example.com", path: "/v1/chat/completions", want: "https://api.example.com/v1/chat/completions"},
		{name: "base path", base: "https://api.example.com/openai", path: "/v1/chat/completions", want: "https://api.example.com/openai/v1/chat/completions"},
		{name: "absolute path", base: "https://api.example.com", path: "https://alt.example.com/v1/chat/completions", want: "https://alt.example.com/v1/chat/completions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JoinBaseURLPath(tt.base, tt.path); got != tt.want {
				t.Fatalf("JoinBaseURLPath(%q, %q) = %q, want %q", tt.base, tt.path, got, tt.want)
			}
		})
	}
}

func TestBaseURLHasPathSuffix(t *testing.T) {
	t.Parallel()

	if !baseURLHasPathSuffix("https://api.example.com/v1", "/v1") {
		t.Fatal("expected /v1 suffix to be detected")
	}
	if baseURLHasPathSuffix("https://api.example.com", "/v1") {
		t.Fatal("did not expect /v1 suffix on root URL")
	}
}
