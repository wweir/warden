package toolhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/wweir/warden/config"
)

const defaultAIHookTimeout = 5 * time.Second

// runAI calls the gateway's own chat completions endpoint to evaluate a tool call.
// The prompt template is rendered with CallContext fields plus Args (parsed Arguments map).
// The model must respond with JSON {"allow": bool, "reason": "..."}.
// Any error during the call is logged and treated as pass-through (fail-open).
// Only an explicit allow:false in the response triggers rejection.
func runAI(ctx context.Context, idx int, hook config.HookConfig, hctx CallContext, gateway GatewayTarget) hookResult {
	r := hookResult{index: idx, htype: "ai", when: hook.When}

	prompt, err := renderPrompt(hook.Prompt, hctx)
	if err != nil {
		slog.Warn("Hook AI: failed to render prompt, passing through", "hook_index", idx, "error", err)
		return r
	}

	content, err := callGateway(ctx, gateway, hook, prompt)
	if err != nil {
		slog.Warn("Hook AI: request failed, passing through", "hook_index", idx, "error", err)
		return r
	}
	r.aiResponse = content

	parseHookResponse(content, &r)
	return r
}

// renderPrompt renders the prompt template with CallContext and parsed Arguments fields.
func renderPrompt(promptTmpl string, hctx CallContext) (string, error) {
	tmpl, err := template.New("prompt").Parse(promptTmpl)
	if err != nil {
		return "", fmt.Errorf("parse prompt template: %w", err)
	}

	// Args exposes individual fields from the Arguments JSON object
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
		return "", fmt.Errorf("render prompt template: %w", err)
	}
	return buf.String(), nil
}

// callGateway sends a chat completion request to the gateway and returns the assistant content.
func callGateway(ctx context.Context, gateway GatewayTarget, hook config.HookConfig, prompt string) (string, error) {
	reqCtx, cancel, err := withOptionalTimeout(ctx, timeoutOrDefault(hook.TimeoutDuration, defaultAIHookTimeout))
	if err != nil {
		return "", fmt.Errorf("resolve request timeout: %w", err)
	}
	defer cancel()

	addr := gateway.Addr
	if strings.HasPrefix(addr, ":") {
		addr = "localhost" + addr
	}
	url := "http://" + addr + hook.Route + "/chat/completions"

	reqBody, err := json.Marshal(map[string]any{
		"model": hook.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if gateway.InternalAuthToken != "" {
		req.Header.Set(InternalAuthHeader, gateway.InternalAuthToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("non-2xx status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("response has no choices")
	}

	content, err := extractAssistantText(chatResp.Choices[0].Message.Content)
	if err != nil {
		return "", err
	}
	return content, nil
}

func extractAssistantText(content any) (string, error) {
	switch v := content.(type) {
	case string:
		return v, nil
	case []any:
		var parts []string
		for idx, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				return "", fmt.Errorf("response content[%d] is not an object", idx)
			}
			partType, _ := obj["type"].(string)
			if partType != "" && !slices.Contains([]string{"text", "output_text"}, partType) {
				continue
			}
			text, _ := obj["text"].(string)
			if text == "" {
				text, _ = obj["content"].(string)
			}
			if text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) == 0 {
			return "", fmt.Errorf("response content array has no text parts")
		}
		return strings.Join(parts, ""), nil
	default:
		return "", fmt.Errorf("response content type %T is not supported", content)
	}
}

func timeoutOrDefault(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}

func withOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc, error) {
	if timeout <= 0 {
		return ctx, func() {}, nil
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}, nil
	}
	ctxWithTimeout, cancel := context.WithTimeout(ctx, timeout)
	return ctxWithTimeout, cancel, nil
}
