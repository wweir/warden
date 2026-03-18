package openai

import (
	"testing"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/pkg/protocol"
)

func TestAssembleResponsesStreamSupportsDataOnlyCompletedEvent(t *testing.T) {
	t.Parallel()

	rawSSE := []byte(
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_123\",\"object\":\"response\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":5,\"total_tokens\":8}}}\n\n",
	)

	events := protocol.ParseEvents(rawSSE)
	resp := ExtractCompletedResponse(events)
	if resp == nil {
		t.Fatal("expected completed response from data-only SSE")
	}
	if resp.ID != "resp_123" {
		t.Fatalf("response id = %q, want resp_123", resp.ID)
	}

	assembled, err := AssembleResponsesStream(rawSSE)
	if err != nil {
		t.Fatalf("AssembleResponsesStream error = %v", err)
	}

	if got := gjson.GetBytes(assembled, "id").String(); got != "resp_123" {
		t.Fatalf("assembled id = %q, want resp_123", got)
	}
	if got := gjson.GetBytes(assembled, "output.0.content.0.text").String(); got != "ok" {
		t.Fatalf("assembled text = %q, want ok", got)
	}
	if got := gjson.GetBytes(assembled, "usage.input_tokens").Int(); got != 3 {
		t.Fatalf("assembled prompt tokens = %d, want 3", got)
	}
}
