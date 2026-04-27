package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wweir/warden/config"
	gatewaypkg "github.com/wweir/warden/internal/gateway"
	"github.com/wweir/warden/internal/reqlog"
	"github.com/wweir/warden/pkg/toolhook"
)

// --- helpers ---

// hookServer returns a webhook server that responds with {allow, reason}.
func hookServer(allow bool, reason string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"allow":%t,"reason":%q}`, allow, reason)
	}))
}

// chatResponseWithToolCalls builds a non-stream Chat response JSON with tool_calls.
func chatResponseWithToolCalls(toolCalls ...map[string]any) string {
	tcs, _ := json.Marshal(toolCalls)
	return fmt.Sprintf(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"gpt-4o",`+
		`"choices":[{"index":0,"message":{"role":"assistant","tool_calls":%s},"finish_reason":"tool_calls"}],`+
		`"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, tcs)
}

func chatResponseWithChoices(choices ...map[string]any) string {
	rawChoices, _ := json.Marshal(choices)
	return fmt.Sprintf(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"gpt-4o",`+
		`"choices":%s,`+
		`"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, rawChoices)
}

// responsesResponseWithFuncCalls builds a non-stream Responses response JSON with function_call items.
func responsesResponseWithFuncCalls(funcCalls ...map[string]any) string {
	output, _ := json.Marshal(funcCalls)
	return fmt.Sprintf(`{"id":"resp_1","status":"completed","output":%s,`+
		`"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`, output)
}

// anthropicResponseWithToolUse builds a non-stream Anthropic response JSON with tool_use blocks.
func anthropicResponseWithToolUse(toolUses ...map[string]any) string {
	content, _ := json.Marshal(toolUses)
	return fmt.Sprintf(`{"id":"msg_1","type":"message","role":"assistant","content":%s,`+
		`"model":"claude-3","stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`, content)
}

func makeToolCall(id, name, args string) map[string]any {
	return map[string]any{
		"id":   id,
		"type": "function",
		"function": map[string]string{
			"name":      name,
			"arguments": args,
		},
	}
}

func makeFuncCallItem(callID, name, args string) map[string]any {
	return map[string]any{
		"type":      "function_call",
		"call_id":   callID,
		"name":      name,
		"arguments": args,
		"id":        "fc_" + callID,
		"status":    "completed",
	}
}

func makeToolUseBlock(id, name string, input any) map[string]any {
	return map[string]any{
		"type":  "tool_use",
		"id":    id,
		"name":  name,
		"input": input,
	}
}

// newUpstreamFixedResponse creates an upstream that returns body for requests
// to expectedPath, and handles /models.
func newUpstreamFixedResponse(expectedPath, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
}

