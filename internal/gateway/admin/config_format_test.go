package admin

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/wweir/warden/config"
	"gopkg.in/yaml.v3"
)

func TestMarshalConfigFileUsesTOML(t *testing.T) {
	cfg := &config.ConfigStruct{
		Addr: ":9832",
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				Format: "openai",
				URL:    "https://api.openai.com/v1",
			},
		},
		CLIProxy: &config.CLIProxyConfig{
			Enabled:             true,
			AuthDir:             "/tmp/cliproxy-auth",
			RequestRetry:        2,
			MaxRetryCredentials: 3,
		},
		Webhook: map[string]*config.WebhookConfig{
			"audit": {
				URL:   "https://logs.example.test/ingest",
				Retry: 2,
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: "chat",
				WildcardModels: map[string]*config.WildcardRouteModelConfig{
					"gpt-*": {Providers: []string{"openai"}},
				},
			},
		},
	}

	data, err := marshalConfigFile(cfg, configFileFormatTOML)
	if err != nil {
		t.Fatalf("marshalConfigFile() error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		`addr = ":9832"`,
		`request_retry = 2`,
		`max_retry_credentials = 3`,
		`retry = 2`,
		`[provider.openai]`,
		`[route."/openai"]`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("TOML output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "addr:") {
		t.Fatalf("TOML output looks like YAML:\n%s", text)
	}
	if strings.Contains(text, "2.0") || strings.Contains(text, "3.0") {
		t.Fatalf("TOML output wrote integer fields as floats:\n%s", text)
	}

	var decoded config.ConfigStruct
	if _, err := toml.Decode(text, &decoded); err != nil {
		t.Fatalf("TOML output does not decode back into config struct: %v\n%s", err, text)
	}
}

func TestMarshalConfigFileUsesYAML(t *testing.T) {
	cfg := &config.ConfigStruct{
		Addr: ":9832",
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				Format: "openai",
				URL:    "https://api.openai.com/v1",
				APIKey: "provider-secret",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: "chat",
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": {
						Upstreams: []*config.RouteUpstreamConfig{
							{Provider: "openai", Model: "gpt-4o"},
						},
					},
				},
			},
		},
	}

	data, err := marshalConfigFile(cfg, configFileFormatYAML)
	if err != nil {
		t.Fatalf("marshalConfigFile() error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		`addr: :9832`,
		`provider:`,
		`openai:`,
		`api_key: ` + config.EncodeSecret("provider-secret"),
		`route:`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("YAML output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `[provider.openai]`) {
		t.Fatalf("YAML output looks like TOML:\n%s", text)
	}

	var decoded map[string]any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("YAML output does not decode back into map: %v\n%s", err, text)
	}
	provider := decoded["provider"].(map[string]any)["openai"].(map[string]any)
	if got := provider["api_key"]; got != config.EncodeSecret("provider-secret") {
		t.Fatalf("decoded provider api key = %q, want encoded provider secret", got)
	}
}

func TestHandlerMarshalConfigFileUsesYAMLPathExtension(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "warden.yaml")
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			Addr: ":9832",
		},
		ConfigPath: &configPath,
	})

	data, err := handler.marshalRuntimeConfigFile()
	if err != nil {
		t.Fatalf("marshalRuntimeConfigFile() error = %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `addr: :9832`) {
		t.Fatalf("runtime config did not use YAML format:\n%s", text)
	}
	if strings.Contains(text, `addr =`) {
		t.Fatalf("runtime config used TOML format for yaml path:\n%s", text)
	}
}

func TestWriteConfigFileForcesPrivateMode(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "warden.toml")
	original := []byte(`addr = ":9832"` + "\n")
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatalf("write original config: %v", err)
	}
	configHash := fmt.Sprintf("%x", sha256.Sum256(original))

	handler := NewHandler(Deps{
		Cfg:        &config.ConfigStruct{},
		ConfigPath: &configPath,
		ConfigHash: &configHash,
	})
	if err := handler.writeConfigFile([]byte(`addr = ":9833"` + "\n")); err != nil {
		t.Fatalf("writeConfigFile() error = %v", err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != configFileMode {
		t.Fatalf("config mode = %o, want %o", got, configFileMode)
	}
}

func TestWriteConfigFileCreatesPrivateConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "warden.toml")
	configHash := ""
	handler := NewHandler(Deps{
		Cfg:        &config.ConfigStruct{},
		ConfigPath: &configPath,
		ConfigHash: &configHash,
	})

	if err := handler.writeConfigFile([]byte(`addr = ":9833"` + "\n")); err != nil {
		t.Fatalf("writeConfigFile() error = %v", err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != configFileMode {
		t.Fatalf("config mode = %o, want %o", got, configFileMode)
	}
}
