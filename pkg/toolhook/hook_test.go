package toolhook

import (
	"testing"

	"github.com/wweir/warden/config"
)

func TestMatchHooks(t *testing.T) {
	rules := []*config.HookRuleConfig{
		{Match: "filesystem__*", Hook: config.HookConfig{Type: "exec", When: "pre", Command: "echo"}},
		{Match: "web_search", Hook: config.HookConfig{Type: "exec", When: "post", Command: "echo"}},
		{Match: "[", Hook: config.HookConfig{Type: "exec", When: "pre", Command: "echo"}},
	}

	hooks := MatchHooks("filesystem__write_file", rules)
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].When != "pre" {
		t.Fatalf("expected pre hook, got %s", hooks[0].When)
	}

	hooks = MatchHooks("web_search", rules)
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].When != "post" {
		t.Fatalf("expected post hook, got %s", hooks[0].When)
	}
}