// buildGatewayWithHooks creates a config+gateway for a single route/provider with hooks.
// Hook configs using Webhook="audit" are auto-registered with the corresponding WebhookCfg.URL.
func buildGatewayWithHooks(t *testing.T, routePrefix, routeProtocol, upstreamURL, provProtocol, modelName string, hooks []*config.HookRuleConfig) *gatewaypkg.Gateway {
	t.Helper()

	// Collect webhook references from hook configs.
	webhooks := map[string]*config.WebhookConfig{}
	for _, rule := range hooks {
		if rule.Hook.Type == "http" && rule.Hook.Webhook != "" && rule.Hook.WebhookCfg != nil {
			webhooks[rule.Hook.Webhook] = rule.Hook.WebhookCfg
			rule.Hook.WebhookCfg = nil // let Validate resolve it
		}
	}

	cfg := &config.ConfigStruct{
		Addr:    ":0",
		Webhook: webhooks,
		Provider: map[string]*config.ProviderConfig{
			"upstream": {
				URL:      upstreamURL,
				Protocol: provProtocol,
				APIKey:   config.SecretString("test-key"),
			},
		},
		Route: map[string]*config.RouteConfig{
			routePrefix: {
				Protocol: routeProtocol,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					modelName: exactModel(routeProtocol, &config.RouteUpstreamConfig{
						Provider: "upstream",
						Model:    modelName,
					}),
				},
				Hooks: hooks,
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	gw := gatewaypkg.NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)
	return gw
}

// --- Tests ---

func TestToolHookBlockRemovesRejectedToolCall_Chat(t *testing.T) {
	t.Parallel()

	hookSrv := hookServer(false, "dangerous tool")
	defer hookSrv.Close()

	respBody := chatResponseWithToolCalls(
		makeToolCall("call_safe", "read_file", `{"path":"/tmp/a"}`),
		makeToolCall("call_bad", "delete_file", `{"path":"/"}`),
	)
	upstream := newUpstreamFixedResponse("/chat/completions", respBody)
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolChat, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{
			{Match: "delete_file", Hook: config.HookConfig{
				Type:    "http",
				When:    "block",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: hookSrv.URL,
				},
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	// Parse response: delete_file call should be removed
	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	toolCalls := resp.Choices[0].Message.ToolCalls
	if len(toolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1 (delete_file should be removed)", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "read_file" {
		t.Fatalf("remaining tool call = %q, want read_file", toolCalls[0].Function.Name)
	}

	// Check log record has verdict
	records := gw.Broadcaster().Recent()
	record := mustSingleLogRecord(t, records)
	assertHasRejectedVerdict(t, record.ToolVerdicts, "delete_file")
}

func TestToolHookBlockRunsForNonStreamFallback_Chat(t *testing.T) {
	t.Parallel()

	hookSrv := hookServer(false, "dangerous tool")
	defer hookSrv.Close()

	respBody := chatResponseWithToolCalls(
		makeToolCall("call_safe", "read_file", `{"path":"/tmp/a"}`),
		makeToolCall("call_bad", "delete_file", `{"path":"/"}`),
	)
	upstream := newUpstreamFixedResponse("/chat/completions", respBody)
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolChat, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{
			{Match: "delete_file", Hook: config.HookConfig{
				Type:    "http",
				When:    "block",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: hookSrv.URL,
				},
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	toolCalls := resp.Choices[0].Message.ToolCalls
	if len(toolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1 (delete_file should be removed)", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "read_file" {
		t.Fatalf("remaining tool call = %q, want read_file", toolCalls[0].Function.Name)
	}

	record := mustSingleLogRecord(t, gw.Broadcaster().Recent())
	assertHasRejectedVerdict(t, record.ToolVerdicts, "delete_file")
}

func TestToolHookBlockRemovesAllToolCalls_Chat(t *testing.T) {
	t.Parallel()

	hookSrv := hookServer(false, "blocked")
	defer hookSrv.Close()

	respBody := chatResponseWithToolCalls(
		makeToolCall("call_1", "delete_file", `{"path":"/"}`),
	)
	upstream := newUpstreamFixedResponse("/chat/completions", respBody)
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolChat, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{
			{Match: "*", Hook: config.HookConfig{
				Type:    "http",
				When:    "block",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: hookSrv.URL,
				},
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// When all tool calls removed, finish_reason should change to "stop"
	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls json.RawMessage `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("no choices")
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason = %q, want stop", resp.Choices[0].FinishReason)
	}
	if len(resp.Choices[0].Message.ToolCalls) > 0 && string(resp.Choices[0].Message.ToolCalls) != "null" {
		t.Fatalf("tool_calls should be removed, got %s", resp.Choices[0].Message.ToolCalls)
	}
}

func TestToolHookBlockRemovesRejectedFuncCall_Responses(t *testing.T) {
	t.Parallel()

	hookSrv := hookServer(false, "not allowed")
	defer hookSrv.Close()

	respBody := responsesResponseWithFuncCalls(
		makeFuncCallItem("call_ok", "search", `{"q":"hello"}`),
		makeFuncCallItem("call_bad", "exec_cmd", `{"cmd":"rm -rf /"}`),
	)
	upstream := newUpstreamFixedResponse("/responses", respBody)
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolResponsesStateless, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{
			{Match: "exec_cmd", Hook: config.HookConfig{
				Type:    "http",
				When:    "block",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: hookSrv.URL,
				},
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/responses",
		strings.NewReader(`{"model":"gpt-4o","input":"hello"}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Output []json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("output items = %d, want 1 (exec_cmd removed)", len(resp.Output))
	}

	var item struct {
		Name string `json:"name"`
	}
	json.Unmarshal(resp.Output[0], &item)
	if item.Name != "search" {
		t.Fatalf("remaining item name = %q, want search", item.Name)
	}

	record := mustSingleLogRecord(t, gw.Broadcaster().Recent())
	assertHasRejectedVerdict(t, record.ToolVerdicts, "exec_cmd")
}

func TestToolHookBlockRemovesRejectedToolUse_Anthropic(t *testing.T) {
	t.Parallel()

	hookSrv := hookServer(false, "blocked")
	defer hookSrv.Close()

	respBody := anthropicResponseWithToolUse(
		makeToolUseBlock("tu_safe", "read_file", map[string]any{"path": "/tmp/a"}),
		makeToolUseBlock("tu_bad", "shell_exec", map[string]any{"cmd": "rm -rf /"}),
	)
	upstream := newUpstreamFixedResponse("/v1/messages", respBody)
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/anthropic", config.RouteProtocolAnthropic, upstream.URL+"/v1", "anthropic", "claude-3",
		[]*config.HookRuleConfig{
			{Match: "shell_exec", Hook: config.HookConfig{
				Type:    "http",
				When:    "block",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: hookSrv.URL,
				},
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/anthropic/messages",
		strings.NewReader(`{"model":"claude-3","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Content    []map[string]any `json:"content"`
		StopReason string           `json:"stop_reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("content blocks = %d, want 1 (shell_exec removed)", len(resp.Content))
	}
	if name, _ := resp.Content[0]["name"].(string); name != "read_file" {
		t.Fatalf("remaining block name = %q, want read_file", name)
	}

	record := mustSingleLogRecord(t, gw.Broadcaster().Recent())
	assertHasRejectedVerdict(t, record.ToolVerdicts, "shell_exec")
}

func TestToolHookAllowPassesThrough(t *testing.T) {
	t.Parallel()

	hookSrv := hookServer(true, "ok")
	defer hookSrv.Close()

	respBody := chatResponseWithToolCalls(
		makeToolCall("call_1", "read_file", `{"path":"/tmp/a"}`),
	)
	upstream := newUpstreamFixedResponse("/chat/completions", respBody)
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolChat, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{
			{Match: "*", Hook: config.HookConfig{
				Type:    "http",
				When:    "block",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: hookSrv.URL,
				},
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []json.RawMessage `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1 (should pass through)", len(resp.Choices[0].Message.ToolCalls))
	}

	record := mustSingleLogRecord(t, gw.Broadcaster().Recent())
	for _, v := range record.ToolVerdicts {
		if v.Rejected {
			t.Fatalf("unexpected rejected verdict: %+v", v)
		}
	}
}

func TestToolHookAsyncRecordsVerdictButDoesNotModifyResponse(t *testing.T) {
	t.Parallel()

	hookSrv := hookServer(false, "flagged for review")
	defer hookSrv.Close()

	respBody := chatResponseWithToolCalls(
		makeToolCall("call_1", "delete_file", `{"path":"/"}`),
	)
	upstream := newUpstreamFixedResponse("/chat/completions", respBody)
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolChat, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{
			{Match: "*", Hook: config.HookConfig{
				Type:    "http",
				When:    "async",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: hookSrv.URL,
				},
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Async mode: tool call should still be present in response
	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []json.RawMessage `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1 (async should not remove)", len(resp.Choices[0].Message.ToolCalls))
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", resp.Choices[0].FinishReason)
	}

	record := waitForRejectedVerdict(t, gw, "delete_file", "async")
	if len(record.ToolVerdicts) == 0 {
		t.Fatal("expected async verdicts in request log")
	}
}

func TestToolHookAsyncLogUpdatePreservesInitialDuration(t *testing.T) {
	t.Parallel()

	hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"allow":false,"reason":"flagged for review"}`)
	}))
	defer hookSrv.Close()

	respBody := chatResponseWithToolCalls(
		makeToolCall("call_1", "delete_file", `{"path":"/"}`),
	)
	upstream := newUpstreamFixedResponse("/chat/completions", respBody)
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolChat, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{
			{Match: "*", Hook: config.HookConfig{
				Type:    "http",
				When:    "async",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: hookSrv.URL,
				},
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	initial := mustSingleLogRecord(t, gw.Broadcaster().Recent())
	if len(initial.ToolVerdicts) != 0 {
		t.Fatalf("expected initial log without async verdicts, got %+v", initial.ToolVerdicts)
	}

	updated := waitForRejectedVerdict(t, gw, "delete_file", "async")
	if updated.DurationMs != initial.DurationMs {
		t.Fatalf("updated duration = %d, want initial duration %d", updated.DurationMs, initial.DurationMs)
	}
}

func TestToolHookAsyncStreamLogUpdatePreservesInitialDuration(t *testing.T) {
	t.Parallel()

	hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"allow":false,"reason":"flagged for review"}`)
	}))
	defer hookSrv.Close()

	const streamBody = "" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"delete_file\",\"arguments\":\"\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"path\\\":\\\"/\\\"}\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n" +
		"data: [DONE]\n\n"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, streamBody)
	}))
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolChat, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{
			{Match: "*", Hook: config.HookConfig{
				Type:    "http",
				When:    "async",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: hookSrv.URL,
				},
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	initial := mustSingleLogRecord(t, gw.Broadcaster().Recent())
	if len(initial.ToolVerdicts) != 0 {
		t.Fatalf("expected initial stream log without async verdicts, got %+v", initial.ToolVerdicts)
	}

	updated := waitForRejectedVerdict(t, gw, "delete_file", "async")
	if updated.DurationMs != initial.DurationMs {
		t.Fatalf("updated duration = %d, want initial duration %d", updated.DurationMs, initial.DurationMs)
	}
}

func TestToolHookBlockAndAsyncCombined(t *testing.T) {
	t.Parallel()

	blockSrv := hookServer(false, "blocked by policy")
	defer blockSrv.Close()
	asyncSrv := hookServer(false, "flagged")
	defer asyncSrv.Close()

	respBody := chatResponseWithToolCalls(
		makeToolCall("call_1", "delete_file", `{"path":"/"}`),
		makeToolCall("call_2", "read_file", `{"path":"/tmp/a"}`),
	)
	upstream := newUpstreamFixedResponse("/chat/completions", respBody)
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolChat, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{
			{Match: "delete_file", Hook: config.HookConfig{
				Type:    "http",
				When:    "block",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: blockSrv.URL,
				},
			}},
			{Match: "*", Hook: config.HookConfig{
				Type:    "http",
				When:    "async",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: asyncSrv.URL,
				},
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Block should remove delete_file
	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1", len(resp.Choices[0].Message.ToolCalls))
	}
	if resp.Choices[0].Message.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("remaining = %q, want read_file", resp.Choices[0].Message.ToolCalls[0].Function.Name)
	}

	record := waitForRejectedVerdict(t, gw, "delete_file", "async")
	var blockRejected, asyncRejected bool
	for _, v := range record.ToolVerdicts {
		if v.Rejected && v.Mode == "block" {
			blockRejected = true
		}
		if v.Rejected && v.Mode == "async" {
			asyncRejected = true
		}
	}
	if !blockRejected {
		t.Fatal("expected block rejected verdict in log")
	}
	if !asyncRejected {
		t.Fatal("expected async rejected verdict in request log")
	}
}

func TestToolHookBlockRemovesRejectedToolCallsFromAllChoices(t *testing.T) {
	t.Parallel()

	hookSrv := hookServer(false, "blocked")
	defer hookSrv.Close()

	respBody := chatResponseWithChoices(
		map[string]any{
			"index": 0,
			"message": map[string]any{
				"role": "assistant",
				"tool_calls": []map[string]any{
					makeToolCall("call_1", "read_file", `{"path":"/tmp/a"}`),
				},
			},
			"finish_reason": "tool_calls",
		},
		map[string]any{
			"index": 1,
			"message": map[string]any{
				"role": "assistant",
				"tool_calls": []map[string]any{
					makeToolCall("call_2", "delete_file", `{"path":"/"}`),
				},
			},
			"finish_reason": "tool_calls",
		},
	)
	upstream := newUpstreamFixedResponse("/chat/completions", respBody)
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolChat, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{{
			Match: "delete_file",
			Hook: config.HookConfig{
				Type:    "http",
				When:    "block",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: hookSrv.URL,
				},
			},
		}},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"n":2}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Choices) != 2 {
		t.Fatalf("choices = %d, want 2", len(resp.Choices))
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 || resp.Choices[0].Message.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("choice 0 tool_calls = %+v, want read_file preserved", resp.Choices[0].Message.ToolCalls)
	}
	if len(resp.Choices[1].Message.ToolCalls) != 0 {
		t.Fatalf("choice 1 tool_calls = %+v, want blocked call removed", resp.Choices[1].Message.ToolCalls)
	}
	if resp.Choices[1].FinishReason != "stop" {
		t.Fatalf("choice 1 finish_reason = %q, want stop", resp.Choices[1].FinishReason)
	}
}

