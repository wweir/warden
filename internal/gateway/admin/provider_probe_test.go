package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
)

func TestProbeProviderModelProtocolSupportsAnthropicToChatProvider(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	probe := probeProviderModelProtocol(context.TODO(), &config.ProviderConfig{
		URL:             server.URL,
		Protocol:        "openai",
		APIKey:          config.SecretString("token"),
		AnthropicToChat: true,
	}, "gpt-4o", config.RouteProtocolAnthropic)

	if probe.Status != "supported" {
		t.Fatalf("probe status = %q, want supported, error=%q", probe.Status, probe.Error)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("upstream path = %q, want /chat/completions", gotPath)
	}
	if got := gjson.GetBytes(gotBody, "model").String(); got != "gpt-4o" {
		t.Fatalf("probe model = %q, want gpt-4o", got)
	}
	if got := gjson.GetBytes(gotBody, "messages.0.role").String(); got != "user" {
		t.Fatalf("first role = %q, want user", got)
	}
}

func TestProbeProviderModelProtocolChatUsesMinimalPayload(t *testing.T) {
	t.Parallel()

	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("upstream path = %q, want /chat/completions", r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	probe := probeProviderModelProtocol(context.TODO(), &config.ProviderConfig{
		URL:      server.URL,
		Protocol: "openai",
		APIKey:   config.SecretString("token"),
	}, "gpt-4o", config.RouteProtocolChat)

	if probe.Status != "supported" {
		t.Fatalf("probe status = %q, want supported, error=%q", probe.Status, probe.Error)
	}
	if got := gjson.GetBytes(gotBody, "messages.0.content").String(); got != "ping" {
		t.Fatalf("probe message content = %q, want ping", got)
	}
	if got := gjson.GetBytes(gotBody, "store"); got.Exists() {
		t.Fatalf("probe store = %s, want absent", got.Raw)
	}
	if got := gjson.GetBytes(gotBody, "max_tokens"); got.Exists() {
		t.Fatalf("probe max_tokens = %s, want absent", got.Raw)
	}
	if got := gjson.GetBytes(gotBody, "max_completion_tokens"); got.Exists() {
		t.Fatalf("probe max_completion_tokens = %s, want absent", got.Raw)
	}
}

func TestProbeProviderModelProtocolResponsesSendsSingleStatelessRequest(t *testing.T) {
	t.Parallel()

	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("upstream path = %q, want /responses", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_probe","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()

	probe := probeProviderModelProtocol(context.TODO(), &config.ProviderConfig{
		URL:      server.URL,
		Protocol: "openai",
		APIKey:   config.SecretString("token"),
	}, "gpt-4o", config.RouteProtocolResponses)

	if probe.Status != "supported" {
		t.Fatalf("probe status = %q, want supported, error=%q", probe.Status, probe.Error)
	}
	if len(bodies) != 1 {
		t.Fatalf("probe request count = %d, want 1", len(bodies))
	}
	if got, _ := bodies[0]["store"].(bool); got {
		t.Fatalf("probe store = %v, want false", bodies[0]["store"])
	}
	if got := bodies[0]["previous_response_id"]; got != nil {
		t.Fatalf("probe previous_response_id = %v, want absent", got)
	}
}
