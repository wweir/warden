package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/wweir/warden/config"
)

func testExactModel(protocol string, upstreams ...*config.RouteUpstreamConfig) *config.ExactRouteModelConfig {
	_ = protocol

	return &config.ExactRouteModelConfig{
		Upstreams: upstreams,
	}
}

func TestGatewayConfiguredAPIKeyValidatesAndForwardsProviderAuth(t *testing.T) {
	t.Parallel()

	const (
		clientKeyName  = "client-auth-test"
		clientKeyValue = "client-token-123"
		providerKey    = "provider-token-456"
	)

	var (
		gotAuthorization string
		gotClientAPIKey  string
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotClientAPIKey = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"gpt-4o","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":5}}`)
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		APIKeys: map[string]config.SecretString{
			clientKeyName: clientKeyValue,
		},
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString(providerKey),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": testExactModel(config.RouteProtocolChat, &config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	beforeRequests := testutil.ToFloat64(apiKeyRequestCounter.WithLabelValues(clientKeyName, "/openai", config.RouteProtocolChat, "gpt-4o", "", "chat/completions", "success"))
	beforePrompt := testutil.ToFloat64(apiKeyTokenCounter.WithLabelValues(clientKeyName, "/openai", config.RouteProtocolChat, "gpt-4o", "", "prompt"))
	beforeCompletion := testutil.ToFloat64(apiKeyTokenCounter.WithLabelValues(clientKeyName, "/openai", config.RouteProtocolChat, "gpt-4o", "", "completion"))

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+clientKeyValue)
	req.Header.Set("X-Api-Key", clientKeyValue)
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotAuthorization != "Bearer "+providerKey {
		t.Fatalf("upstream Authorization = %q", gotAuthorization)
	}
	if gotClientAPIKey != "" {
		t.Fatalf("upstream X-Api-Key should be stripped, got %q", gotClientAPIKey)
	}

	afterRequests := testutil.ToFloat64(apiKeyRequestCounter.WithLabelValues(clientKeyName, "/openai", config.RouteProtocolChat, "gpt-4o", "", "chat/completions", "success"))
	afterPrompt := testutil.ToFloat64(apiKeyTokenCounter.WithLabelValues(clientKeyName, "/openai", config.RouteProtocolChat, "gpt-4o", "", "prompt"))
	afterCompletion := testutil.ToFloat64(apiKeyTokenCounter.WithLabelValues(clientKeyName, "/openai", config.RouteProtocolChat, "gpt-4o", "", "completion"))

	if afterRequests-beforeRequests != 1 {
		t.Fatalf("api key request delta = %v, want 1", afterRequests-beforeRequests)
	}
	if afterPrompt-beforePrompt != 3 {
		t.Fatalf("api key prompt token delta = %v, want 3", afterPrompt-beforePrompt)
	}
	if afterCompletion-beforeCompletion != 5 {
		t.Fatalf("api key completion token delta = %v, want 5", afterCompletion-beforeCompletion)
	}
}

func TestGatewayConfiguredAPIKeyRejectsUnauthorizedRequest(t *testing.T) {
	t.Parallel()

	cfg := &config.ConfigStruct{
		APIKeys: map[string]config.SecretString{
			"client-reject-test": "valid-token",
		},
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      "http://127.0.0.1:1",
				Protocol: "openai",
				APIKey:   config.SecretString("provider-token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": testExactModel(config.RouteProtocolChat, &config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "invalid api key") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestMaskAPIKeysRedactsKeyValues(t *testing.T) {
	t.Parallel()

	cfg := map[string]any{
		"admin_password": "secret",
		"api_keys": map[string]any{
			"client-a": "token-a",
			"client-b": "token-b",
		},
		"provider": map[string]any{
			"openai": map[string]any{
				"api_key": "provider-secret",
			},
		},
	}

	maskAPIKeys(cfg)

	if cfg["admin_password"] != redactedPlaceholder {
		t.Fatalf("admin_password = %#v", cfg["admin_password"])
	}
	apiKeys := cfg["api_keys"].(map[string]any)
	if apiKeys["client-a"] != redactedPlaceholder || apiKeys["client-b"] != redactedPlaceholder {
		t.Fatalf("api_keys not redacted: %#v", apiKeys)
	}
	provider := cfg["provider"].(map[string]any)["openai"].(map[string]any)
	if provider["api_key"] != redactedPlaceholder {
		t.Fatalf("provider api_key = %#v", provider["api_key"])
	}
}
