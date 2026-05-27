package proxy

import (
	"encoding/json"
	"testing"
)

func TestIsNonChatModel(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		// Models that should be hidden
		{"gpt-image-1", true},
		{"gpt-image-1.5", true},
		{"gpt-image-2", true},
		{"gpt-4o-audio-preview", true},
		{"gpt-4o-realtime-preview", true},
		{"dall-e-3", true},
		{"dall-e-2", true},
		{"tts-1", true},
		{"tts-1-hd", true},
		{"whisper-1", true},
		{"text-embedding-3-small", true},
		{"text-embedding-ada-002", true},
		{"omni-moderation-latest", true},

		// Models that should be visible
		{"gpt-5.5", false},
		{"gpt-5.4", false},
		{"gpt-5.4-mini", false},
		{"gpt-5.3-codex", false},
		{"gpt-5.2", false},
		{"gpt-5.2-chat-latest", false},
		{"gpt-5.2-pro", false},
		{"codex-auto-review", false},
		{"claude-3-5-sonnet", false},
		{"glm-5.1", false},
		{"deepseek-chat", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := isNonChatModel(tt.id)
			if got != tt.want {
				t.Errorf("isNonChatModel(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestCodexCompatibleModelsFiltersNonChat(t *testing.T) {
	input := []json.RawMessage{
		json.RawMessage(`{"id":"gpt-5.5","object":"model"}`),
		json.RawMessage(`{"id":"gpt-image-1","object":"model"}`),
		json.RawMessage(`{"id":"gpt-4o-audio-preview","object":"model"}`),
	}

	out := codexCompatibleModels(input)
	if len(out) != 3 {
		t.Fatalf("expected 3 output models, got %d", len(out))
	}

	// gpt-5.5 should be visible and supported
	var gpt55 map[string]any
	if err := json.Unmarshal(out[0], &gpt55); err != nil {
		t.Fatal(err)
	}
	if gpt55["visibility"] != "list" {
		t.Errorf("gpt-5.5 visibility = %v, want list", gpt55["visibility"])
	}
	if gpt55["supported_in_api"] != true {
		t.Errorf("gpt-5.5 supported_in_api = %v, want true", gpt55["supported_in_api"])
	}

	// gpt-image-1 should be hidden and unsupported
	var img map[string]any
	if err := json.Unmarshal(out[1], &img); err != nil {
		t.Fatal(err)
	}
	if img["visibility"] != "hide" {
		t.Errorf("gpt-image-1 visibility = %v, want hide", img["visibility"])
	}
	if img["supported_in_api"] != false {
		t.Errorf("gpt-image-1 supported_in_api = %v, want false", img["supported_in_api"])
	}

	// gpt-4o-audio-preview should be hidden and unsupported
	var audio map[string]any
	if err := json.Unmarshal(out[2], &audio); err != nil {
		t.Fatal(err)
	}
	if audio["visibility"] != "hide" {
		t.Errorf("gpt-4o-audio-preview visibility = %v, want hide", audio["visibility"])
	}
	if audio["supported_in_api"] != false {
		t.Errorf("gpt-4o-audio-preview supported_in_api = %v, want false", audio["supported_in_api"])
	}
}

func TestCodexCompatibleModelsPreservesUpstreamVisibility(t *testing.T) {
	// If upstream already sets visibility, our defaults should not override it.
	input := []json.RawMessage{
		json.RawMessage(`{"id":"custom-model","visibility":"hide"}`),
	}
	out := codexCompatibleModels(input)
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(out[0], &entry); err != nil {
		t.Fatal(err)
	}
	if rawString(entry["visibility"]) != "hide" {
		t.Errorf("visibility = %q, want hide", rawString(entry["visibility"]))
	}
}
