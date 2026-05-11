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
		want string
	}{
		{name: "chat", path: "/v1/chat/completions", want: config.RouteProtocolChat},
		{name: "responses", path: "/v1/responses", want: config.RouteProtocolResponses},
		{name: "embeddings", path: "/v1/embeddings", want: config.ServiceProtocolEmbeddings},
		{name: "anthropic", path: "/messages", want: config.RouteProtocolAnthropic},
		{name: "passthrough", path: "/v1/files", want: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ServiceProtocolFromRequest(tt.path); got != tt.want {
				t.Fatalf("ServiceProtocolFromRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnsupportedRouteProtocolMessage(t *testing.T) {
	t.Parallel()

	got := UnsupportedRouteProtocolMessage(config.RouteProtocolChat, config.RouteProtocolResponses)
	want := "route protocol chat does not support responses requests"
	if got != want {
		t.Fatalf("UnsupportedRouteProtocolMessage() = %q, want %q", got, want)
	}
}

func TestIsStatefulResponsesRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body []byte
		want bool
	}{
		{name: "no field", body: []byte(`{"model":"gpt","input":"hi"}`), want: false},
		{name: "non-empty id", body: []byte(`{"model":"gpt","previous_response_id":"resp_1","input":"hi"}`), want: true},
		{name: "empty id", body: []byte(`{"model":"gpt","previous_response_id":""}`), want: false},
		{name: "null id", body: []byte(`{"model":"gpt","previous_response_id":null}`), want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsStatefulResponsesRequest(tt.body); got != tt.want {
				t.Fatalf("IsStatefulResponsesRequest(%s) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}
