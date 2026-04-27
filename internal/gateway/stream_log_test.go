package gateway

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
)

func TestGatewayLogsResponsesStreamAsFinalResponseObject(t *testing.T) {
	t.Parallel()

	const streamBody = "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_123\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_123\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-4o\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":5,\"total_tokens\":8,\"input_tokens_details\":{\"cached_tokens\":2}}}}\n\n"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
			return
		}
		if r.URL.Path != "/responses" {
			t.Fatalf("upstream path = %q, want /responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(streamBody))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("provider-token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolResponsesStateless,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": exactModel(config.RouteProtocolResponsesStateless, &config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodPost, "/openai/responses", strings.NewReader(`{"model":"gpt-4o","input":"hello","stream":true}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}

	record := mustSingleLogRecord(t, gw.Broadcaster().Recent())
	if !gjson.ValidBytes(record.Response) {
		t.Fatalf("logged response is not valid JSON: %q", string(record.Response))
	}
	if strings.Contains(string(record.Response), "data: ") {
		t.Fatalf("logged response should not contain SSE wrapper: %q", string(record.Response))
	}
	if got := gjson.GetBytes(record.Response, "object").String(); got != "response" {
		t.Fatalf("logged object = %q, want response", got)
	}
	if got := gjson.GetBytes(record.Response, "output.0.content.0.text").String(); got != "ok" {
		t.Fatalf("logged text = %q, want ok", got)
	}
	if record.TokenUsage == nil {
		t.Fatal("expected token_usage in log record")
	}
	if record.TokenUsage.PromptTokens != 3 || record.TokenUsage.CompletionTokens != 5 || record.TokenUsage.CacheTokens != 2 {
		t.Fatalf("unexpected token_usage: %+v", record.TokenUsage)
	}
	if record.TokenUsage.Completeness != "exact" || record.TokenUsage.Source != "reported_sse" {
		t.Fatalf("unexpected token_usage metadata: %+v", record.TokenUsage)
	}
}

func TestGatewayPublishesPendingStreamLogBeforeUpstreamCompletes(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
			return
		}
		if r.URL.Path != "/responses" {
			t.Fatalf("upstream path = %q, want /responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_123\",\"status\":\"in_progress\",\"output\":[]}}\n\n"))
		w.(http.Flusher).Flush()
		close(started)
		<-release
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_123\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-4o\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("provider-token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolResponsesStateless,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": exactModel(config.RouteProtocolResponsesStateless, &config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodPost, "/openai/responses", strings.NewReader(`{"model":"gpt-4o","input":"hello","stream":true}`))
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		gw.ServeHTTP(rec, req)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream stream start")
	}

	var pending reqlog.Record
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		records := gw.Broadcaster().Recent()
		if len(records) == 1 && records[0].Pending {
			pending = records[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pending.RequestID == "" {
		t.Fatal("pending stream log was not published before completion")
	}
	if len(pending.Response) != 0 {
		t.Fatalf("pending response length = %d, want 0", len(pending.Response))
	}

	close(release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for gateway response")
	}

	record := mustSingleLogRecord(t, gw.Broadcaster().Recent())
	if record.Pending {
		t.Fatal("final log is still marked pending")
	}
	if got := gjson.GetBytes(record.Response, "output.0.content.0.text").String(); got != "ok" {
		t.Fatalf("logged text = %q, want ok", got)
	}
}

func TestGatewayRelaysChatStreamBeforeUpstreamCompletes(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
			return
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("upstream path = %q, want /chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_123\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hel\"}}]}\n\n"))
		w.(http.Flusher).Flush()
		<-release
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_123\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("provider-token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": exactModel(config.RouteProtocolChat, &config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)
	server := httptest.NewServer(gw)
	defer server.Close()

	firstFrame := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/openai/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":true}`))
		if err != nil {
			errCh <- err
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()
		reader := bufio.NewReader(resp.Body)
		line, err := reader.ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		firstFrame <- line
		_, _ = io.ReadAll(reader)
		errCh <- nil
	}()

	select {
	case line := <-firstFrame:
		if !strings.Contains(line, `"content":"hel"`) {
			close(release)
			t.Fatalf("first frame = %q, want streamed content", line)
		}
	case err := <-errCh:
		close(release)
		t.Fatalf("request failed before first frame: %v", err)
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("timed out waiting for first chat stream frame before upstream completion")
	}

	close(release)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("finish request: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stream completion")
	}
}

func TestGatewayRecordsIncompleteChatStreamAsInStreamError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
			return
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("upstream path = %q, want /chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_123\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hel\"}}]}\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("provider-token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": exactModel(config.RouteProtocolChat, &config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), `"content":"hel"`) {
		t.Fatalf("body = %q, want relayed partial content", rec.Body.String())
	}

	record := mustSingleLogRecord(t, gw.Broadcaster().Recent())
	if record.Error == "" {
		t.Fatalf("log error is empty, want incomplete stream error")
	}
	status := gw.selector.ProviderDetail("openai")
	if status == nil {
		t.Fatal("ProviderDetail(openai) = nil")
	}
	if status.InStreamErrors != 1 || status.FailureCount != 1 {
		t.Fatalf("provider stream counters = in_stream:%d failures:%d, want 1/1", status.InStreamErrors, status.FailureCount)
	}
}

func TestGatewayFallsBackWhenStreamRequestReturnsJSON(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
			return
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("upstream path = %q, want /chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`))
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
				Protocol: "openai",
				APIKey:   config.SecretString("provider-token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": exactModel(config.RouteProtocolChat, &config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := gjson.Get(rec.Body.String(), "choices.0.message.content").String(); got != "hello" {
		t.Fatalf("response content = %q, want hello", got)
	}

	record := mustSingleLogRecord(t, gw.Broadcaster().Recent())
	if record.TokenUsage == nil {
		t.Fatal("expected token_usage in log record")
	}
	if record.TokenUsage.PromptTokens != 3 || record.TokenUsage.CompletionTokens != 5 {
		t.Fatalf("unexpected token_usage: %+v", record.TokenUsage)
	}
}
