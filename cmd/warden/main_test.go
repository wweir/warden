package main

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/sower-proxy/feconf"
	_ "github.com/sower-proxy/feconf/decoder/yaml"
	_ "github.com/sower-proxy/feconf/reader/file"
	"github.com/wweir/warden/config"
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
	t.Parallel()

	path := writeTempConfig(t, promptEnabledFalseConfig)

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
	t.Parallel()

	path := writeTempConfig(t, promptEnabledFalseConfig)

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
