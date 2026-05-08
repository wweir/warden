package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/sower-proxy/feconf"
	_ "github.com/sower-proxy/feconf/decoder/toml"
	_ "github.com/sower-proxy/feconf/reader/file"
	"github.com/wweir/warden/config"
)

const promptEnabledFalseConfig = `addr = ":8080"

[provider.openai]
url = "https://api.openai.com/v1"
protocol = "openai"

[route."/codex"]
protocol = "chat"

[route."/codex".exact_models."gpt-5.3-codex"]
prompt_enabled = false

[[route."/codex".exact_models."gpt-5.3-codex".upstreams]]
provider = "openai"
model = "gpt-5.3-codex"
`

func TestExampleConfigTOMLParses(t *testing.T) {
	path := writeTempConfigWithExt(t, config.ExampleConfig, "*.toml")

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
	if cfg.Addr != ":9832" {
		t.Fatalf("Addr = %q, want :9832", cfg.Addr)
	}
	if cfg.Provider["openai"] == nil || cfg.Route["/openai"] == nil {
		t.Fatalf("example config missing openai provider or /openai route")
	}
}

func TestConfigCandidatesPreferManagedTOML(t *testing.T) {
	want := []string{"warden.toml", "config/warden.toml", "/etc/warden/warden.toml"}
	if len(configCandidates) != len(want) {
		t.Fatalf("configCandidates len = %d, want %d: %v", len(configCandidates), len(want), configCandidates)
	}
	for i, wantCandidate := range want {
		if configCandidates[i] != wantCandidate {
			t.Fatalf("configCandidates[%d] = %q, want %q", i, configCandidates[i], wantCandidate)
		}
	}
}

