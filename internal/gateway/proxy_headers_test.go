package gateway

import (
	"crypto/tls"
	"net/http"
	"testing"

	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
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

	got := upstreampkg.BuildProxyRequestHeaders(req, true)

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

	got := upstreampkg.BuildProxyRequestHeaders(req, false)

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
