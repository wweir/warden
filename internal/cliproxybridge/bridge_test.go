package cliproxybridge

import (
	"os"
	"strings"
	"testing"

	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestApplyFeatureHidingDefaults(t *testing.T) {
	t.Parallel()

	cfg := &sdkconfig.Config{}
	cfg.SDKConfig.PassthroughHeaders = true
	applyFeatureHidingDefaults(cfg)

	if got := cfg.CodexHeaderDefaults.UserAgent; got != defaultCodexUserAgent {
		t.Fatalf("codex user-agent = %q, want %q", got, defaultCodexUserAgent)
	}
	if got := cfg.CodexHeaderDefaults.BetaFeatures; got != defaultCodexBetaFeatures {
		t.Fatalf("codex beta-features = %q, want %q", got, defaultCodexBetaFeatures)
	}
	if cfg.SDKConfig.PassthroughHeaders {
		t.Fatal("passthrough-headers = true, want false")
	}
	if got := cfg.ClaudeHeaderDefaults.UserAgent; got != defaultClaudeUserAgent {
		t.Fatalf("claude user-agent = %q, want %q", got, defaultClaudeUserAgent)
	}
	if got := cfg.ClaudeHeaderDefaults.PackageVersion; got != defaultClaudePackageVersion {
		t.Fatalf("claude package-version = %q, want %q", got, defaultClaudePackageVersion)
	}
	if got := cfg.ClaudeHeaderDefaults.RuntimeVersion; got != defaultClaudeRuntimeVersion {
		t.Fatalf("claude runtime-version = %q, want %q", got, defaultClaudeRuntimeVersion)
	}
	if got := cfg.ClaudeHeaderDefaults.OS; got != defaultClaudeOS {
		t.Fatalf("claude os = %q, want %q", got, defaultClaudeOS)
	}
	if got := cfg.ClaudeHeaderDefaults.Arch; got != defaultClaudeArch {
		t.Fatalf("claude arch = %q, want %q", got, defaultClaudeArch)
	}
	if got := cfg.ClaudeHeaderDefaults.Timeout; got != defaultClaudeTimeout {
		t.Fatalf("claude timeout = %q, want %q", got, defaultClaudeTimeout)
	}
	if cfg.ClaudeHeaderDefaults.StabilizeDeviceProfile == nil {
		t.Fatal("claude stabilize-device-profile = nil, want non-nil")
	}
	if got := *cfg.ClaudeHeaderDefaults.StabilizeDeviceProfile; got != defaultClaudeStabilizeDeviceState {
		t.Fatalf("claude stabilize-device-profile = %v, want %v", got, defaultClaudeStabilizeDeviceState)
	}
}

func TestWriteRuntimeConfigIncludesFeatureHidingDefaults(t *testing.T) {
	t.Parallel()

	cfg := &sdkconfig.Config{}
	applyFeatureHidingDefaults(cfg)

	path, err := writeRuntimeConfig(cfg)
	if err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"codex-header-defaults:",
		"passthrough-headers: false",
		"user-agent: " + defaultCodexUserAgent,
		"beta-features: " + defaultCodexBetaFeatures,
		"claude-header-defaults:",
		"user-agent: " + defaultClaudeUserAgent,
		"package-version: " + defaultClaudePackageVersion,
		"runtime-version: " + defaultClaudeRuntimeVersion,
		"os: " + defaultClaudeOS,
		"arch: " + defaultClaudeArch,
		"timeout: \"" + defaultClaudeTimeout + "\"",
		"stabilize-device-profile: true",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("runtime config missing %q in:\n%s", want, content)
		}
	}
}
