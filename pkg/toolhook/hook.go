package toolhook

import (
	"context"
	"encoding/json"
	"log/slog"
	"path"
	"regexp"
	"sync"
	"time"

	"github.com/wweir/warden/config"
)

// jsonObjectRe extracts the first JSON object from a string that may contain surrounding text.
var jsonObjectRe = regexp.MustCompile(`\{[\s\S]*\}`)

// CallContext is the JSON payload passed to every hook invocation.
// For async hooks, Result and IsError are populated with the tool call outcome.
type CallContext struct {
	ToolName  string          `json:"tool_name"` // original tool name from model output
	FullName  string          `json:"full_name"` // normalized full name, e.g. mcp__tool
	MCPName   string          `json:"mcp_name"`  // optional: parsed from names like prefix__tool
	CallID    string          `json:"call_id"`
	Arguments json.RawMessage `json:"arguments"`
	Result    string          `json:"result,omitempty"`   // post hook only
	IsError   bool            `json:"is_error,omitempty"` // post hook only
}

// hookResult carries the outcome of a single hook execution.
// err is non-nil only when the hook explicitly signals rejection (allow: false).
// Execution errors (timeout, crash, network) leave err nil (fail-open).
type hookResult struct {
	index        int
	htype        string // "exec", "ai" or "http"
	when         string // "block" or "async"
	stdout       string // exec only
	stderr       string // exec only
	aiResponse   string // ai only: raw model response
	httpResponse string // http only: raw webhook response
	rejected     bool   // true if hook explicitly returned allow:false
	reason       string // rejection reason
}

// hookResponse is the shared fixed JSON format for exec/ai/http hook responses.
type hookResponse struct {
	Allow  *bool  `json:"allow"`
	Reason string `json:"reason"`
}

// HookVerdict carries the aggregated result of hook execution for one tool call.
type HookVerdict struct {
	ToolName string `json:"tool_name"`
	CallID   string `json:"call_id,omitempty"`
	Rejected bool   `json:"rejected"`
	Reason   string `json:"reason,omitempty"`
	Mode     string `json:"mode"` // "block" or "async"
}

// MatchHooks returns all HookConfig entries whose Match pattern matches toolFullName.
func MatchHooks(toolFullName string, rules []*config.HookRuleConfig) []config.HookConfig {
	var hooks []config.HookConfig
	for _, rule := range rules {
		matched, err := path.Match(rule.Match, toolFullName)
		if err != nil {
			slog.Warn("Invalid hook rule match pattern", "pattern", rule.Match, "error", err)
			continue
		}
		if matched {
			hooks = append(hooks, rule.Hook)
		}
	}
	return hooks
}

// RunBlock runs all block-mode hooks for a tool concurrently.
// Returns a HookVerdict indicating whether any hook rejected the call.
// Execution errors are logged and treated as pass-through (fail-open).
func RunBlock(ctx context.Context, gatewayAddr string, hooks []config.HookConfig, hctx CallContext) HookVerdict {
	v := HookVerdict{ToolName: hctx.FullName, CallID: hctx.CallID, Mode: "block"}
	block := filterHooks(hooks, "block")
	if len(block) == 0 {
		return v
	}

	results := runConcurrent(ctx, block, hctx, gatewayAddr)
	for _, r := range results {
		logResult(hctx.FullName, r)
		if r.rejected {
			v.Rejected = true
			v.Reason = r.reason
			return v
		}
	}
	return v
}

// RunAsync runs all async-mode hooks for a tool concurrently.
// Results are logged; rejections are recorded but do not block the response.
func RunAsync(ctx context.Context, gatewayAddr string, hooks []config.HookConfig, hctx CallContext) HookVerdict {
	v := HookVerdict{ToolName: hctx.FullName, CallID: hctx.CallID, Mode: "async"}
	async := filterHooks(hooks, "async")
	if len(async) == 0 {
		return v
	}

	results := runConcurrent(ctx, async, hctx, gatewayAddr)
	for _, r := range results {
		logResult(hctx.FullName, r)
		if r.rejected {
			v.Rejected = true
			v.Reason = r.reason
		}
	}
	return v
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
func runConcurrent(ctx context.Context, hooks []config.HookConfig, hctx CallContext, gatewayAddr string) []hookResult {
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
func runOne(ctx context.Context, idx int, hook config.HookConfig, hctx CallContext, gatewayAddr string) hookResult {
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
	case "http":
		return runHTTP(ctx, idx, hook, hctx)
	default:
		// unknown type: fail-open, log warning
		slog.Warn("Hook: unknown type, skipping", "hook_index", idx, "hook_type", hook.Type)
		return hookResult{index: idx, htype: hook.Type, when: hook.When}
	}
}

// logResult writes a structured log entry for a completed hook execution.
func logResult(toolFullName string, r hookResult) {
	attrs := []any{
		"tool", toolFullName,
		"hook_index", r.index,
		"hook_type", r.htype,
		"hook_when", r.when,
	}
	if r.stdout != "" {
		attrs = append(attrs, "stdout_bytes", len(r.stdout))
	}
	if r.stderr != "" {
		attrs = append(attrs, "stderr_bytes", len(r.stderr))
	}
	if r.aiResponse != "" {
		attrs = append(attrs, "ai_response_bytes", len(r.aiResponse))
	}
	if r.httpResponse != "" {
		attrs = append(attrs, "http_response_bytes", len(r.httpResponse))
	}
	if r.rejected {
		slog.Warn("Hook rejected tool call", append(attrs, "reason", r.reason)...)
	} else {
		slog.Debug("Hook executed", attrs...)
	}
}
