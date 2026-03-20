package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
)

func TestGatewayResponsesRouteExposesResponsesEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotPath = r.URL.Path
			gotBody, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"ali-coding": {
				URL:             upstream.URL,
				Protocol:        "openai",
				APIKey:          config.SecretString("token"),
				ResponsesToChat: true,
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolResponsesStateless,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"qwen3-coder-plus": exactModel(config.RouteProtocolResponsesStateless,
						&config.RouteUpstreamConfig{Provider: "ali-coding", Model: "qwen3.5-plus"},
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

	req := httptest.NewRequest(http.MethodPost, "/openai/responses", strings.NewReader(`{"model":"qwen3-coder-plus","input":"hello"}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("upstream path = %q, want /chat/completions", gotPath)
	}
	if gjson.GetBytes(gotBody, "model").String() != "qwen3.5-plus" {
		t.Fatalf("upstream model = %q, want qwen3.5-plus", gjson.GetBytes(gotBody, "model").String())
	}
	if gjson.GetBytes(gotBody, "messages.0.role").String() != "user" {
		t.Fatalf("first message role = %q, want user", gjson.GetBytes(gotBody, "messages.0.role").String())
	}
	if gjson.GetBytes(gotBody, "messages.0.content").String() != "hello" {
		t.Fatalf("first message content = %q, want hello", gjson.GetBytes(gotBody, "messages.0.content").String())
	}
	if gjson.Get(rec.Body.String(), "output.0.type").String() != "message" {
		t.Fatalf("response output type = %q, want message", gjson.Get(rec.Body.String(), "output.0.type").String())
	}
}

func TestGatewayStatefulResponsesBypassResponsesToChatConversion(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotPath = r.URL.Path
			gotBody, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_123","status":"completed","output":[{"type":"message","content":"ok"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolResponsesStateful,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": exactModel(config.RouteProtocolResponsesStateful, &config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
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

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotPath != "/responses" {
		t.Fatalf("upstream path = %q, want /responses", gotPath)
	}
	if gjson.GetBytes(gotBody, "previous_response_id").String() != "resp_prev" {
		t.Fatalf("previous_response_id = %q, want resp_prev", gjson.GetBytes(gotBody, "previous_response_id").String())
	}
	if gjson.Get(rec.Body.String(), "id").String() != "resp_123" {
		t.Fatalf("response id = %q, want resp_123", gjson.Get(rec.Body.String(), "id").String())
	}
}

func TestGatewayChatRouteRejectsStatefulResponsesRequests(t *testing.T) {
	t.Parallel()

	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_123","status":"completed","output":[{"type":"message","content":"ok"}]}`))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": exactModel(config.RouteProtocolChat, &config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
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

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "does not support stateful responses requests") {
		t.Fatalf("body = %q, want stateful unsupported message", rec.Body.String())
	}
	if upstreamHits != 0 {
		t.Fatalf("upstream hits = %d, want 0", upstreamHits)
	}
}

func TestGatewayChatRouteExposesChatEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotPath = r.URL.Path
			gotBody, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"gmn": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-5.4": exactModel(config.RouteProtocolChat,
						&config.RouteUpstreamConfig{Provider: "gmn", Model: "gpt-5.4"},
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

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("upstream path = %q, want /chat/completions", gotPath)
	}
	if gjson.GetBytes(gotBody, "messages.0.role").String() != "user" {
		t.Fatalf("first message role = %q, want user", gjson.GetBytes(gotBody, "messages.0.role").String())
	}
	if gjson.GetBytes(gotBody, "messages.0.content").String() != "hello" {
		t.Fatalf("first message content = %q, want hello", gjson.GetBytes(gotBody, "messages.0.content").String())
	}
	if gjson.Get(rec.Body.String(), "choices.0.message.content").String() != "ok" {
		t.Fatalf("response content = %q, want ok", gjson.Get(rec.Body.String(), "choices.0.message.content").String())
	}
}

func TestGatewayProxyPassesThroughNonInferencePaths(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		paths []string
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": exactModel(config.RouteProtocolChat, &config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodGet, "/openai/v1/models", nil)
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	for _, path := range paths {
		if path == "/v1/models" {
			return
		}
	}
	t.Fatalf("upstream paths = %v, want one request to /v1/models", paths)
}

