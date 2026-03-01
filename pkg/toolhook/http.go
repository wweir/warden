package toolhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/wweir/warden/config"
)

// runHTTP calls a configured webhook endpoint to evaluate a tool call.
// The request body defaults to CallContext JSON, or webhook.body_template when configured.
// The webhook may return JSON {"allow":bool,"reason":"..."} to explicitly reject.
func runHTTP(ctx context.Context, idx int, hook config.HookConfig, hctx CallContext) hookResult {
	r := hookResult{index: idx, htype: "http", when: hook.When}

	if hook.WebhookCfg == nil {
		slog.Warn("Hook http: webhook config is nil, passing through", "hook_index", idx)
		return r
	}

	body, err := renderWebhookBody(hook.WebhookCfg.BodyTemplate, hctx)
	if err != nil {
		slog.Warn("Hook http: failed to render body, passing through", "hook_index", idx, "error", err)
		return r
	}

	method := strings.ToUpper(hook.WebhookCfg.Method)
	if method == "" {
		method = http.MethodPost
	}

	attempts := hook.WebhookCfg.Retry + 1
	if attempts < 1 {
		attempts = 1
	}

	for attempt := 0; attempt < attempts; attempt++ {
		if ctx.Err() != nil {
			slog.Warn("Hook http: context canceled", "hook_index", idx, "error", ctx.Err())
			return r
		}

		statusCode, respText, reqErr := callWebhook(ctx, method, hook.WebhookCfg.URL, hook.WebhookCfg.Headers, body)
		r.httpResponse = respText

		if reqErr != nil {
			slog.Warn("Hook http: request failed, retrying", "hook_index", idx, "attempt", attempt+1, "attempts", attempts, "error", reqErr)
			if attempt+1 < attempts {
				if !waitRetry(ctx) {
					return r
				}
				continue
			}
			return r
		}

		if statusCode < 200 || statusCode >= 300 {
			slog.Warn("Hook http: non-2xx status, retrying", "hook_index", idx, "attempt", attempt+1, "attempts", attempts, "status", statusCode)
			if attempt+1 < attempts {
				if !waitRetry(ctx) {
					return r
				}
				continue
			}
			return r
		}

		parseHookResponse(respText, &r)
		return r
	}

	return r
}

func renderWebhookBody(bodyTemplate string, hctx CallContext) ([]byte, error) {
	if bodyTemplate == "" {
		return json.Marshal(hctx)
	}

	tmpl, err := template.New("webhook_body").Parse(bodyTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse body template: %w", err)
	}

	type data struct {
		CallContext
		Args map[string]any
	}
	d := data{CallContext: hctx}
	if len(hctx.Arguments) > 0 {
		var args map[string]any
		if err := json.Unmarshal(hctx.Arguments, &args); err == nil {
			d.Args = args
		}
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, d); err != nil {
		return nil, fmt.Errorf("render body template: %w", err)
	}
	return buf.Bytes(), nil
}

func callWebhook(ctx context.Context, method, url string, headers map[string]string, body []byte) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, "", fmt.Errorf("create request: %w", err)
	}

	if _, ok := headers["Content-Type"]; !ok {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", fmt.Errorf("read response: %w", err)
	}

	return resp.StatusCode, string(bodyBytes), nil
}

func waitRetry(ctx context.Context) bool {
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
