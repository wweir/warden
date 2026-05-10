package admin

import (
	"errors"
	"testing"
)

type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func TestGenerateAPIKeyReturnsErrorWhenEntropyUnavailable(t *testing.T) {
	t.Parallel()

	previous := apiKeyRandomReader
	apiKeyRandomReader = failingReader{}
	t.Cleanup(func() {
		apiKeyRandomReader = previous
	})

	key, err := GenerateAPIKey()
	if err == nil {
		t.Fatal("GenerateAPIKey() error = nil, want error")
	}
	if key != "" {
		t.Fatalf("GenerateAPIKey() key = %q, want empty", key)
	}
}

func TestSanitizeConfigJSONPreservesMaskedProviderAPIKey(t *testing.T) {
	newCfg := map[string]any{
		"provider": map[string]any{
			"openai": map[string]any{
				"family": "openai",
			},
		},
	}
	currentCfg := map[string]any{
		"provider": map[string]any{
			"openai": map[string]any{
				"api_key": "secret",
			},
		},
	}

	SanitizeConfigJSON(newCfg, currentCfg)

	providerCfg := newCfg["provider"].(map[string]any)["openai"].(map[string]any)
	if got := providerCfg["api_key"]; got != "secret" {
		t.Fatalf("api_key = %v, want preserved secret", got)
	}
}

func TestSanitizeConfigJSONAllowsExplicitProviderAPIKeyClear(t *testing.T) {
	newCfg := map[string]any{
		"provider": map[string]any{
			"openai": map[string]any{
				"family":                  "openai",
				clearProviderAPIKeyMarker: true,
			},
		},
	}
	currentCfg := map[string]any{
		"provider": map[string]any{
			"openai": map[string]any{
				"api_key": "secret",
			},
		},
	}

	SanitizeConfigJSON(newCfg, currentCfg)
	NormalizeProviderConfigJSON(newCfg)

	providerCfg := newCfg["provider"].(map[string]any)["openai"].(map[string]any)
	if _, exists := providerCfg["api_key"]; exists {
		t.Fatalf("api_key was preserved despite explicit clear: %#v", providerCfg)
	}
	if _, exists := providerCfg[clearProviderAPIKeyMarker]; exists {
		t.Fatalf("clear marker leaked into normalized config: %#v", providerCfg)
	}
}