func TestGatewayChatRouteInjectsSystemPromptOnlyWhenEnabled(t *testing.T) {
	t.Parallel()

	enabled := true
	disabled := false
	var gotBodies [][]byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			gotBodies = append(gotBodies, body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"enabled-model": {
						PromptEnabled: &enabled,
						SystemPrompt:  "enabled prompt",
						Upstreams: []*config.RouteUpstreamConfig{
							{Provider: "openai", Model: "gpt-4o"},
						},
					},
					"disabled-model": {
						PromptEnabled: &disabled,
						SystemPrompt:  "disabled prompt",
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

	reqEnabled := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", strings.NewReader(`{"model":"enabled-model","messages":[{"role":"user","content":"hello"}]}`))
	recEnabled := httptest.NewRecorder()
	gw.ServeHTTP(recEnabled, reqEnabled)
	if recEnabled.Code != http.StatusOK {
		t.Fatalf("enabled status = %d, want %d, body=%q", recEnabled.Code, http.StatusOK, recEnabled.Body.String())
	}

	reqDisabled := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", strings.NewReader(`{"model":"disabled-model","messages":[{"role":"user","content":"hello"}]}`))
	recDisabled := httptest.NewRecorder()
	gw.ServeHTTP(recDisabled, reqDisabled)
	if recDisabled.Code != http.StatusOK {
		t.Fatalf("disabled status = %d, want %d, body=%q", recDisabled.Code, http.StatusOK, recDisabled.Body.String())
	}

	if len(gotBodies) != 2 {
		t.Fatalf("got %d upstream requests, want 2", len(gotBodies))
	}
	if gjson.GetBytes(gotBodies[0], "messages.0.role").String() != "system" {
		t.Fatalf("enabled first role = %q, want system", gjson.GetBytes(gotBodies[0], "messages.0.role").String())
	}
	if gjson.GetBytes(gotBodies[0], "messages.0.content").String() != "enabled prompt" {
		t.Fatalf("enabled first content = %q, want enabled prompt", gjson.GetBytes(gotBodies[0], "messages.0.content").String())
	}
	if gjson.GetBytes(gotBodies[1], "messages.0.role").String() != "user" {
		t.Fatalf("disabled first role = %q, want user", gjson.GetBytes(gotBodies[1], "messages.0.role").String())
	}
	if gjson.GetBytes(gotBodies[1], "messages.0.content").String() != "hello" {
		t.Fatalf("disabled first content = %q, want hello", gjson.GetBytes(gotBodies[1], "messages.0.content").String())
	}
}

func TestGatewayResponsesRouteInjectsSystemPromptOnlyWhenEnabled(t *testing.T) {
	t.Parallel()

	enabled := true
	disabled := false
	var gotBodies [][]byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			gotBodies = append(gotBodies, body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_123","status":"completed","output":[{"type":"message","content":"ok"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolResponsesStateless,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"enabled-model": {
						PromptEnabled: &enabled,
						SystemPrompt:  "enabled prompt",
						Upstreams: []*config.RouteUpstreamConfig{
							{Provider: "openai", Model: "gpt-4o"},
						},
					},
					"disabled-model": {
						PromptEnabled: &disabled,
						SystemPrompt:  "disabled prompt",
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

	reqEnabled := httptest.NewRequest(http.MethodPost, "/openai/responses", strings.NewReader(`{"model":"enabled-model","input":"hello"}`))
	recEnabled := httptest.NewRecorder()
	gw.ServeHTTP(recEnabled, reqEnabled)
	if recEnabled.Code != http.StatusOK {
		t.Fatalf("enabled status = %d, want %d, body=%q", recEnabled.Code, http.StatusOK, recEnabled.Body.String())
	}

	reqDisabled := httptest.NewRequest(http.MethodPost, "/openai/responses", strings.NewReader(`{"model":"disabled-model","input":"hello"}`))
	recDisabled := httptest.NewRecorder()
	gw.ServeHTTP(recDisabled, reqDisabled)
	if recDisabled.Code != http.StatusOK {
		t.Fatalf("disabled status = %d, want %d, body=%q", recDisabled.Code, http.StatusOK, recDisabled.Body.String())
	}

	if len(gotBodies) != 2 {
		t.Fatalf("got %d upstream requests, want 2", len(gotBodies))
	}
	if gjson.GetBytes(gotBodies[0], "input.0.role").String() != "developer" {
		t.Fatalf("enabled first role = %q, want developer", gjson.GetBytes(gotBodies[0], "input.0.role").String())
	}
	if gjson.GetBytes(gotBodies[0], "input.0.content").String() != "enabled prompt" {
		t.Fatalf("enabled first content = %q, want enabled prompt", gjson.GetBytes(gotBodies[0], "input.0.content").String())
	}
	if gjson.GetBytes(gotBodies[1], "input").String() != "hello" {
		t.Fatalf("disabled input = %q, want hello", gjson.GetBytes(gotBodies[1], "input").String())
	}
}
