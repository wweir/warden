package config

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResolveAPIKeyCommand(t *testing.T) {
	prov := &ProviderConfig{
		Name:          "cmd",
		Protocol:      ProviderProtocolOpenAI,
		APIKeyCommand: helperCommand("token"),
	}

	token, err := prov.ResolveAPIKey(context.Background())
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v", err)
	}
	if token != "cmd-token" {
		t.Fatalf("ResolveAPIKey() = %q, want cmd-token", token)
	}
}

func TestResolveAPIKeyCommandRejectsInvalidOutput(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		wantErr string
	}{
		{name: "empty", mode: "empty", wantErr: "empty token"},
		{name: "multiple lines", mode: "multi", wantErr: "multiple lines"},
		{name: "too large", mode: "large", wantErr: "exceeds limit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := &ProviderConfig{APIKeyCommand: helperCommand(tt.mode)}
			_, err := prov.ResolveAPIKey(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected %q error, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestResolveAPIKeyCommandFailureDoesNotExposeOutput(t *testing.T) {
	prov := &ProviderConfig{APIKeyCommand: helperCommand("fail")}

	_, err := prov.ResolveAPIKey(context.Background())
	if err == nil {
		t.Fatal("expected command failure")
	}
	if !strings.Contains(err.Error(), "exit code 7") {
		t.Fatalf("command error missing exit code: %v", err)
	}
	if strings.Contains(err.Error(), "secret-out") {
		t.Fatalf("command error leaked stdout: %v", err)
	}
}

func TestResolveAPIKeyCommandFailureIncludesExitCode(t *testing.T) {
	prov := &ProviderConfig{APIKeyCommand: helperCommand("fail")}

	_, err := prov.ResolveAPIKey(context.Background())
	if err == nil {
		t.Fatal("expected command failure")
	}
	if strings.Contains(err.Error(), "secret-out") {
		t.Fatalf("command error leaked stdout: %v", err)
	}
	if !strings.Contains(err.Error(), "secret-err") {
		t.Fatalf("command error missing stderr: %v", err)
	}
}

func TestResolveAPIKeyCommandTimeout(t *testing.T) {
	prov := &ProviderConfig{
		APIKeyCommand:        helperCommand("sleep"),
		APIKeyCommandTimeout: "20ms",
	}

	_, err := prov.ResolveAPIKey(context.Background())
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestResolveAPIKeyCommandCachesByTTL(t *testing.T) {
	countFile := t.TempDir() + "/count"
	prov := &ProviderConfig{
		APIKeyCommand:    helperCommand("counter", countFile),
		APIKeyCommandTTL: "1m",
	}

	first, err := prov.ResolveAPIKey(context.Background())
	if err != nil {
		t.Fatalf("first ResolveAPIKey() error = %v", err)
	}
	second, err := prov.ResolveAPIKey(context.Background())
	if err != nil {
		t.Fatalf("second ResolveAPIKey() error = %v", err)
	}
	if first != "token-1" || second != "token-1" {
		t.Fatalf("cached tokens = %q, %q; want token-1, token-1", first, second)
	}
	if got := strings.TrimSpace(readTestFile(t, countFile)); got != "1" {
		t.Fatalf("command count = %q, want 1", got)
	}
}

func TestResolveAPIKeyCommandTTLZeroDisablesCache(t *testing.T) {
	countFile := t.TempDir() + "/count"
	prov := &ProviderConfig{
		APIKeyCommand:    helperCommand("counter", countFile),
		APIKeyCommandTTL: "0s",
	}

	first, err := prov.ResolveAPIKey(context.Background())
	if err != nil {
		t.Fatalf("first ResolveAPIKey() error = %v", err)
	}
	second, err := prov.ResolveAPIKey(context.Background())
	if err != nil {
		t.Fatalf("second ResolveAPIKey() error = %v", err)
	}
	if first != "token-1" || second != "token-2" {
		t.Fatalf("tokens = %q, %q; want token-1, token-2", first, second)
	}
}

func TestInvalidateAuthClearsAPIKeyCommandCache(t *testing.T) {
	countFile := t.TempDir() + "/count"
	prov := &ProviderConfig{
		Protocol:         ProviderProtocolOpenAI,
		APIKeyCommand:    helperCommand("counter", countFile),
		APIKeyCommandTTL: "1m",
	}

	first, err := prov.ResolveAPIKey(context.Background())
	if err != nil {
		t.Fatalf("first ResolveAPIKey() error = %v", err)
	}
	prov.InvalidateAuth()
	second, err := prov.ResolveAPIKey(context.Background())
	if err != nil {
		t.Fatalf("second ResolveAPIKey() error = %v", err)
	}
	if first != "token-1" || second != "token-2" {
		t.Fatalf("tokens = %q, %q; want token-1, token-2", first, second)
	}
}

func TestAPIKeyCommandHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
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
	case "empty":
	case "multi":
		fmt.Print("line1\nline2\n")
	case "large":
		fmt.Print(strings.Repeat("x", apiKeyCommandOutputLimit+1))
	case "fail":
		fmt.Fprint(os.Stdout, "secret-out")
		fmt.Fprint(os.Stderr, "secret-err")
		os.Exit(7)
	case "sleep":
		time.Sleep(2 * time.Second)
		fmt.Print("late-token")
	case "counter":
		if len(args) < 3 {
			os.Exit(2)
		}
		path := args[2]
		count := 0
		if data, err := os.ReadFile(path); err == nil {
			_, _ = fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &count)
		}
		count++
		if err := os.WriteFile(path, []byte(fmt.Sprintf("%d", count)), 0o600); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(3)
		}
		fmt.Printf("token-%d", count)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func helperCommand(args ...string) string {
	parts := []string{quoteTestShellArg(os.Args[0]), "-test.run=TestAPIKeyCommandHelperProcess", "--"}
	for _, arg := range args {
		parts = append(parts, quoteTestShellArg(arg))
	}
	command := strings.Join(parts, " ")
	if runtime.GOOS == "windows" {
		return "set GO_WANT_HELPER_PROCESS=1&& " + command
	}
	return "GO_WANT_HELPER_PROCESS=1 " + command
}

func quoteTestShellArg(arg string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
	}
	return `'` + strings.ReplaceAll(arg, `'`, `'\''`) + `'`
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
