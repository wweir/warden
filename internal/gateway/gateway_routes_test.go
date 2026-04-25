package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	sel "github.com/wweir/warden/internal/selector"
)

func TestGatewayModelsExposeObservedWildcardMatch(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
		case "/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4.1-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("token"),
				Models:   []string{"gpt-4.1-mini"},
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				WildcardModels: map[string]*config.WildcardRouteModelConfig{
					"gpt-*": wildcardModel(config.RouteProtocolChat, "openai"),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	inferReq := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", strings.NewReader(`{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"hello"}]}`))
	inferRec := httptest.NewRecorder()
	gw.ServeHTTP(inferRec, inferReq)
	if inferRec.Code != http.StatusOK {
		t.Fatalf("inference status = %d, want %d, body=%q", inferRec.Code, http.StatusOK, inferRec.Body.String())
	}

	modelsReq := httptest.NewRequest(http.MethodGet, "/openai/models", nil)
	modelsRec := httptest.NewRecorder()
	gw.ServeHTTP(modelsRec, modelsReq)
	if modelsRec.Code != http.StatusOK {
		t.Fatalf("models status = %d, want %d, body=%q", modelsRec.Code, http.StatusOK, modelsRec.Body.String())
	}
	if got := gjson.Get(modelsRec.Body.String(), `data.#(id=="gpt-4.1-mini").id`).String(); got != "gpt-4.1-mini" {
		t.Fatalf("models body = %s, want observed wildcard model", modelsRec.Body.String())
	}
	if got := gjson.Get(modelsRec.Body.String(), `data.#(id=="gpt-4.1-mini").owned_by`).String(); got != "/openai" {
		t.Fatalf("owned_by = %q, want /openai", got)
	}
}

func TestObserveMatchedModelAddsWildcardMatchAfterSuccess(t *testing.T) {
	t.Parallel()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      "http://openai.example.com",
				Protocol: "openai",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				WildcardModels: map[string]*config.WildcardRouteModelConfig{
					"gpt-*": wildcardModel(config.RouteProtocolChat, "openai"),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := &Gateway{
		cfg:      cfg,
		selector: sel.NewSelector(cfg),
	}
	route := cfg.Route["/openai"]

	manager, err := gw.newInferenceManager(
		route,
		config.RouteProtocolChat,
		"chat/completions",
		inferenceRequest{Model: "gpt-4.1-mini"},
		true,
	)
	if err != nil {
		t.Fatalf("newInferenceManager() error = %v", err)
	}

	models := gw.selector.Models(route)
	bodyBytes, err := json.Marshal(map[string]any{"data": models})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	body := string(bodyBytes)
	if got := gjson.Get(body, `data.#(id=="gpt-4.1-mini").id`).String(); got != "" {
		t.Fatalf("models body = %s, want no observed wildcard model before success", body)
	}

	gw.selector.ObserveMatchedModel(manager.Current().Target)

	models = gw.selector.Models(route)
	bodyBytes, err = json.Marshal(map[string]any{"data": models})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	body = string(bodyBytes)
	if got := gjson.Get(body, `data.#(id=="gpt-4.1-mini").id`).String(); got != "gpt-4.1-mini" {
		t.Fatalf("models body = %s, want observed wildcard model after success", body)
	}
	if got := gjson.Get(body, `data.#(id=="gpt-4.1-mini").owned_by`).String(); got != "/openai" {
		t.Fatalf("owned_by = %q, want /openai", got)
	}
}

func TestGatewayProxyWildcardMatchAppearsInModelsForOllamaStyleRoute(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			http.Error(w, "models unavailable", http.StatusNotImplemented)
		case "/api/chat":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"done":true}`))
		default:
			t.Fatalf("unexpected upstream path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"ollama": {
				URL:      upstream.URL,
				Protocol: "openai",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/ollama": {
				Protocol: config.RouteProtocolChat,
				WildcardModels: map[string]*config.WildcardRouteModelConfig{
					"*": wildcardModel(config.RouteProtocolChat, "ollama"),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	proxyReq := httptest.NewRequest(http.MethodPost, "/ollama/api/chat", strings.NewReader(`{"model":"qwen2.5:7b","messages":[{"role":"user","content":"hello"}]}`))
	proxyRec := httptest.NewRecorder()
	gw.ServeHTTP(proxyRec, proxyReq)
	if proxyRec.Code != http.StatusOK {
		t.Fatalf("proxy status = %d, want %d, body=%q", proxyRec.Code, http.StatusOK, proxyRec.Body.String())
	}

	modelsReq := httptest.NewRequest(http.MethodGet, "/ollama/models", nil)
	modelsRec := httptest.NewRecorder()
	gw.ServeHTTP(modelsRec, modelsReq)
	if modelsRec.Code != http.StatusOK {
		t.Fatalf("models status = %d, want %d, body=%q", modelsRec.Code, http.StatusOK, modelsRec.Body.String())
	}
	if got := gjson.Get(modelsRec.Body.String(), `data.#(id=="qwen2.5:7b").id`).String(); got != "qwen2.5:7b" {
		t.Fatalf("models body = %s, want observed wildcard model for proxy route", modelsRec.Body.String())
	}
	if got := gjson.Get(modelsRec.Body.String(), `data.#(id=="qwen2.5:7b").owned_by`).String(); got != "/ollama" {
		t.Fatalf("owned_by = %q, want /ollama", got)
	}
}

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

