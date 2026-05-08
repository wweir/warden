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
)

func TestMarshalConfigFileUsesTOML(t *testing.T) {
	cfg := &config.ConfigStruct{
		Addr: ":9832",
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				Family: "openai",
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

	data, err := marshalConfigFile(cfg)
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
