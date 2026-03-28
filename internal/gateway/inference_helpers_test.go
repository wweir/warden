package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wweir/warden/config"
	requestctxpkg "github.com/wweir/warden/internal/gateway/requestctx"
	"github.com/wweir/warden/pkg/protocol/openai"
)

func TestReadJSONInferenceRequest(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{"model":"gpt-4o","stream":true}`))
	req.Header.Set("X-Provider", "provider-a")

	got, err := readJSONInferenceRequest(req)
	if err != nil {
		t.Fatalf("readJSONInferenceRequest() error = %v", err)
	}
	if got.Model != "gpt-4o" {
		t.Fatalf("Model = %q", got.Model)
	}
	if !got.Stream {
		t.Fatal("Stream = false, want true")
	}
	if got.ExplicitProvider != "provider-a" {
		t.Fatalf("ExplicitProvider = %q", got.ExplicitProvider)
	}
}

func TestReadJSONInferenceRequestInvalidJSON(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{"model":`))

	_, err := readJSONInferenceRequest(req)
	if err == nil || err.Error() != "invalid JSON" {
		t.Fatalf("error = %v, want invalid JSON", err)
	}
}

func TestApplyInferenceMetricHeadersUsesProviderFallback(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
	route := &config.RouteConfig{Prefix: "/openai", Protocol: config.RouteProtocolResponsesStateless}

	labels := applyInferenceMetricHeaders(rec, req, route, config.RouteProtocolResponsesStateless, "responses", "provider-a", nil)

	if labels.Provider != "provider-a" {
		t.Fatalf("labels.Provider = %q", labels.Provider)
	}
	if got := rec.Header().Get("X-Provider"); got != "provider-a" {
		t.Fatalf("X-Provider = %q", got)
	}
	if got := rec.Header().Get("X-Route"); got != "/openai" {
		t.Fatalf("X-Route = %q", got)
	}
}

func TestBootstrapInferenceRequest(t *testing.T) {
	t.Parallel()

	route := &config.RouteConfig{
		Prefix: "/openai",
		Hooks: []*config.HookRuleConfig{
			{Match: "*", Hook: config.HookConfig{Type: "exec"}},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", strings.NewReader(`{"model":"gpt-4o","stream":true}`))
	req.Header.Set("X-Provider", "provider-a")

	got, err := bootstrapInferenceRequest(req, route)
	if err != nil {
		t.Fatalf("bootstrapInferenceRequest() error = %v", err)
	}
	if got.request == req {
		t.Fatal("request was not cloned with route context")
	}
	if got.req.Model != "gpt-4o" {
		t.Fatalf("Model = %q", got.req.Model)
	}
	if got.requestID == "" {
		t.Fatal("requestID is empty")
	}
	hooks := requestctxpkg.RouteHooksFromContext(got.request.Context())
	if len(hooks) != 1 {
		t.Fatalf("route hooks len = %d, want 1", len(hooks))
	}
}

func TestApplyRouteModelPrompt(t *testing.T) {
	t.Parallel()

	req := inferenceRequest{RawBody: []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)}
	model := &config.CompiledRouteModel{
		PromptEnabled: true,
		SystemPrompt:  "be precise",
	}

	applyRouteModelPrompt(&req, model, openai.InjectSystemPromptRaw)

	var body map[string]any
	if err := json.Unmarshal(req.RawBody, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	messages, _ := body["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(messages))
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "system" {
		t.Fatalf("first role = %v, want system", first["role"])
	}
}
