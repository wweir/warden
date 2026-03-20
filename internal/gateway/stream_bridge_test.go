package gateway

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wweir/warden/config"
)

func TestGatewayAnthropicToChatStreamRelaysFirstFrameWithoutBuffering(t *testing.T) {
	firstChunkSent := make(chan struct{})
	allowFinish := make(chan struct{})
	var signalFirst sync.Once

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"))
		w.(http.Flusher).Flush()
		signalFirst.Do(func() { close(firstChunkSent) })

		<-allowFinish
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:             upstream.URL,
				Protocol:        "openai",
				APIKey:          config.SecretString("token"),
				AnthropicToChat: true,
			},
		},
		Route: map[string]*config.RouteConfig{
			"/anthropic": {
				Protocol: config.RouteProtocolAnthropic,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"claude-compatible": exactModel(config.RouteProtocolAnthropic,
						&config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"},
					),
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

	req, err := http.NewRequest(http.MethodPost, server.URL+"/anthropic/messages", strings.NewReader(`{"model":"claude-compatible","messages":[{"role":"user","content":"hello"}],"max_tokens":64,"stream":true}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("gateway request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d, body=%q", resp.StatusCode, http.StatusOK, string(body))
	}

	<-firstChunkSent
	reader := bufio.NewReader(resp.Body)
	frameCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		frame, readErr := readSSEFrame(reader)
		if readErr != nil {
			errCh <- readErr
			return
		}
		frameCh <- frame
	}()

	select {
	case frame := <-frameCh:
		if !strings.Contains(string(frame), "event: message_start") {
			t.Fatalf("first frame = %q, want message_start", string(frame))
		}
	case readErr := <-errCh:
		t.Fatalf("read first frame: %v", readErr)
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("timed out waiting for first anthropic frame; stream is buffered")
	}

	close(allowFinish)
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read rest: %v", err)
	}
	if !strings.Contains(string(rest), "event: message_stop") {
		t.Fatalf("full stream = %q, want message_stop", string(rest))
	}
}

func TestGatewayAnthropicNativeStreamRelaysFirstFrameWithoutBuffering(t *testing.T) {
	firstChunkSent := make(chan struct{})
	allowFinish := make(chan struct{})
	var signalFirst sync.Once

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-3-5-sonnet\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"))
		w.(http.Flusher).Flush()
		signalFirst.Do(func() { close(firstChunkSent) })

		<-allowFinish
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":1}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"anthropic": {
				URL:      upstream.URL,
				Protocol: "anthropic",
				APIKey:   config.SecretString("token"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/anthropic": {
				Protocol: config.RouteProtocolAnthropic,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"claude-3-5-sonnet": exactModel(config.RouteProtocolAnthropic,
						&config.RouteUpstreamConfig{Provider: "anthropic", Model: "claude-3-5-sonnet"},
					),
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

	req, err := http.NewRequest(http.MethodPost, server.URL+"/anthropic/messages", strings.NewReader(`{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hello"}],"max_tokens":64,"stream":true}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("gateway request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d, body=%q", resp.StatusCode, http.StatusOK, string(body))
	}

	<-firstChunkSent
	reader := bufio.NewReader(resp.Body)
	frameCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		frame, readErr := readSSEFrame(reader)
		if readErr != nil {
			errCh <- readErr
			return
		}
		frameCh <- frame
	}()

	select {
	case frame := <-frameCh:
		if !strings.Contains(string(frame), "event: message_start") {
			t.Fatalf("first frame = %q, want message_start", string(frame))
		}
	case readErr := <-errCh:
		t.Fatalf("read first frame: %v", readErr)
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("timed out waiting for first anthropic frame; native stream is buffered")
	}

	close(allowFinish)
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read rest: %v", err)
	}
	if !strings.Contains(string(rest), "event: message_stop") {
		t.Fatalf("full stream = %q, want message_stop", string(rest))
	}
}

func TestGatewayResponsesToChatCountsTruncatedUpstreamAsInStreamFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"ali-coding": {
				URL:             upstream.URL,
				Protocol:        "openai",
				APIKey:          config.SecretString("token"),
				ResponsesToChat: true,
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolResponsesStateless,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"qwen3-coder-plus": exactModel(config.RouteProtocolResponsesStateless,
						&config.RouteUpstreamConfig{Provider: "ali-coding", Model: "qwen3.5-plus"},
					),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodPost, "/openai/responses", strings.NewReader(`{"model":"qwen3-coder-plus","input":"hello","stream":true}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: response.completed") {
		t.Fatalf("body = %q, want response.completed", body)
	}
	if !strings.Contains(body, "\"status\":\"incomplete\"") {
		t.Fatalf("body = %q, want incomplete status", body)
	}

	status := gw.selector.ProviderDetail("ali-coding")
	if status == nil {
		t.Fatal("provider status is nil")
	}
	if status.InStreamErrors != 1 {
		t.Fatalf("InStreamErrors = %d, want 1", status.InStreamErrors)
	}
}

func TestGatewayAnthropicToChatCountsTruncatedUpstreamAsInStreamFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:             upstream.URL,
				Protocol:        "openai",
				APIKey:          config.SecretString("token"),
				AnthropicToChat: true,
			},
		},
		Route: map[string]*config.RouteConfig{
			"/anthropic": {
				Protocol: config.RouteProtocolAnthropic,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"claude-compatible": exactModel(config.RouteProtocolAnthropic,
						&config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"},
					),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodPost, "/anthropic/messages", strings.NewReader(`{"model":"claude-compatible","messages":[{"role":"user","content":"hello"}],"max_tokens":64,"stream":true}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: message_start") {
		t.Fatalf("body = %q, want message_start", body)
	}
	if strings.Contains(body, "event: message_stop") {
		t.Fatalf("body = %q, want truncated anthropic stream without message_stop", body)
	}

	status := gw.selector.ProviderDetail("openai")
	if status == nil {
		t.Fatal("provider status is nil")
	}
	if status.InStreamErrors != 1 {
		t.Fatalf("InStreamErrors = %d, want 1", status.InStreamErrors)
	}
}
