package mcphook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/wweir/warden/config"
)

// rejectionError returns a formatted error for an explicit hook rejection.
func rejectionError(r hookResult) error {
	return fmt.Errorf("hook[%d] (%s) rejected: %s", r.index, r.htype, r.reason)
}

// runExec executes an external command hook.
// The HookContext is passed as JSON via stdin.
// The command's stdout must contain a JSON object {"allow": bool, "reason": "..."}.
// Any execution error (non-zero exit, timeout) is logged and treated as pass-through (fail-open).
// Only an explicit allow:false in the stdout JSON triggers rejection.
func runExec(ctx context.Context, idx int, hook config.HookConfig, hctx HookContext) hookResult {
	r := hookResult{index: idx, htype: "exec", when: hook.When}

	payload, err := json.Marshal(hctx)
	if err != nil {
		slog.Warn("Hook exec: failed to marshal context, skipping", "hook_index", idx, "error", err)
		return r
	}

	cmd := exec.CommandContext(ctx, hook.Command, hook.Args...)
	cmd.Stdin = bytes.NewReader(payload)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// execution error: fail-open
		slog.Warn("Hook exec: execution error, passing through", "hook_index", idx, "error", err,
			"stderr", stderr.String())
		r.stdout = stdout.String()
		r.stderr = stderr.String()
		return r
	}
	r.stdout = stdout.String()
	r.stderr = stderr.String()

	parseHookResponse(r.stdout, &r)
	return r
}

// parseHookResponse tries to decode a hookResponse JSON from text.
// On parse failure, the hook is treated as pass-through (fail-open).
// Only allow:false sets r.rejected.
func parseHookResponse(text string, r *hookResult) {
	if text == "" {
		return
	}

	var resp hookResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		// try to extract first JSON object (model may add surrounding text)
		match := jsonObjectRe.FindString(text)
		if match == "" {
			slog.Debug("Hook: no JSON in output, passing through", "hook_index", r.index, "hook_type", r.htype)
			return
		}
		if err := json.Unmarshal([]byte(match), &resp); err != nil {
			slog.Debug("Hook: failed to parse JSON, passing through", "hook_index", r.index, "hook_type", r.htype, "error", err)
			return
		}
	}

	if !resp.Allow {
		r.rejected = true
		r.reason = resp.Reason
	}
}
