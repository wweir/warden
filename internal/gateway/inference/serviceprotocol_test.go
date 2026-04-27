package inference

import (
	"testing"

	"github.com/wweir/warden/config"
)

func TestServiceProtocolFromRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		body []byte
		want string
	}{
		{name: "chat", path: "/v1/chat/completions", body: []byte(`{"model":"gpt"}`), want: config.RouteProtocolChat},
		{name: "responses stateless", path: "/v1/responses", body: []byte(`{"model":"gpt"}`), want: config.RouteProtocolResponsesStateless},
		{name: "responses stateful", path: "/v1/responses", body: []byte(`{"model":"gpt","previous_response_id":"resp_1"}`), want: config.RouteProtocolResponsesStateful},
		{name: "embeddings", path: "/v1/embeddings", body: []byte(`{"model":"embed"}`), want: config.ServiceProtocolEmbeddings},
		{name: "anthropic", path: "/messages", body: []byte(`{"model":"claude"}`), want: config.RouteProtocolAnthropic},
		{name: "passthrough", path: "/v1/files", body: []byte(`{}`), want: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ServiceProtocolFromRequest(tt.path, tt.body); got != tt.want {
				t.Fatalf("ServiceProtocolFromRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnsupportedRouteProtocolMessage(t *testing.T) {
	t.Parallel()

	got := UnsupportedRouteProtocolMessage(config.RouteProtocolChat, config.RouteProtocolResponsesStateful)
	want := "route protocol chat does not support stateful responses requests"
	if got != want {
		t.Fatalf("UnsupportedRouteProtocolMessage() = %q, want %q", got, want)
	}
}
