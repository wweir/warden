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

	"github.com/Masterminds/sprig/v3"
	"github.com/wweir/warden/config"
)

const defaultWebhookTimeout = 5 * time.Second

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

		statusCode, respText, reqErr := callWebhook(ctx, method, hook.WebhookCfg, body)
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

	tmpl, err := template.New("webhook_body").Funcs(sprig.FuncMap()).Parse(bodyTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse body template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, newHookTemplateData(hctx)); err != nil {
		return nil, fmt.Errorf("render body template: %w", err)
	}
	return buf.Bytes(), nil
}

func callWebhook(ctx context.Context, method string, webhookCfg *config.WebhookConfig, body []byte) (int, string, error) {
	reqCtx, cancel, err := withOptionalTimeout(ctx, parseWebhookTimeout(webhookCfg.Timeout, defaultWebhookTimeout))
	if err != nil {
		return 0, "", fmt.Errorf("resolve request timeout: %w", err)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, webhookCfg.URL, bytes.NewReader(body))
	if err != nil {
		return 0, "", fmt.Errorf("create request: %w", err)
	}

	if _, ok := webhookCfg.Headers["Content-Type"]; !ok {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range webhookCfg.Headers {
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

func parseWebhookTimeout(raw string, fallback time.Duration) time.Duration {
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
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
