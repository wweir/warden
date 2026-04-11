package toolhook

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/wweir/warden/config"
)

func TestMatchHooks(t *testing.T) {
	rules := []*config.HookRuleConfig{
		{Match: "filesystem__*", Hook: config.HookConfig{Type: "exec", When: "block", Command: "echo"}},
		{Match: "web_search", Hook: config.HookConfig{Type: "exec", When: "async", Command: "echo"}},
		{Match: "[", Hook: config.HookConfig{Type: "exec", When: "block", Command: "echo"}},
	}

	hooks := MatchHooks("filesystem__write_file", rules)
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].When != "block" {
		t.Fatalf("expected block hook, got %s", hooks[0].When)
	}

	hooks = MatchHooks("web_search", rules)
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].When != "async" {
		t.Fatalf("expected async hook, got %s", hooks[0].When)
	}
}

func TestLogResultRedactsRawHookOutputs(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)

	logResult("filesystem__write_file", hookResult{
		index:        1,
		htype:        "exec",
		when:         "block",
		stdout:       "secret-stdout-token",
		stderr:       "secret-stderr-token",
		aiResponse:   "secret-ai-token",
		httpResponse: "secret-http-token",
	})

	logged := buf.String()
	for _, secret := range []string{"secret-stdout-token", "secret-stderr-token", "secret-ai-token", "secret-http-token"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("log leaked raw hook output %q: %s", secret, logged)
		}
	}
	for _, field := range []string{"stdout_bytes", "stderr_bytes", "ai_response_bytes", "http_response_bytes"} {
		if !strings.Contains(logged, field) {
			t.Fatalf("log missing summary field %q: %s", field, logged)
		}
	}
}

func TestRunBlockReturnsFirstRejectedVerdict(t *testing.T) {
	v := RunBlock(context.Background(), ":0", nil, CallContext{FullName: "filesystem__write_file"})
	if v.Rejected {
		t.Fatalf("expected rejected=false")
	}
}
