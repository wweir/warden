package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
)

func TestGatewayFailoverLogsTrailAcrossProtocols(t *testing.T) {
	tests := []struct {
		name           string
		routePrefix    string
		routeProtocol  string
		requestPath    string
		requestBody    string
		upstreamPath   string
		primaryURL     func(string) string
		fallbackURL    func(string) string
		primaryProto   string
		fallbackProto  string
		mutatePrimary  func(*config.ProviderConfig)
		mutateFallback func(*config.ProviderConfig)
		exactModel     string
		primaryModel   string
		fallbackModel  string
		successBody    string
		failBody       string
	}{
		{
			name:          "chat direct",
			routePrefix:   "/openai-chat",
			routeProtocol: config.RouteProtocolChat,
			requestPath:   "/openai-chat/chat/completions",
			requestBody:   `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`,
			upstreamPath:  "/chat/completions",
			primaryURL:    func(url string) string { return url },
			fallbackURL:   func(url string) string { return url },
			primaryProto:  "openai",
			fallbackProto: "openai",
			exactModel:    "gpt-4o",
			primaryModel:  "gpt-4o-primary",
			fallbackModel: "gpt-4o-fallback",
			successBody:   `{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4o-fallback","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			failBody:      `{"error":{"type":"server_error","message":"primary failed"}}`,
		},
		{
			name:          "responses direct",
			routePrefix:   "/openai-responses",
			routeProtocol: config.RouteProtocolResponsesStateless,
			requestPath:   "/openai-responses/responses",
			requestBody:   `{"model":"gpt-4o","input":"hello"}`,
			upstreamPath:  "/responses",
			primaryURL:    func(url string) string { return url },
			fallbackURL:   func(url string) string { return url },
			primaryProto:  "openai",
			fallbackProto: "openai",
			exactModel:    "gpt-4o",
			primaryModel:  "gpt-4o-primary",
			fallbackModel: "gpt-4o-fallback",
			successBody:   `{"id":"resp_456","status":"completed","output":[{"type":"message","content":"ok"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			failBody:      `{"error":{"type":"server_error","message":"primary failed"}}`,
		},
		{
			name:          "responses via chat",
			routePrefix:   "/openai-resp-chat",
			routeProtocol: config.RouteProtocolResponsesStateless,
			requestPath:   "/openai-resp-chat/responses",
			requestBody:   `{"model":"gpt-4o","input":"hello"}`,
			upstreamPath:  "/chat/completions",
			primaryURL:    func(url string) string { return url },
			fallbackURL:   func(url string) string { return url },
			primaryProto:  "openai",
			fallbackProto: "openai",
			mutatePrimary: func(prov *config.ProviderConfig) { prov.ResponsesToChat = true },
			mutateFallback: func(prov *config.ProviderConfig) {
				prov.ResponsesToChat = true
			},
			exactModel:    "gpt-4o",
			primaryModel:  "gpt-4o-primary",
			fallbackModel: "gpt-4o-fallback",
			successBody:   `{"id":"chatcmpl_456","object":"chat.completion","created":1,"model":"gpt-4o-fallback","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			failBody:      `{"error":{"type":"server_error","message":"primary failed"}}`,
		},
		{
			name:          "anthropic proxy messages",
			routePrefix:   "/anthropic",
			routeProtocol: config.RouteProtocolAnthropic,
			requestPath:   "/anthropic/messages",
			requestBody:   `{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`,
			upstreamPath:  "/v1/messages",
			primaryURL:    func(url string) string { return url + "/v1" },
			fallbackURL:   func(url string) string { return url + "/v1" },
			primaryProto:  "anthropic",
			fallbackProto: "anthropic",
			exactModel:    "claude-3-5-sonnet",
			primaryModel:  "claude-3-5-sonnet-primary",
			fallbackModel: "claude-3-5-sonnet-fallback",
			successBody:   `{"id":"msg_123","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude-3-5-sonnet-fallback","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
			failBody:      `{"type":"error","error":{"type":"overloaded_error","message":"primary failed"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primary := newFailoverUpstream(t, tt.upstreamPath, http.StatusInternalServerError, tt.failBody)
			defer primary.Close()
			fallback := newFailoverUpstream(t, tt.upstreamPath, http.StatusOK, tt.successBody)
			defer fallback.Close()

			primaryProv := &config.ProviderConfig{
				URL:      tt.primaryURL(primary.URL),
				Protocol: tt.primaryProto,
				APIKey:   config.SecretString("primary-token"),
			}
			if tt.mutatePrimary != nil {
				tt.mutatePrimary(primaryProv)
			}

			fallbackProv := &config.ProviderConfig{
				URL:      tt.fallbackURL(fallback.URL),
				Protocol: tt.fallbackProto,
				APIKey:   config.SecretString("fallback-token"),
			}
			if tt.mutateFallback != nil {
				tt.mutateFallback(fallbackProv)
			}

			cfg := &config.ConfigStruct{
				Provider: map[string]*config.ProviderConfig{
					"primary":  primaryProv,
					"fallback": fallbackProv,
				},
				Route: map[string]*config.RouteConfig{
					tt.routePrefix: {
						Protocol: tt.routeProtocol,
						ExactModels: map[string]*config.ExactRouteModelConfig{
							tt.exactModel: exactModel(tt.routeProtocol,
								&config.RouteUpstreamConfig{Provider: "primary", Model: tt.primaryModel},
								&config.RouteUpstreamConfig{Provider: "fallback", Model: tt.fallbackModel},
							),
						},
					},
				},
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("validate config: %v", err)
			}

			gw := NewGateway(cfg, "", "")
			t.Cleanup(gw.Close)

			req := httptest.NewRequest(http.MethodPost, tt.requestPath, strings.NewReader(tt.requestBody))
			rec := httptest.NewRecorder()

			gw.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
			}

			record := mustSingleLogRecord(t, gw.Broadcaster().Recent())
			if record.Provider != "fallback" {
				t.Fatalf("logged provider = %q, want fallback", record.Provider)
			}
			if len(record.Failovers) != 1 {
				t.Fatalf("failover log count = %d, want 1", len(record.Failovers))
			}
			failover := record.Failovers[0]
			if failover.FailedProvider != "primary" {
				t.Fatalf("failed provider = %q, want primary", failover.FailedProvider)
			}
			if failover.FailedProviderModel != tt.primaryModel {
				t.Fatalf("failed provider model = %q, want %q", failover.FailedProviderModel, tt.primaryModel)
			}
			if failover.NextProvider != "fallback" {
				t.Fatalf("next provider = %q, want fallback", failover.NextProvider)
			}
			if failover.NextProviderModel != tt.fallbackModel {
				t.Fatalf("next provider model = %q, want %q", failover.NextProviderModel, tt.fallbackModel)
			}
			if !strings.Contains(failover.Error, "500") {
				t.Fatalf("failover error = %q, want status code", failover.Error)
			}
		})
	}
}

func TestGatewayStatefulResponsesDoNotFailover(t *testing.T) {
	t.Parallel()

	primaryHits := 0
	fallbackHits := 0

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
			return
		}
		primaryHits++
		if r.URL.Path != "/responses" {
			t.Fatalf("primary upstream path = %q, want /responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"type":"server_error","message":"primary failed"}}`)
	}))
	defer primary.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
			return
		}
		fallbackHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_fallback","status":"completed","output":[{"type":"message","content":"ok"}]}`)
	}))
	defer fallback.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"primary": {
				URL:      primary.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("primary-token"),
			},
			"fallback": {
				URL:      fallback.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("fallback-token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolResponsesStateful,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": exactModel(config.RouteProtocolResponsesStateful,
						&config.RouteUpstreamConfig{Provider: "primary", Model: "gpt-4o-primary"},
					),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodPost, "/openai/responses", strings.NewReader(`{"model":"gpt-4o","input":"hello","previous_response_id":"resp_prev"}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if primaryHits != 1 {
		t.Fatalf("primary hits = %d, want 1", primaryHits)
	}
	if fallbackHits != 0 {
		t.Fatalf("fallback hits = %d, want 0", fallbackHits)
	}

	record := mustSingleLogRecord(t, gw.Broadcaster().Recent())
	if record.Provider != "primary" {
		t.Fatalf("logged provider = %q, want primary", record.Provider)
	}
	if len(record.Failovers) != 0 {
		t.Fatalf("failover log count = %d, want 0", len(record.Failovers))
	}
}

func newFailoverUpstream(t *testing.T, expectedPath string, statusCode int, body string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
			return
		}
		if r.URL.Path != expectedPath {
			t.Fatalf("upstream path = %q, want %q", r.URL.Path, expectedPath)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = io.WriteString(w, body)
	}))
}

func mustSingleLogRecord(t *testing.T, records []reqlog.Record) reqlog.Record {
	t.Helper()

	if len(records) != 1 {
		t.Fatalf("recent log count = %d, want 1", len(records))
	}
	return records[0]
}
