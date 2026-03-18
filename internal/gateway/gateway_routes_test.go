package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
)

func TestGatewayChatRouteExposesResponsesWhenProviderCanServeIt(t *testing.T) {
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
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"qwen3-coder-plus": {
						Upstreams: []*config.RouteUpstreamConfig{
							{Provider: "ali-coding", Model: "qwen3.5-plus"},
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

func TestGatewayResponsesRouteExposesChatWhenProviderCanServeIt(t *testing.T) {
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
			"gmn": {
				URL:             upstream.URL,
				Protocol:        "openai",
				APIKey:          config.SecretString("token"),
				ChatToResponses: true,
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolResponses,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-5.4": {
						Upstreams: []*config.RouteUpstreamConfig{
							{Provider: "gmn", Model: "gpt-5.4"},
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

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotPath != "/responses" {
		t.Fatalf("upstream path = %q, want /responses", gotPath)
	}

	var reqBody map[string]json.RawMessage
	if err := json.Unmarshal(gotBody, &reqBody); err != nil {
		t.Fatalf("unmarshal upstream request: %v", err)
	}
	if gjson.GetBytes(reqBody["input"], "0.role").String() != "user" {
		t.Fatalf("first input role = %q, want user", gjson.GetBytes(reqBody["input"], "0.role").String())
	}
	if gjson.GetBytes(reqBody["input"], "0.content").String() != "hello" {
		t.Fatalf("first input content = %q, want hello", gjson.GetBytes(reqBody["input"], "0.content").String())
	}
	if gjson.Get(rec.Body.String(), "choices.0.message.content").String() != "ok" {
		t.Fatalf("response content = %q, want ok", gjson.Get(rec.Body.String(), "choices.0.message.content").String())
	}
}