func TestSafeParseConfigReturnsErrorInsteadOfPanicking(t *testing.T) {
	path := writeTempConfig(t, "addr = \"unterminated\n")

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
		t.Fatal("expected invalid TOML to return an error")
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

func TestConfigRoundTripPreservesExactModelsAfterJSONToTOML(t *testing.T) {
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

	tomlBytes, err := toml.Marshal(cfgMap)
	if err != nil {
		t.Fatalf("toml.Marshal() error = %v", err)
	}

	path := writeTempConfig(t, string(tomlBytes))

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
		t.Fatalf("safeParseConfig() error = %v\ntoml:\n%s", err, string(tomlBytes))
	}

	route := cfg.Route["/codex"]
	if route == nil {
		t.Fatalf("missing /codex route after round trip\ntoml:\n%s", string(tomlBytes))
	}

	model := route.ExactModels["gpt-5.3-codex"]
	if model == nil {
		t.Fatalf("missing exact model after round trip\ntoml:\n%s", string(tomlBytes))
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
	const secretConfig = `addr = ":8080"
admin_password = "YWRtaW4="

[provider.openai]
url = "https://api.openai.com/v1"
family = "openai"
api_key = "cHJvdmlkZXItc2VjcmV0"

[route."/v1"]
protocol = "chat"

[route."/v1".api_keys]
cli = "Y2xpLXNlY3JldA=="

[route."/v1".wildcard_models."*"]
providers = ["openai"]
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
	if got := cfg.Route["/v1"].APIKeys["cli"].Value(); got != "cli-secret" {
		t.Fatalf("Route[/v1].APIKeys[cli].Value() = %q, want %q", got, "cli-secret")
	}
	if got := cfg.Provider["openai"].APIKey.Value(); got != "provider-secret" {
		t.Fatalf("Provider[openai].APIKey.Value() = %q, want %q", got, "provider-secret")
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	return writeTempConfigWithExt(t, content, "warden-*.toml")
}

func writeTempConfigWithExt(t *testing.T, content, pattern string) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), pattern)
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

func TestParseModeFlags(t *testing.T) {
	tests := []struct {
		name              string
		args              []string
		install           bool
		reload            bool
		nonInteractive    bool
		assumeYes         bool
		startAfterInstall *bool
		exposeExternally  *bool
	}{
		{name: "none", args: nil},
		{name: "install", args: []string{"-i"}, install: true},
		{name: "reload", args: []string{"-r"}, reload: true},
		{name: "explicit false", args: []string{"-i=true", "-r=false"}, install: true, reload: false},
		{name: "config arg ignored", args: []string{"-c", "warden.toml", "-r"}, reload: true},
		{name: "non interactive start", args: []string{"-i", "--non-interactive", "--start"}, install: true, nonInteractive: true, startAfterInstall: boolPtr(true)},
		{name: "assume yes", args: []string{"-i", "-y"}, install: true, nonInteractive: true, assumeYes: true},
		{name: "expose", args: []string{"-i", "--expose"}, install: true, exposeExternally: boolPtr(true)},
		{name: "local only wins", args: []string{"-i", "--expose", "--local-only"}, install: true, exposeExternally: boolPtr(false)},
		{name: "no start wins", args: []string{"-i", "--start", "--no-start"}, install: true, startAfterInstall: boolPtr(false)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseModeFlags(tt.args)
			if got.install != tt.install || got.reload != tt.reload || got.nonInteractive != tt.nonInteractive || got.assumeYes != tt.assumeYes {
				t.Fatalf("parseModeFlags(%v) = %+v, want install=%v reload=%v nonInteractive=%v assumeYes=%v", tt.args, got, tt.install, tt.reload, tt.nonInteractive, tt.assumeYes)
			}
			if !equalBoolPtr(got.startAfterInstall, tt.startAfterInstall) {
				t.Fatalf("parseModeFlags(%v) startAfterInstall = %v, want %v", tt.args, got.startAfterInstall, tt.startAfterInstall)
			}
			if !equalBoolPtr(got.exposeExternally, tt.exposeExternally) {
				t.Fatalf("parseModeFlags(%v) exposeExternally = %v, want %v", tt.args, got.exposeExternally, tt.exposeExternally)
			}
		})
	}
}

func TestPrintUsageIncludesCUDAExample(t *testing.T) {
	oldOutput := flag.CommandLine.Output()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	flag.CommandLine.SetOutput(w)
	printUsage()
	w.Close()
	flag.CommandLine.SetOutput(oldOutput)

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "CUDA-backed local OpenAI-compatible provider") {
		t.Fatalf("usage output missing CUDA example: %s", out)
	}
	if !strings.Contains(out, `[provider.ollama]`) {
		t.Fatalf("usage output missing provider example: %s", out)
	}
}

func TestBuildInstallOptions(t *testing.T) {
	start := true
	opts := buildInstallOptions(modeFlags{
		nonInteractive:    true,
		startAfterInstall: &start,
	})
	if opts.Confirm != nil {
		t.Fatal("Confirm should be nil for non-interactive install")
	}
	if opts.StartAfterInstall == nil || !*opts.StartAfterInstall {
		t.Fatalf("StartAfterInstall = %v, want true", opts.StartAfterInstall)
	}
	if opts.ExposeExternally != nil {
		t.Fatalf("ExposeExternally = %v, want nil", opts.ExposeExternally)
	}

	expose := true
	opts = buildInstallOptions(modeFlags{exposeExternally: &expose})
	if opts.Confirm == nil {
		t.Fatal("Confirm should be set for interactive install")
	}
	if opts.ExposeExternally == nil || !*opts.ExposeExternally {
		t.Fatalf("ExposeExternally = %v, want true", opts.ExposeExternally)
	}

	opts = buildInstallOptions(modeFlags{assumeYes: true})
	if opts.Confirm != nil {
		t.Fatal("Confirm should be nil for assume-yes install")
	}
	if opts.StartAfterInstall == nil || !*opts.StartAfterInstall {
		t.Fatalf("StartAfterInstall = %v, want true for assume-yes install", opts.StartAfterInstall)
	}
	if opts.ExposeExternally != nil {
		t.Fatalf("ExposeExternally = %v, want nil for assume-yes install", opts.ExposeExternally)
	}

	start = false
	opts = buildInstallOptions(modeFlags{
		assumeYes:         true,
		startAfterInstall: &start,
	})
	if opts.StartAfterInstall == nil || *opts.StartAfterInstall {
		t.Fatalf("StartAfterInstall = %v, want explicit false to override assume-yes", opts.StartAfterInstall)
	}
}

func TestStdinAdminPasswordReadsPipedConfirmation(t *testing.T) {
	oldStdin := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = reader.Close()
	})
	os.Stdin = reader

	if _, err := writer.WriteString("strong-passphrase\nstrong-passphrase\n"); err != nil {
		t.Fatalf("write password pipe: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close password pipe: %v", err)
	}

	password, ok := stdinAdminPassword("Admin password")
	if !ok {
		t.Fatal("stdinAdminPassword() ok = false, want true")
	}
	if password != "strong-passphrase" {
		t.Fatalf("stdinAdminPassword() password = %q", password)
	}
}

func equalBoolPtr(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func TestValidateConfigPath(t *testing.T) {
	validPath := writeTempConfig(t, `addr = ":8080"

[provider.openai]
url = "https://api.openai.com/v1"
family = "openai"

[route."/v1"]
protocol = "chat"

[route."/v1".wildcard_models."*"]
providers = ["openai"]
`)
	invalidPath := writeTempConfig(t, `addr = ":8080"

[provider.openai]
url = "://bad"
family = "openai"

[route."/v1"]
protocol = "chat"

[route."/v1".wildcard_models."*"]
providers = ["openai"]
`)

	tests := []struct {
		name      string
		path      string
		wantError bool
	}{
		{name: "empty", path: ""},
		{name: "missing", path: validPath + ".missing"},
		{name: "valid", path: validPath},
		{name: "invalid", path: invalidPath, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			err := validateConfigPath(tt.path)
			if (err != nil) != tt.wantError {
				t.Fatalf("validateConfigPath(%q) error = %v, wantError %v", tt.path, err, tt.wantError)
			}
		})
	}
}

func TestValidateConfigPathIgnoresCLIConfigOverride(t *testing.T) {
	validPath := writeTempConfig(t, `addr = ":8080"

[provider.openai]
url = "https://api.openai.com/v1"
family = "openai"

[route."/v1"]
protocol = "chat"

[route."/v1".wildcard_models."*"]
providers = ["openai"]
`)
	invalidPath := writeTempConfig(t, `addr = ":8080"

[provider.openai]
url = "://bad"
family = "openai"

[route."/v1"]
protocol = "chat"

[route."/v1".wildcard_models."*"]
providers = ["openai"]
`)

	oldArgs := os.Args
	os.Args = []string{"test", "-c", validPath}
	t.Cleanup(func() {
		os.Args = oldArgs
	})

	oldCommandLine := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
	})

	if err := validateConfigPath(invalidPath); err == nil {
		t.Fatal("validateConfigPath() error = nil, want invalid target path to win over CLI -c")
	}
}
