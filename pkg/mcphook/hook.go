package mcphook

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/wweir/warden/config"
)

// jsonObjectRe extracts the first JSON object from a string that may contain surrounding text.
var jsonObjectRe = regexp.MustCompile(`\{[\s\S]*\}`)

// HookContext is the JSON payload passed to every hook invocation.
// For post hooks, Result and IsError are populated with the tool call outcome.
type HookContext struct {
	MCPName   string          `json:"mcp_name"`
	ToolName  string          `json:"tool_name"`
	CallID    string          `json:"call_id"`
	Arguments json.RawMessage `json:"arguments"`
	Result    string          `json:"result,omitempty"`   // post hook only
	IsError   bool            `json:"is_error,omitempty"` // post hook only
}

// hookResult carries the outcome of a single hook execution.
// err is non-nil only when the hook explicitly signals rejection (allow: false).
// Execution errors (timeout, crash, network) leave err nil (fail-open).
type hookResult struct {
	index      int
	htype      string // "exec" or "ai"
	when       string // "pre" or "post"
	stdout     string // exec only
	stderr     string // exec only
	aiResponse string // ai only: raw model response
	rejected   bool   // true if hook explicitly returned allow:false
	reason     string // rejection reason
}

// hookResponse is the shared fixed JSON format for both exec stdout and AI response.
type hookResponse struct {
	Allow  bool   `json:"allow"`
	Reason string `json:"reason"`
}

// RunPre runs all pre hooks for a tool concurrently.
// Returns an error only when a hook explicitly signals rejection (allow: false).
// Execution errors are logged and treated as pass-through (fail-open).
func RunPre(ctx context.Context, mcpName, toolName, gatewayAddr string, hooks []config.HookConfig, hctx HookContext) error {
	pre := filterHooks(hooks, "pre")
	if len(pre) == 0 {
		return nil
	}

	results := runConcurrent(ctx, pre, hctx, gatewayAddr)
	for _, r := range results {
		logResult(mcpName, toolName, r)
		if r.rejected {
			return rejectionError(r)
		}
	}
	return nil
}

// RunPost runs all post hooks for a tool concurrently.
// Results are logged; rejections have no effect (post hooks are audit-only).
func RunPost(ctx context.Context, mcpName, toolName, gatewayAddr string, hooks []config.HookConfig, hctx HookContext) {
	post := filterHooks(hooks, "post")
	if len(post) == 0 {
		return
	}

	results := runConcurrent(ctx, post, hctx, gatewayAddr)
	for _, r := range results {
		logResult(mcpName, toolName, r)
	}
}

// filterHooks returns hooks matching the given when value.
func filterHooks(hooks []config.HookConfig, when string) []config.HookConfig {
	var out []config.HookConfig
	for _, h := range hooks {
		if h.When == when {
			out = append(out, h)
		}
	}
	return out
}

// runConcurrent executes all hooks concurrently and collects results.
func runConcurrent(ctx context.Context, hooks []config.HookConfig, hctx HookContext, gatewayAddr string) []hookResult {
	results := make([]hookResult, len(hooks))
	var wg sync.WaitGroup
	for i, h := range hooks {
		wg.Add(1)
		go func(idx int, hook config.HookConfig) {
			defer wg.Done()
			results[idx] = runOne(ctx, idx, hook, hctx, gatewayAddr)
		}(i, h)
	}
	wg.Wait()
	return results
}

// runOne dispatches a single hook by type with timeout applied.
func runOne(ctx context.Context, idx int, hook config.HookConfig, hctx HookContext, gatewayAddr string) hookResult {
	timeout := hook.TimeoutDuration
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch hook.Type {
	case "exec":
		return runExec(ctx, idx, hook, hctx)
	case "ai":
		return runAI(ctx, idx, hook, hctx, gatewayAddr)
	default:
		// unknown type: fail-open, log warning
		slog.Warn("Hook: unknown type, skipping", "hook_index", idx, "hook_type", hook.Type)
		return hookResult{index: idx, htype: hook.Type, when: hook.When}
	}
}

// logResult writes a structured log entry for a completed hook execution.
func logResult(mcpName, toolName string, r hookResult) {
	attrs := []any{
		"mcp", mcpName,
		"tool", toolName,
		"hook_index", r.index,
		"hook_type", r.htype,
		"hook_when", r.when,
	}
	if r.stdout != "" {
		attrs = append(attrs, "stdout", r.stdout)
	}
	if r.stderr != "" {
		attrs = append(attrs, "stderr", r.stderr)
	}
	if r.aiResponse != "" {
		attrs = append(attrs, "ai_response", r.aiResponse)
	}
	if r.rejected {
		slog.Warn("Hook rejected tool call", append(attrs, "reason", r.reason)...)
	} else {
		slog.Debug("Hook executed", attrs...)
	}
}
