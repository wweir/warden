package admin

import (
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

	probe := probeProviderModelProtocol(nil, &config.ProviderConfig{
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

func TestProbeProviderModelProtocolStatefulResponsesStoresFirstResponse(t *testing.T) {
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

	probe := probeProviderModelProtocol(nil, &config.ProviderConfig{
		URL:      server.URL,
		Protocol: "openai",
		APIKey:   config.SecretString("token"),
	}, "gpt-4o", config.RouteProtocolResponsesStateful)

	if probe.Status != "supported" {
		t.Fatalf("probe status = %q, want supported, error=%q", probe.Status, probe.Error)
	}
	if len(bodies) != 2 {
		t.Fatalf("probe request count = %d, want 2", len(bodies))
	}
	if got, _ := bodies[0]["store"].(bool); !got {
		t.Fatalf("first response probe store = %v, want true", bodies[0]["store"])
	}
	if got := bodies[0]["previous_response_id"]; got != nil {
		t.Fatalf("first response previous_response_id = %v, want nil", got)
	}
	if got := bodies[1]["previous_response_id"]; got != "resp_probe" {
		t.Fatalf("second response previous_response_id = %v, want resp_probe", got)
	}
	if got, _ := bodies[1]["store"].(bool); got {
		t.Fatalf("second response probe store = %v, want false", bodies[1]["store"])
	}
}