func TestToolHookFailOpenOnTimeout(t *testing.T) {
	t.Parallel()

	// Slow hook that exceeds timeout
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"allow":false,"reason":"should not reach"}`)
	}))
	defer slowSrv.Close()

	respBody := chatResponseWithToolCalls(
		makeToolCall("call_1", "dangerous_tool", `{}`),
	)
	upstream := newUpstreamFixedResponse("/chat/completions", respBody)
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolChat, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{
			{Match: "*", Hook: config.HookConfig{
				Type:    "http",
				When:    "block",
				Timeout: "100ms",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: slowSrv.URL,
				},
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Fail-open: tool call should remain
	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []json.RawMessage `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1 (fail-open should keep)", len(resp.Choices[0].Message.ToolCalls))
	}
}

func TestToolHookBackwardCompatPrePost(t *testing.T) {
	t.Parallel()

	hookSrv := hookServer(false, "blocked")
	defer hookSrv.Close()

	respBody := chatResponseWithToolCalls(
		makeToolCall("call_1", "bad_tool", `{}`),
	)
	upstream := newUpstreamFixedResponse("/chat/completions", respBody)
	defer upstream.Close()

	// Use legacy "pre" value — should be normalized to "block" by Validate
	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolChat, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{
			{Match: "*", Hook: config.HookConfig{
				Type:    "http",
				When:    "pre",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: hookSrv.URL,
				},
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// "pre" normalized to "block": tool call should be removed
	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls json.RawMessage `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason = %q, want stop (pre→block should remove tool_calls)", resp.Choices[0].FinishReason)
	}
}

func TestToolHookNoMatchDoesNotAffectResponse(t *testing.T) {
	t.Parallel()

	hookSrv := hookServer(false, "should not match")
	defer hookSrv.Close()

	respBody := chatResponseWithToolCalls(
		makeToolCall("call_1", "safe_tool", `{}`),
	)
	upstream := newUpstreamFixedResponse("/chat/completions", respBody)
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolChat, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{
			{Match: "dangerous_*", Hook: config.HookConfig{
				Type:    "http",
				When:    "block",
				Webhook: "audit",
				WebhookCfg: &config.WebhookConfig{
					URL: hookSrv.URL,
				},
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []json.RawMessage `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1 (no match should not affect)", len(resp.Choices[0].Message.ToolCalls))
	}
}

// --- assertion helpers ---

func assertHasRejectedVerdict(t *testing.T, verdicts []toolhook.HookVerdict, toolName string) {
	t.Helper()
	for _, v := range verdicts {
		if v.Rejected && v.ToolName == toolName {
			return
		}
	}
	t.Fatalf("expected rejected verdict for tool %q in %+v", toolName, verdicts)
}

func waitForRejectedVerdict(t *testing.T, gw *gatewaypkg.Gateway, toolName, mode string) reqlog.Record {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		record := mustSingleLogRecord(t, gw.Broadcaster().Recent())
		for _, v := range record.ToolVerdicts {
			if v.ToolName == toolName && v.Mode == mode && v.Rejected {
				return record
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s rejected verdict for tool %q", mode, toolName)
	return reqlog.Record{}
}

func TestToolHookAsyncDoesNotBlockResponse(t *testing.T) {
	t.Parallel()

	hookStarted := make(chan struct{}, 1)
	releaseHook := make(chan struct{})
	hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case hookStarted <- struct{}{}:
		default:
		}
		<-releaseHook
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"allow":false,"reason":"flagged"}`)
	}))
	defer hookSrv.Close()
	defer close(releaseHook)

	respBody := chatResponseWithToolCalls(
		makeToolCall("call_1", "delete_file", `{"path":"/"}`),
	)
	upstream := newUpstreamFixedResponse("/chat/completions", respBody)
	defer upstream.Close()

	gw := buildGatewayWithHooks(t, "/openai", config.RouteProtocolChat, upstream.URL, "openai", "gpt-4o",
		[]*config.HookRuleConfig{{
			Match: "*",
			Hook: config.HookConfig{
				Type:       "http",
				When:       "async",
				Webhook:    "audit",
				WebhookCfg: &config.WebhookConfig{URL: hookSrv.URL},
			},
		}},
	)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	start := time.Now()
	gw.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("async hook blocked response for %v", elapsed)
	}
	select {
	case <-hookStarted:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("expected async hook to start in background")
	}
}
