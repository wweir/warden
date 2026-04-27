package upstream

import (
	"encoding/json"
	"testing"

	"github.com/wweir/warden/pkg/protocol/openai"
)

func TestMarshalProtocolRequest_OpenAI(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []openai.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	body, err := MarshalProtocolRequest("openai", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// should be standard OpenAI format
	var result map[string]json.RawMessage
	json.Unmarshal(body, &result)

	if _, ok := result["max_tokens"]; ok {
		t.Error("OpenAI format should not add max_tokens by default")
	}
}

func TestMarshalProtocolRequest_Anthropic(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "claude-3-opus-20240229",
		Messages: []openai.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	body, err := MarshalProtocolRequest("anthropic", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(body, &result)

	// Anthropic should have max_tokens
	var maxTokens int
	json.Unmarshal(result["max_tokens"], &maxTokens)
	if maxTokens != 4096 {
		t.Errorf("expected max_tokens=4096, got %d", maxTokens)
	}
}

func TestProtocolEndpoint(t *testing.T) {
	if got := ProtocolEndpoint("anthropic", false); got != "/messages" {
		t.Errorf("anthropic endpoint = %q, want /messages", got)
	}
	if got := ProtocolEndpoint("openai", false); got != "/chat/completions" {
		t.Errorf("openai endpoint = %q, want /chat/completions", got)
	}
	if got := ProtocolEndpoint("openai", true); got != "/responses" {
		t.Errorf("openai responses endpoint = %q, want /responses", got)
	}
}
