package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sower-proxy/feconf"
	_ "github.com/sower-proxy/feconf/decoder/yaml"
	_ "github.com/sower-proxy/feconf/reader/file"
	"github.com/wweir/warden/config"
	"gopkg.in/yaml.v3"
)

const promptEnabledFalseConfig = `addr: ":8080"
provider:
  openai:
    url: https://api.openai.com/v1
    protocol: openai
route:
  /codex:
    protocol: chat
    exact_models:
      gpt-5.3-codex:
        prompt_enabled: false
        upstreams:
          - provider: openai
            model: gpt-5.3-codex
`

func TestSafeParseConfigReturnsErrorInsteadOfPanicking(t *testing.T) {
	path := writeTempConfig(t, promptEnabledFalseConfig)

	oldArgs := os.Args
	os.Args = []string{"test"}
	t.Cleanup(func() {
		os.Args = oldArgs
	})

	oldCommandLine := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
	})

	conf := feconf.New[config.ConfigStruct]("c", path)
	cfg, err := safeParseConfig(conf)
	if err == nil || cfg != nil {
		t.Fatal("expected default feconf parser to fail on explicit false prompt_enabled")
	}
	if !strings.Contains(err.Error(), "feconf parse panic") {
		t.Fatalf("error = %v, want feconf panic context", err)
	}
}

func TestBuildConfigParserConfigSupportsExplicitFalsePromptEnabled(t *testing.T) {
	path := writeTempConfig(t, promptEnabledFalseConfig)

	oldArgs := os.Args
	os.Args = []string{"test"}
	t.Cleanup(func() {
		os.Args = oldArgs
	})

	oldCommandLine := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
	})

	conf := feconf.New[config.ConfigStruct]("c", path)
	conf.ParserConf = buildConfigParserConfig()

	cfg, err := safeParseConfig(conf)
	if err != nil {
		t.Fatalf("safeParseConfig() error = %v", err)
	}
	if cfg.Route["/codex"] == nil {
		t.Fatal("missing /codex route")
	}
	model := cfg.Route["/codex"].ExactModels["gpt-5.3-codex"]
	if model == nil {
		t.Fatal("missing exact model")
	}
	if model.PromptEnabled == nil {
		t.Fatal("PromptEnabled = nil, want explicit false")
	}
	if *model.PromptEnabled {
		t.Fatal("PromptEnabled = true, want false")
	}
}

func TestConfigRoundTripPreservesExactModelsAfterJSONToYAML(t *testing.T) {
	const configJSON = `{
		"addr": ":8080",
		"provider": {
			"openai": {
				"url": "https://api.openai.com/v1",
				"protocol": "openai"
			}
		},
		"route": {
			"/codex": {
				"protocol": "chat",
				"exact_models": {
					"gpt-5.3-codex": {
						"prompt_enabled": false,
						"upstreams": [
							{
								"provider": "openai",
								"model": "gpt-5.3-codex"
							}
						]
					}
				}
			}
		}
	}`

	var cfgMap any
	if err := json.Unmarshal([]byte(configJSON), &cfgMap); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	yamlBytes, err := yaml.Marshal(cfgMap)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	path := writeTempConfig(t, string(yamlBytes))

	oldArgs := os.Args
	os.Args = []string{"test"}
	t.Cleanup(func() {
		os.Args = oldArgs
	})

	oldCommandLine := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
	})

	conf := feconf.New[config.ConfigStruct]("c", path)
	conf.ParserConf = buildConfigParserConfig()

	cfg, err := safeParseConfig(conf)
	if err != nil {
		t.Fatalf("safeParseConfig() error = %v\nyaml:\n%s", err, string(yamlBytes))
	}

	route := cfg.Route["/codex"]
	if route == nil {
		t.Fatalf("missing /codex route after round trip\nyaml:\n%s", string(yamlBytes))
	}

	model := route.ExactModels["gpt-5.3-codex"]
	if model == nil {
		t.Fatalf("missing exact model after round trip\nyaml:\n%s", string(yamlBytes))
	}

	if len(model.Upstreams) != 1 {
		t.Fatalf("upstreams len = %d, want 1", len(model.Upstreams))
	}
	if model.Upstreams[0].Provider != "openai" || model.Upstreams[0].Model != "gpt-5.3-codex" {
		t.Fatalf("unexpected upstream = %+v", model.Upstreams[0])
	}
	if model.PromptEnabled == nil || *model.PromptEnabled {
		t.Fatalf("PromptEnabled = %v, want explicit false", model.PromptEnabled)
	}
}

func TestBuildConfigParserConfigDecodesBase64Secrets(t *testing.T) {
	const secretConfig = `addr: ":8080"
admin_password: YWRtaW4=
api_keys:
  cli: Y2xpLXNlY3JldA==
provider:
  openai:
    url: https://api.openai.com/v1
    family: openai
    api_key: cHJvdmlkZXItc2VjcmV0
route:
  /v1:
    protocol: chat
    wildcard_models:
      "*":
        providers:
          - openai
`

	path := writeTempConfig(t, secretConfig)

	oldArgs := os.Args
	os.Args = []string{"test"}
	t.Cleanup(func() {
		os.Args = oldArgs
	})

	oldCommandLine := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
	})

	conf := feconf.New[config.ConfigStruct]("c", path)
	conf.ParserConf = buildConfigParserConfig()

	cfg, err := safeParseConfig(conf)
	if err != nil {
		t.Fatalf("safeParseConfig() error = %v", err)
	}

	if got := cfg.AdminPassword.Value(); got != "admin" {
		t.Fatalf("AdminPassword.Value() = %q, want %q", got, "admin")
	}
	if got := cfg.APIKeys["cli"].Value(); got != "cli-secret" {
		t.Fatalf("APIKeys[cli].Value() = %q, want %q", got, "cli-secret")
	}
	if got := cfg.Provider["openai"].APIKey.Value(); got != "provider-secret" {
		t.Fatalf("Provider[openai].APIKey.Value() = %q, want %q", got, "provider-secret")
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "warden-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return file.Name()
}

func TestWritePidFileAllowsExecRestartSamePID(t *testing.T) {
	originalPidFile := pidFile
	tempPidFile := writeTempConfig(t, fmt.Sprintf("%d\n", os.Getpid()))
	pidFile = tempPidFile
	t.Cleanup(func() {
		pidFile = originalPidFile
	})

	if err := writePidFile(); err != nil {
		t.Fatalf("writePidFile() error = %v", err)
	}

	data, err := os.ReadFile(tempPidFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != fmt.Sprintf("%d", os.Getpid()) {
		t.Fatalf("pid file = %q, want %d", got, os.Getpid())
	}
}
