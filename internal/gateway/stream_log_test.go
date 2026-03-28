package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	proxypkg "github.com/wweir/warden/internal/gateway/proxy"
	"github.com/wweir/warden/internal/reqlog"
)

func TestGatewayLogsResponsesStreamAsFinalResponseObject(t *testing.T) {
	t.Parallel()

	const streamBody = "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_123\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_123\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-4o\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":5,\"total_tokens\":8}}}\n\n"

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

func TestAssembleProxyResponseConvertsAnthropicChatStreamToChatCompletionObject(t *testing.T) {
	t.Parallel()

	const streamBody = "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_abc123\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-3-5-sonnet\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	logged := proxypkg.AssembleResponse(config.RouteProtocolChat, "anthropic", []byte(streamBody))
	if !gjson.ValidBytes(logged) {
		t.Fatalf("logged response is not valid JSON: %q", string(logged))
	}
	if got := gjson.GetBytes(logged, "object").String(); got != "chat.completion" {
		t.Fatalf("logged object = %q, want chat.completion", got)
	}
	if got := gjson.GetBytes(logged, "choices.0.message.content").String(); got != "Hello world" {
		t.Fatalf("logged content = %q, want Hello world", got)
	}
	if got := gjson.GetBytes(logged, "choices.0.finish_reason").String(); got != "stop" {
		t.Fatalf("logged finish_reason = %q, want stop", got)
	}
	if got := gjson.GetBytes(logged, "usage.prompt_tokens").Int(); got != 10 {
		t.Fatalf("logged prompt tokens = %d, want 10", got)
	}
	if got := gjson.GetBytes(logged, "usage.completion_tokens").Int(); got != 5 {
		t.Fatalf("logged completion tokens = %d, want 5", got)
	}
}