func TestGatewayChatRouteExposesEmbeddingsEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotPath = r.URL.Path
			gotBody, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"text-embedding-3-large","usage":{"prompt_tokens":4,"total_tokens":4}}`))
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
					"text-embedding-3-large": exactModel(config.RouteProtocolChat,
						&config.RouteUpstreamConfig{Provider: "openai", Model: "text-embedding-3-large"},
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

	req := httptest.NewRequest(http.MethodPost, "/openai/embeddings", strings.NewReader(`{"model":"text-embedding-3-large","input":"hello"}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotPath != "/embeddings" {
		t.Fatalf("upstream path = %q, want /embeddings", gotPath)
	}
	if gjson.GetBytes(gotBody, "model").String() != "text-embedding-3-large" {
		t.Fatalf("upstream model = %q, want text-embedding-3-large", gjson.GetBytes(gotBody, "model").String())
	}
	if gjson.Get(rec.Body.String(), "data.0.object").String() != "embedding" {
		t.Fatalf("response data object = %q, want embedding", gjson.Get(rec.Body.String(), "data.0.object").String())
	}
}

func TestGatewayRouteExplicitServiceProtocolsExposeMultipleEndpoints(t *testing.T) {
	t.Parallel()

	seenPaths := map[string]bool{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			seenPaths[r.URL.Path] = true
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/chat/completions":
			_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
		case "/responses":
			_, _ = w.Write([]byte(`{"id":"resp_123","object":"response","created_at":1,"model":"gpt-4o","output":[{"id":"msg_123","type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
		case "/embeddings":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":2,"total_tokens":2}}`))
		default:
			http.NotFound(w, r)
		}
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
			"/multi": {
				Protocol:         config.RouteProtocolChat,
				ServiceProtocols: []string{config.RouteProtocolChat, config.RouteProtocolResponsesStateless, config.ServiceProtocolEmbeddings},
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": exactModel(config.RouteProtocolChat,
						&config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"},
					),
					"text-embedding-3-small": exactModel(config.RouteProtocolChat,
						&config.RouteUpstreamConfig{Provider: "openai", Model: "text-embedding-3-small"},
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

	requests := []struct {
		path string
		body string
	}{
		{"/multi/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`},
		{"/multi/responses", `{"model":"gpt-4o","input":"hello"}`},
		{"/multi/embeddings", `{"model":"text-embedding-3-small","input":"hello"}`},
	}
	for _, tc := range requests {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		gw.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d, body=%q", tc.path, rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	for _, path := range []string{"/chat/completions", "/responses", "/embeddings"} {
		if !seenPaths[path] {
			t.Fatalf("upstream path %s was not called", path)
		}
	}
}

func TestGatewayMixedServiceProvidersSelectsProviderByEndpoint(t *testing.T) {
	t.Parallel()

	var gotChatPath string
	var gotEmbeddingsPath string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			switch r.URL.Path {
			case "/chat/completions":
				gotChatPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
				return
			case "/embeddings":
				gotEmbeddingsPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":2,"total_tokens":2}}`))
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"chat": {
				URL:              upstream.URL,
				Protocol:         "openai",
				APIKey:           config.SecretString("token"),
				ServiceProtocols: []string{config.RouteProtocolChat},
			},
			"embeddings": {
				URL:              upstream.URL,
				Protocol:         "openai",
				APIKey:           config.SecretString("token"),
				ServiceProtocols: []string{config.ServiceProtocolEmbeddings},
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"text-embedding-3-small": exactModel(config.RouteProtocolChat,
						&config.RouteUpstreamConfig{Provider: "chat", Model: "gpt-4o"},
						&config.RouteUpstreamConfig{Provider: "embeddings", Model: "text-embedding-3-small"},
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

	chatReq := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", strings.NewReader(`{"model":"text-embedding-3-small","messages":[]}`))
	chatRec := httptest.NewRecorder()
	gw.ServeHTTP(chatRec, chatReq)
	if chatRec.Code != http.StatusOK {
		t.Fatalf("chat status = %d, want %d, body=%q", chatRec.Code, http.StatusOK, chatRec.Body.String())
	}
	if gotChatPath != "/chat/completions" {
		t.Fatalf("chat upstream path = %q, want /chat/completions", gotChatPath)
	}

	embedReq := httptest.NewRequest(http.MethodPost, "/openai/embeddings", strings.NewReader(`{"model":"text-embedding-3-small","input":"hello"}`))
	embedRec := httptest.NewRecorder()
	gw.ServeHTTP(embedRec, embedReq)
	if embedRec.Code != http.StatusOK {
		t.Fatalf("embeddings status = %d, want %d, body=%q", embedRec.Code, http.StatusOK, embedRec.Body.String())
	}
	if gotEmbeddingsPath != "/embeddings" {
		t.Fatalf("embeddings upstream path = %q, want /embeddings", gotEmbeddingsPath)
	}
}

func TestGatewayResponsesRouteExposesEmbeddingsEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotPath = r.URL.Path
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":2,"total_tokens":2}}`))
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
			"/responses": {
				Protocol: config.RouteProtocolResponsesStateful,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"text-embedding-3-small": exactModel(config.RouteProtocolResponsesStateful,
						&config.RouteUpstreamConfig{Provider: "openai", Model: "text-embedding-3-small"},
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

	req := httptest.NewRequest(http.MethodPost, "/responses/embeddings", strings.NewReader(`{"model":"text-embedding-3-small","input":"hello"}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotPath != "/embeddings" {
		t.Fatalf("upstream path = %q, want /embeddings", gotPath)
	}
}

func TestGatewayAnthropicRouteExposesEmbeddingsEndpointViaOpenAIProvider(t *testing.T) {
	t.Parallel()

	var gotPath string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotPath = r.URL.Path
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":2,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:             upstream.URL,
				Protocol:        "openai",
				APIKey:          config.SecretString("token"),
				AnthropicToChat: true,
			},
		},
		Route: map[string]*config.RouteConfig{
			"/claude": {
				Protocol: config.RouteProtocolAnthropic,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"text-embedding-3-small": exactModel(config.RouteProtocolAnthropic,
						&config.RouteUpstreamConfig{Provider: "openai", Model: "text-embedding-3-small"},
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

	req := httptest.NewRequest(http.MethodPost, "/claude/embeddings", strings.NewReader(`{"model":"text-embedding-3-small","input":"hello"}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotPath != "/embeddings" {
		t.Fatalf("upstream path = %q, want /embeddings", gotPath)
	}
}

func TestGatewayAnthropicRouteEmbeddingsSkipsNativeAnthropicProvider(t *testing.T) {
	t.Parallel()

	var anthropicHits int
	anthropicUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			anthropicHits++
			http.Error(w, "anthropic embeddings should not be called", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer anthropicUpstream.Close()

	var openAIPath string
	var openAIModel string
	openAIUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			openAIPath = r.URL.Path
			body, _ := io.ReadAll(r.Body)
			openAIModel = gjson.GetBytes(body, "model").String()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":2,"total_tokens":2}}`))
	}))
	defer openAIUpstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"anthropic": {
				URL:      anthropicUpstream.URL,
				Protocol: "anthropic",
				APIKey:   config.SecretString("token"),
			},
			"openai": {
				URL:             openAIUpstream.URL,
				Protocol:        "openai",
				APIKey:          config.SecretString("token"),
				AnthropicToChat: true,
			},
		},
		Route: map[string]*config.RouteConfig{
			"/claude": {
				Protocol: config.RouteProtocolAnthropic,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"embed-public": exactModel(config.RouteProtocolAnthropic,
						&config.RouteUpstreamConfig{Provider: "anthropic", Model: "claude-3-7-sonnet"},
						&config.RouteUpstreamConfig{Provider: "openai", Model: "text-embedding-3-small"},
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

	req := httptest.NewRequest(http.MethodPost, "/claude/embeddings", strings.NewReader(`{"model":"embed-public","input":"hello"}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if anthropicHits != 0 {
		t.Fatalf("anthropic upstream hits = %d, want 0", anthropicHits)
	}
	if openAIPath != "/embeddings" {
		t.Fatalf("openai upstream path = %q, want /embeddings", openAIPath)
	}
	if openAIModel != "text-embedding-3-small" {
		t.Fatalf("openai upstream model = %q, want text-embedding-3-small", openAIModel)
	}
}

func TestGatewayAnthropicRouteRejectsEmbeddingsOnNativeAnthropicProvider(t *testing.T) {
	t.Parallel()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"anthropic": {
				URL:      "https://anthropic.example.com",
				Protocol: "anthropic",
				APIKey:   config.SecretString("token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/claude": {
				Protocol: config.RouteProtocolAnthropic,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"claude-3-7-sonnet": exactModel(config.RouteProtocolAnthropic,
						&config.RouteUpstreamConfig{Provider: "anthropic", Model: "claude-3-7-sonnet"},
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

	req := httptest.NewRequest(http.MethodPost, "/claude/embeddings", strings.NewReader(`{"model":"claude-3-7-sonnet","input":"hello"}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "does not support embeddings requests") {
		t.Fatalf("body = %q, want unsupported embeddings message", rec.Body.String())
	}
}

func TestGatewayResponsesToChatRejectsUnsupportedStatelessField(t *testing.T) {
	t.Parallel()

	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			upstreamHits++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
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

	req := httptest.NewRequest(http.MethodPost, "/openai/responses", strings.NewReader(`{"model":"qwen3-coder-plus","input":"hello","reasoning":{"effort":"medium"}}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "reasoning") {
		t.Fatalf("body = %q, want unsupported field message", rec.Body.String())
	}
	if upstreamHits != 0 {
		t.Fatalf("upstream hits = %d, want 0", upstreamHits)
	}
}

func TestGatewayResponsesToChatMapsMaxOutputTokensAndNormalizesToolChoice(t *testing.T) {
	t.Parallel()

	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotBody, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
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

	req := httptest.NewRequest(http.MethodPost, "/openai/responses", strings.NewReader(`{
		"model":"qwen3-coder-plus",
		"input":"hello",
		"max_output_tokens":64,
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],
		"tool_choice":{"type":"function","name":"lookup"}
	}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := gjson.GetBytes(gotBody, "max_completion_tokens").Int(); got != 64 {
		t.Fatalf("max_completion_tokens = %d, want 64", got)
	}
	if gjson.GetBytes(gotBody, "max_output_tokens").Exists() {
		t.Fatalf("upstream body should not include max_output_tokens: %s", string(gotBody))
	}
	if got := gjson.GetBytes(gotBody, "tool_choice.type").String(); got != "function" {
		t.Fatalf("tool_choice.type = %q, want function", got)
	}
	if got := gjson.GetBytes(gotBody, "tool_choice.function.name").String(); got != "lookup" {
		t.Fatalf("tool_choice.function.name = %q, want lookup", got)
	}
}

func TestGatewayResponsesToChatConvertsInstructionsToDeveloperMessage(t *testing.T) {
	t.Parallel()

	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotBody, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
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

	req := httptest.NewRequest(http.MethodPost, "/openai/responses", strings.NewReader(`{"model":"qwen3-coder-plus","input":"hello","instructions":"be precise"}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := gjson.GetBytes(gotBody, "messages.0.role").String(); got != "developer" {
		t.Fatalf("first message role = %q, want developer", got)
	}
	if got := gjson.GetBytes(gotBody, "messages.0.content").String(); got != "be precise" {
		t.Fatalf("first message content = %q, want be precise", got)
	}
	if got := gjson.GetBytes(gotBody, "messages.1.role").String(); got != "user" {
		t.Fatalf("second message role = %q, want user", got)
	}
}

func TestGatewayResponsesToChatNormalizesUsageAndFinishReason(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl_123",
			"object":"chat.completion",
			"created":1,
			"model":"gpt-4o",
			"choices":[{"index":0,"message":{"role":"assistant","content":"partial"},"finish_reason":"length"}],
			"usage":{
				"prompt_tokens":7,
				"completion_tokens":11,
				"total_tokens":18,
				"prompt_tokens_details":{"cached_tokens":3},
				"completion_tokens_details":{"reasoning_tokens":5}
			}
		}`))
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
	body := rec.Body.Bytes()
	if got := gjson.GetBytes(body, "status").String(); got != "incomplete" {
		t.Fatalf("status = %q, want incomplete", got)
	}
	if got := gjson.GetBytes(body, "incomplete_details.reason").String(); got != "max_output_tokens" {
		t.Fatalf("incomplete_details.reason = %q, want max_output_tokens", got)
	}
	if got := gjson.GetBytes(body, "usage.input_tokens").Int(); got != 7 {
		t.Fatalf("usage.input_tokens = %d, want 7", got)
	}
	if got := gjson.GetBytes(body, "usage.output_tokens").Int(); got != 11 {
		t.Fatalf("usage.output_tokens = %d, want 11", got)
	}
	if got := gjson.GetBytes(body, "usage.input_tokens_details.cached_tokens").Int(); got != 3 {
		t.Fatalf("usage.input_tokens_details.cached_tokens = %d, want 3", got)
	}
	if got := gjson.GetBytes(body, "usage.output_tokens_details.reasoning_tokens").Int(); got != 5 {
		t.Fatalf("usage.output_tokens_details.reasoning_tokens = %d, want 5", got)
	}
}

func TestGatewayResponsesToChatRetriesDeveloperRoleAsSystem(t *testing.T) {
	t.Parallel()

	var gotBodies [][]byte
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			gotBodies = append(gotBodies, body)
			attempt := len(gotBodies)
			mu.Unlock()
			if attempt == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"developer role is not supported","type":"invalid_request_error"}}`))
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
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

	req := httptest.NewRequest(http.MethodPost, "/openai/responses", strings.NewReader(`{"model":"qwen3-coder-plus","input":"hello","instructions":"be precise"}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(gotBodies) != 2 {
		t.Fatalf("upstream attempts = %d, want 2", len(gotBodies))
	}
	if got := gjson.GetBytes(gotBodies[0], "messages.0.role").String(); got != "developer" {
		t.Fatalf("first attempt role = %q, want developer", got)
	}
	if got := gjson.GetBytes(gotBodies[1], "messages.0.role").String(); got != "system" {
		t.Fatalf("second attempt role = %q, want system", got)
	}
}

func TestGatewayAnthropicToChatLogsBridgeConversionFailure(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl_123",
			"object":"chat.completion",
			"created":1,
			"model":"gpt-4o",
			"choices":[{"index":0,"message":{"role":"assistant","content":{"type":"image"}},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}
		}`))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:             upstream.URL,
				Protocol:        "openai",
				APIKey:          config.SecretString("token"),
				AnthropicToChat: true,
			},
		},
		Route: map[string]*config.RouteConfig{
			"/anthropic": {
				Protocol: config.RouteProtocolAnthropic,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"claude-compatible": exactModel(config.RouteProtocolAnthropic,
						&config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"},
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

	req := httptest.NewRequest(http.MethodPost, "/anthropic/messages", strings.NewReader(`{"model":"claude-compatible","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusBadGateway, rec.Body.String())
	}

	record := mustSingleLogRecord(t, gw.Broadcaster().Recent())
	if !strings.Contains(record.Error, "assistant content type") {
		t.Fatalf("log error = %q, want bridge conversion error", record.Error)
	}
	if len(record.Response) == 0 {
		t.Fatal("expected failed bridge conversion to retain upstream response in log")
	}

	status := gw.selector.ProviderDetail("openai")
	if status == nil {
		t.Fatal("provider status is nil")
	}
	if status.SuccessCount != 1 {
		t.Fatalf("Successes = %d, want 1", status.SuccessCount)
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
		if r.Method == http.MethodPost {
			upstreamHits++
		}
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

func TestGatewayAnthropicRouteExposesMessagesEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotPath = r.URL.Path
			gotBody, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_123","type":"message","role":"assistant","model":"claude-3-5-sonnet","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":3}}`))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"anthropic": {
				URL:      upstream.URL,
				Protocol: "anthropic",
				APIKey:   config.SecretString("token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/anthropic": {
				Protocol: config.RouteProtocolAnthropic,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"claude-3-5-sonnet": exactModel(config.RouteProtocolAnthropic,
						&config.RouteUpstreamConfig{Provider: "anthropic", Model: "claude-3-5-sonnet"},
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

	req := httptest.NewRequest(http.MethodPost, "/anthropic/messages", strings.NewReader(`{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotPath != "/messages" {
		t.Fatalf("upstream path = %q, want /messages", gotPath)
	}
	if gjson.GetBytes(gotBody, "model").String() != "claude-3-5-sonnet" {
		t.Fatalf("upstream model = %q, want claude-3-5-sonnet", gjson.GetBytes(gotBody, "model").String())
	}
	if gjson.Get(rec.Body.String(), "content.0.text").String() != "ok" {
		t.Fatalf("response text = %q, want ok", gjson.Get(rec.Body.String(), "content.0.text").String())
	}
}

func TestGatewayAnthropicRouteBridgesToChatProvider(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotPath = r.URL.Path
			gotBody, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:             upstream.URL,
				Protocol:        "openai",
				APIKey:          config.SecretString("token"),
				AnthropicToChat: true,
			},
		},
		Route: map[string]*config.RouteConfig{
			"/anthropic": {
				Protocol: config.RouteProtocolAnthropic,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"claude-compatible": exactModel(config.RouteProtocolAnthropic,
						&config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"},
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

	req := httptest.NewRequest(http.MethodPost, "/anthropic/messages", strings.NewReader(`{"model":"claude-compatible","system":"be concise","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("upstream path = %q, want /chat/completions", gotPath)
	}
	if gjson.GetBytes(gotBody, "model").String() != "gpt-4o" {
		t.Fatalf("upstream model = %q, want gpt-4o", gjson.GetBytes(gotBody, "model").String())
	}
	if gjson.GetBytes(gotBody, "messages.0.role").String() != "system" {
		t.Fatalf("first message role = %q, want system", gjson.GetBytes(gotBody, "messages.0.role").String())
	}
	if gjson.GetBytes(gotBody, "messages.1.role").String() != "user" {
		t.Fatalf("second message role = %q, want user", gjson.GetBytes(gotBody, "messages.1.role").String())
	}
	if gjson.Get(rec.Body.String(), "type").String() != "message" {
		t.Fatalf("response type = %q, want message", gjson.Get(rec.Body.String(), "type").String())
	}
	if gjson.Get(rec.Body.String(), "content.0.text").String() != "ok" {
		t.Fatalf("response text = %q, want ok", gjson.Get(rec.Body.String(), "content.0.text").String())
	}
	if gjson.Get(rec.Body.String(), "stop_reason").String() != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn", gjson.Get(rec.Body.String(), "stop_reason").String())
	}
}

func TestGatewayProxyPrefersLongestMatchingRoutePrefix(t *testing.T) {
	t.Parallel()

	shortUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		t.Fatalf("short prefix upstream should not be selected: path=%q", r.URL.Path)
	}))
	defer shortUpstream.Close()

	var gotPath string
	longUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("long-prefix"))
	}))
	defer longUpstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"short": {
				URL:      shortUpstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("token-short"),
			},
			"long": {
				URL:      longUpstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("token-long"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/api": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-short": exactModel(config.RouteProtocolChat, &config.RouteUpstreamConfig{Provider: "short", Model: "gpt-short"}),
				},
			},
			"/api/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-long": exactModel(config.RouteProtocolChat, &config.RouteUpstreamConfig{Provider: "long", Model: "gpt-long"}),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodGet, "/api/openai/files", nil)
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotPath != "/files" {
		t.Fatalf("upstream path = %q, want /files", gotPath)
	}
	if rec.Body.String() != "long-prefix" {
		t.Fatalf("body = %q, want long-prefix", rec.Body.String())
	}
}

func TestGatewayAnthropicToChatFailoverKeepsPublicModelName(t *testing.T) {
	t.Parallel()

	var primaryHits int
	var fallbackHits int
	var fallbackBody []byte

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		primaryHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"type":"server_error","message":"primary failed"}}`))
	}))
	defer primary.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		fallbackHits++
		fallbackBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4.1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`))
	}))
	defer fallback.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"primary": {
				URL:             primary.URL,
				Protocol:        "openai",
				APIKey:          config.SecretString("token"),
				AnthropicToChat: true,
			},
			"fallback": {
				URL:             fallback.URL,
				Protocol:        "openai",
				APIKey:          config.SecretString("token"),
				AnthropicToChat: true,
			},
		},
		Route: map[string]*config.RouteConfig{
			"/anthropic": {
				Protocol: config.RouteProtocolAnthropic,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"claude-compatible": exactModel(config.RouteProtocolAnthropic,
						&config.RouteUpstreamConfig{Provider: "primary", Model: "gpt-4o"},
						&config.RouteUpstreamConfig{Provider: "fallback", Model: "gpt-4.1"},
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

	req := httptest.NewRequest(http.MethodPost, "/anthropic/messages", strings.NewReader(`{"model":"claude-compatible","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if primaryHits != 1 {
		t.Fatalf("primary hits = %d, want 1", primaryHits)
	}
	if fallbackHits != 1 {
		t.Fatalf("fallback hits = %d, want 1", fallbackHits)
	}
	if got := gjson.GetBytes(fallbackBody, "model").String(); got != "gpt-4.1" {
		t.Fatalf("fallback model = %q, want gpt-4.1", got)
	}
	if gjson.Get(rec.Body.String(), "content.0.text").String() != "ok" {
		t.Fatalf("response text = %q, want ok", gjson.Get(rec.Body.String(), "content.0.text").String())
	}
}

func TestGatewayResponsesFailoverSwitchesToResponsesToChatProvider(t *testing.T) {
	t.Parallel()

	var primaryHits int
	var fallbackHits int
	var fallbackPath string
	var fallbackBody []byte

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		primaryHits++
		if r.URL.Path != "/responses" {
			t.Fatalf("primary upstream path = %q, want /responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"type":"server_error","message":"primary failed"}}`))
	}))
	defer primary.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		fallbackHits++
		fallbackPath = r.URL.Path
		fallbackBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_789","object":"chat.completion","created":1,"model":"gpt-4.1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))
	}))
	defer fallback.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"primary": {
				URL:      primary.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("token-primary"),
			},
			"fallback": {
				URL:             fallback.URL,
				Protocol:        "openai",
				APIKey:          config.SecretString("token-fallback"),
				ResponsesToChat: true,
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolResponsesStateless,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-public": exactModel(config.RouteProtocolResponsesStateless,
						&config.RouteUpstreamConfig{Provider: "primary", Model: "gpt-4o"},
						&config.RouteUpstreamConfig{Provider: "fallback", Model: "gpt-4.1"},
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

	req := httptest.NewRequest(http.MethodPost, "/openai/responses", strings.NewReader(`{"model":"gpt-public","input":"hello"}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if primaryHits != 1 {
		t.Fatalf("primary hits = %d, want 1", primaryHits)
	}
	if fallbackHits != 1 {
		t.Fatalf("fallback hits = %d, want 1", fallbackHits)
	}
	if fallbackPath != "/chat/completions" {
		t.Fatalf("fallback path = %q, want /chat/completions", fallbackPath)
	}
	if got := gjson.GetBytes(fallbackBody, "model").String(); got != "gpt-4.1" {
		t.Fatalf("fallback model = %q, want gpt-4.1", got)
	}
	if got := gjson.Get(rec.Body.String(), "output.0.type").String(); got != "message" {
		t.Fatalf("response output type = %q, want message", got)
	}
}

func TestGatewayAnthropicFailoverSwitchesToAnthropicToChatProvider(t *testing.T) {
	t.Parallel()

	var primaryHits int
	var fallbackHits int
	var fallbackPath string
	var fallbackBody []byte

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		primaryHits++
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("primary upstream path = %q, want /v1/messages", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"primary failed"}}`))
	}))
	defer primary.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		fallbackHits++
		fallbackPath = r.URL.Path
		fallbackBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_999","object":"chat.completion","created":1,"model":"gpt-4.1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))
	}))
	defer fallback.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"primary": {
				URL:      primary.URL + "/v1",
				Protocol: "anthropic",
				APIKey:   config.SecretString("token-primary"),
			},
			"fallback": {
				URL:             fallback.URL,
				Protocol:        "openai",
				APIKey:          config.SecretString("token-fallback"),
				AnthropicToChat: true,
			},
		},
		Route: map[string]*config.RouteConfig{
			"/anthropic": {
				Protocol: config.RouteProtocolAnthropic,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"claude-public": exactModel(config.RouteProtocolAnthropic,
						&config.RouteUpstreamConfig{Provider: "primary", Model: "claude-3-5-sonnet"},
						&config.RouteUpstreamConfig{Provider: "fallback", Model: "gpt-4.1"},
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

	req := httptest.NewRequest(http.MethodPost, "/anthropic/messages", strings.NewReader(`{"model":"claude-public","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if primaryHits != 1 {
		t.Fatalf("primary hits = %d, want 1", primaryHits)
	}
	if fallbackHits != 1 {
		t.Fatalf("fallback hits = %d, want 1", fallbackHits)
	}
	if fallbackPath != "/chat/completions" {
		t.Fatalf("fallback path = %q, want /chat/completions", fallbackPath)
	}
	if got := gjson.GetBytes(fallbackBody, "model").String(); got != "gpt-4.1" {
		t.Fatalf("fallback model = %q, want gpt-4.1", got)
	}
	if got := gjson.Get(rec.Body.String(), "content.0.text").String(); got != "ok" {
		t.Fatalf("response text = %q, want ok", got)
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
