package upstream

import (
	"crypto/tls"
	"net/http"
	"testing"
)

func TestBuildProxyRequestHeadersSanitizesAndOverwrites(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodPost, "http://localhost:8080/openai/v1/messages", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.RemoteAddr = "10.10.1.23:34567"
	req.Host = "gateway.local:8080"

	req.Header.Set("Accept-Encoding", "gzip, br, zstd")
	req.Header.Set("Authorization", "Bearer client-token")
	req.Header.Set("Api-Key", "client-api-key")
	req.Header.Set("X-Api-Key", "client-x-api-key")
	req.Header.Set("Cookie", "sid=abc")
	req.Header.Set("Connection", "Keep-Alive, Upgrade, X-Conn-Header")
	req.Header.Set("Proxy-Connection", "X-Proxy-Conn")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Transfer-Encoding", "chunked")
	req.Header.Set("Te", "trailers")
	req.Header.Set("Trailer", "X-Trailer")
	req.Header.Set("Forwarded", "for=1.2.3.4;proto=https")
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "client.example")
	req.Header.Set("X-Real-Ip", "1.2.3.4")
	req.Header.Set("X-Conn-Header", "bad-value")
	req.Header.Set("X-Proxy-Conn", "bad-proxy-value")
	req.Header.Set("User-Agent", "unit-test-agent")

	got := BuildProxyRequestHeaders(req, true)

	if got.Get("User-Agent") != "unit-test-agent" {
		t.Fatalf("User-Agent not preserved, got %q", got.Get("User-Agent"))
	}
	if got.Get("Accept-Encoding") != "zstd, br;q=0.9, gzip;q=0.8" {
		t.Fatalf("Accept-Encoding = %q", got.Get("Accept-Encoding"))
	}
	if got.Get("X-Forwarded-For") != "10.10.1.23" {
		t.Fatalf("X-Forwarded-For = %q", got.Get("X-Forwarded-For"))
	}
	if got.Get("X-Real-Ip") != "10.10.1.23" {
		t.Fatalf("X-Real-Ip = %q", got.Get("X-Real-Ip"))
	}
	if got.Get("X-Forwarded-Proto") != "http" {
		t.Fatalf("X-Forwarded-Proto = %q", got.Get("X-Forwarded-Proto"))
	}
	if got.Get("X-Forwarded-Host") != "gateway.local:8080" {
		t.Fatalf("X-Forwarded-Host = %q", got.Get("X-Forwarded-Host"))
	}

	removedHeaders := []string{
		"Authorization",
		"Api-Key",
		"X-Api-Key",
		"Cookie",
		"Connection",
		"Proxy-Connection",
		"Keep-Alive",
		"Upgrade",
		"Transfer-Encoding",
		"Te",
		"Trailer",
		"Forwarded",
		"X-Conn-Header",
		"X-Proxy-Conn",
	}
	for _, headerName := range removedHeaders {
		if got.Get(headerName) != "" {
			t.Fatalf("%s should be removed, got %q", headerName, got.Get(headerName))
		}
	}

	if req.Header.Get("Authorization") == "" {
		t.Fatalf("source request headers should not be mutated")
	}
}

func TestBuildProxyRequestHeadersNoAcceptEncodingAndHTTPS(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodGet, "http://localhost:8080/openai/v1/models", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.RemoteAddr = "192.168.1.8"
	req.Host = "gateway.example"
	req.TLS = &tls.ConnectionState{}

	got := BuildProxyRequestHeaders(req, false)

	if got.Get("Accept-Encoding") != "" {
		t.Fatalf("Accept-Encoding should be empty, got %q", got.Get("Accept-Encoding"))
	}
	if got.Get("X-Forwarded-For") != "192.168.1.8" {
		t.Fatalf("X-Forwarded-For = %q", got.Get("X-Forwarded-For"))
	}
	if got.Get("X-Forwarded-Proto") != "https" {
		t.Fatalf("X-Forwarded-Proto = %q", got.Get("X-Forwarded-Proto"))
	}
	if got.Get("X-Forwarded-Host") != "gateway.example" {
		t.Fatalf("X-Forwarded-Host = %q", got.Get("X-Forwarded-Host"))
	}
}

func TestSanitizeCLIProxyRequestHeadersRemovesFeatureHeaders(t *testing.T) {
	t.Parallel()

	headers := http.Header{}
	headers.Set("User-Agent", "curl/8.0")
	headers.Set("X-Forwarded-For", "10.0.0.2")
	headers.Set("X-Forwarded-Port", "9832")
	headers.Set("X-Real-Ip", "10.0.0.2")
	headers.Set("Cf-Connecting-Ip", "10.0.0.3")
	headers.Set("Sec-Fetch-Site", "same-origin")
	headers.Set("X-Stainless-Os", "Linux")
	headers.Set("X-Stainless-Custom", "fingerprint")
	headers.Set("X-Codex-Beta-Features", "client-feature")
	headers.Set("X-Codex-Turn-Metadata", "metadata")
	headers.Set("X-Client-Request-Id", "client-request")
	headers.Set("Anthropic-Beta", "client-beta")
	headers.Set("Anthropic-Version", "2023-06-01")
	headers.Set("X-Anthropic-Client", "client")
	headers.Set("OpenAI-Organization", "org")
	headers.Set("X-OpenAI-Client-User-Agent", "openai-client")
	headers.Set("Originator", "third-party")
	headers.Set("X-Claude-Code-Session-Id", "session")
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept-Encoding", "zstd, br;q=0.9, gzip;q=0.8")
	headers.Set("X-Warden-Trace", "keep")

	SanitizeCLIProxyRequestHeaders(headers)

	removedHeaders := []string{
		"User-Agent",
		"X-Forwarded-For",
		"X-Forwarded-Port",
		"X-Real-Ip",
		"Cf-Connecting-Ip",
		"Sec-Fetch-Site",
		"X-Stainless-Os",
		"X-Stainless-Custom",
		"X-Codex-Beta-Features",
		"X-Codex-Turn-Metadata",
		"X-Client-Request-Id",
		"Anthropic-Beta",
		"Anthropic-Version",
		"X-Anthropic-Client",
		"OpenAI-Organization",
		"X-OpenAI-Client-User-Agent",
		"Originator",
		"X-Claude-Code-Session-Id",
	}
	for _, headerName := range removedHeaders {
		if got := headers.Get(headerName); got != "" {
			t.Fatalf("%s should be removed, got %q", headerName, got)
		}
	}
	if got := headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := headers.Get("Accept-Encoding"); got != "zstd, br;q=0.9, gzip;q=0.8" {
		t.Fatalf("Accept-Encoding = %q", got)
	}
	if got := headers.Get("X-Warden-Trace"); got != "keep" {
		t.Fatalf("X-Warden-Trace = %q", got)
	}
}
