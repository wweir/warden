package admin

import (
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
