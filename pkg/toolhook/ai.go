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

	"github.com/wweir/warden/config"
)

// runAI calls the gateway's own chat completions endpoint to evaluate a tool call.
// The prompt template is rendered with CallContext fields plus Args (parsed Arguments map).
// The model must respond with JSON {"allow": bool, "reason": "..."}.
// Any error during the call is logged and treated as pass-through (fail-open).
// Only an explicit allow:false in the response triggers rejection.
func runAI(ctx context.Context, idx int, hook config.HookConfig, hctx CallContext, gatewayAddr string) hookResult {
	r := hookResult{index: idx, htype: "ai", when: hook.When}

	prompt, err := renderPrompt(hook.Prompt, hctx)
	if err != nil {
		slog.Warn("Hook AI: failed to render prompt, passing through", "hook_index", idx, "error", err)
		return r
	}

	content, err := callGateway(ctx, gatewayAddr, hook.Route, hook.Model, prompt)
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
func callGateway(ctx context.Context, gatewayAddr, route, model, prompt string) (string, error) {
	addr := gatewayAddr
	if strings.HasPrefix(addr, ":") {
		addr = "localhost" + addr
	}
	url := "http://" + addr + route + "/chat/completions"

	reqBody, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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

	content, ok := chatResp.Choices[0].Message.Content.(string)
	if !ok {
		return "", fmt.Errorf("response content is not a string")
	}
	return content, nil
}
