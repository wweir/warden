package gateway

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wweir/warden/config"
)

func TestGatewayProxyLogsDecompressedResponsesBody(t *testing.T) {
	t.Parallel()

	const responseBody = `{"id":"resp_123","object":"response","status":"completed","model":"gpt-4o","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":3,"output_tokens":5,"total_tokens":8}}`

	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write([]byte(responseBody)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
			return
		}
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("upstream path = %q, want /v1/responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(compressed.Bytes())
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("provider-token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolResponses,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": {
						Upstreams: []*config.RouteUpstreamConfig{
							{Provider: "openai", Model: "gpt-4o"},
						},
					},
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"hello"}`))
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}

	records := gw.Broadcaster().Recent()
	if len(records) != 1 {
		t.Fatalf("recent log count = %d, want 1", len(records))
	}

	got := string(records[0].Response)
	if got != responseBody {
		t.Fatalf("logged response = %q, want %q", got, responseBody)
	}
	if strings.Contains(got, "<compressed response encoding=") {
		t.Fatalf("logged response should be decompressed, got %q", got)
	}
}
