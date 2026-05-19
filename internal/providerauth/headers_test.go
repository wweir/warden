package providerauth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/wweir/warden/config"
)

func TestSetHeadersUsesAPIKeyCommandForOpenAI(t *testing.T) {
	header := http.Header{}
	prov := &config.ProviderConfig{
		Name:          "cmd-openai",
		Format:      config.ProviderFormatOpenAI,
		APIKeyCommand: providerAuthHelperCommand("token"),
	}

	if err := SetHeaders(context.Background(), header, prov, ""); err != nil {
		t.Fatalf("SetHeaders() error = %v", err)
	}
	if got := header.Get("Authorization"); got != "Bearer cmd-token" {
		t.Fatalf("Authorization = %q, want Bearer cmd-token", got)
	}
}

func TestSetHeadersUsesAPIKeyCommandForAnthropic(t *testing.T) {
	header := http.Header{}
	prov := &config.ProviderConfig{
		Name:          "cmd-anthropic",
		Format:      config.ProviderFormatAnthropic,
		APIKeyCommand: providerAuthHelperCommand("token"),
	}

	if err := SetHeaders(context.Background(), header, prov, ""); err != nil {
		t.Fatalf("SetHeaders() error = %v", err)
	}
	if got := header.Get("x-api-key"); got != "cmd-token" {
		t.Fatalf("x-api-key = %q, want cmd-token", got)
	}
	if got := header.Get("Authorization"); got != "Bearer cmd-token" {
		t.Fatalf("Authorization = %q, want Bearer cmd-token", got)
	}
	if got := header.Get("anthropic-version"); got == "" {
		t.Fatal("anthropic-version was not set")
	}
}

func TestSetHeadersReturnsAPIKeyCommandError(t *testing.T) {
	header := http.Header{}
	prov := &config.ProviderConfig{
		Name:          "cmd-fail",
		Format:      config.ProviderFormatOpenAI,
		APIKeyCommand: providerAuthHelperCommand("fail"),
	}

	err := SetHeaders(context.Background(), header, prov, "")
	if err == nil || !strings.Contains(err.Error(), "resolve provider api key") {
		t.Fatalf("expected provider api key error, got %v", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaked command output: %v", err)
	}
}

func TestSetHeadersUsesEndpointHeadersForMatchingURL(t *testing.T) {
	header := http.Header{}
	prov := &config.ProviderConfig{
		Name:  "multi",
		URL:   "https://gateway.example.com",
		Format: config.ProviderFormatOpenAI,
		APIKey: config.SecretString("shared-token"),
		Headers: map[string]string{
			"X-Common": "provider",
		},
		Endpoints: map[string]*config.ProviderEndpointConfig{
			"openai": {
				URL:    "https://gateway.example.com/openai/v1",
				Format: config.ProviderFormatOpenAI,
				Headers: map[string]string{
					"X-Mode": "openai",
				},
			},
			"anthropic": {
				URL:    "https://gateway.example.com/anthropic/v1",
				Format: config.ProviderFormatAnthropic,
				Headers: map[string]string{
					"X-Mode": "anthropic",
				},
			},
		},
	}

	if err := SetHeaders(context.Background(), header, prov, "https://gateway.example.com/anthropic/v1/messages?x=1"); err != nil {
		t.Fatalf("SetHeaders() error = %v", err)
	}
	if got := header.Get("x-api-key"); got != "shared-token" {
		t.Fatalf("x-api-key = %q, want shared-token", got)
	}
	if got := header.Get("X-Mode"); got != "anthropic" {
		t.Fatalf("X-Mode = %q, want anthropic", got)
	}
	if got := header.Get("X-Common"); got != "provider" {
		t.Fatalf("X-Common = %q, want provider", got)
	}
	if got := header.Get("Authorization"); got != "Bearer shared-token" {
		t.Fatalf("Authorization = %q, want Bearer shared-token", got)
	}
}

func TestProviderAuthHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_PROVIDER_AUTH_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		os.Exit(2)
	}
	switch args[1] {
	case "token":
		fmt.Print("cmd-token")
	case "fail":
		fmt.Print("secret-token")
		os.Exit(7)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func providerAuthHelperCommand(args ...string) string {
	parts := []string{quoteProviderAuthShellArg(os.Args[0]), "-test.run=TestProviderAuthHelperProcess", "--"}
	for _, arg := range args {
		parts = append(parts, quoteProviderAuthShellArg(arg))
	}
	command := strings.Join(parts, " ")
	if runtime.GOOS == "windows" {
		return "set GO_WANT_PROVIDER_AUTH_HELPER_PROCESS=1&& " + command
	}
	return "GO_WANT_PROVIDER_AUTH_HELPER_PROCESS=1 " + command
}

func quoteProviderAuthShellArg(arg string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
	}
	return `'` + strings.ReplaceAll(arg, `'`, `'\''`) + `'`
}
