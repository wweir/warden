package toolhook

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wweir/warden/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func withDefaultClient(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: rt}
	t.Cleanup(func() {
		http.DefaultClient = old
	})
}

func TestRunHTTPReject(t *testing.T) {
	withDefaultClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"allow":false,"reason":"blocked"}`)),
			Header:     make(http.Header),
		}, nil
	}))

	hook := config.HookConfig{
		Type:    "http",
		When:    "block",
		Webhook: "audit",
		WebhookCfg: &config.WebhookConfig{
			URL: "http://example.com/hook",
		},
	}

	r := runHTTP(context.Background(), 0, hook, CallContext{FullName: "filesystem__delete_file"})
	if !r.rejected {
		t.Fatalf("expected rejected=true")
	}
	if r.reason != "blocked" {
		t.Fatalf("expected reason=blocked, got %s", r.reason)
	}
}

func TestRunHTTPRetryAndTemplate(t *testing.T) {
	var n int32
	var reqBody string

	withDefaultClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		reqBody = string(body)
		if atomic.AddInt32(&n, 1) == 1 {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader(`internal`)),
				Header:     make(http.Header),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"allow":true,"reason":"ok"}`)),
			Header:     make(http.Header),
		}, nil
	}))

	hook := config.HookConfig{
		Type:    "http",
		When:    "block",
		Webhook: "audit",
		WebhookCfg: &config.WebhookConfig{
			URL:          "http://example.com/hook",
			Retry:        1,
			BodyTemplate: `{"tool":"{{.FullName}}","path":"{{.Args.path}}"}`,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r := runHTTP(ctx, 0, hook, CallContext{FullName: "filesystem__write_file", Arguments: []byte(`{"path":"/tmp/a"}`)})
	if r.rejected {
		t.Fatalf("expected rejected=false")
	}
	if atomic.LoadInt32(&n) != 2 {
		t.Fatalf("expected 2 attempts, got %d", n)
	}
	if !strings.Contains(reqBody, `"filesystem__write_file"`) {
		t.Fatalf("expected body to contain full_name, got %s", reqBody)
	}
	if !strings.Contains(reqBody, `"/tmp/a"`) {
		t.Fatalf("expected body to contain parsed args, got %s", reqBody)
	}
}

func TestRunHTTPSupportsSprigBodyTemplateFuncs(t *testing.T) {
	var reqBody string

	withDefaultClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		reqBody = string(body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"allow":true}`)),
			Header:     make(http.Header),
		}, nil
	}))

	hook := config.HookConfig{
		Type:    "http",
		When:    "block",
		Webhook: "audit",
		WebhookCfg: &config.WebhookConfig{
			URL:          "http://example.com/hook",
			BodyTemplate: `{"tool":"{{ upper .ToolName }}"}`,
		},
	}

	r := runHTTP(context.Background(), 0, hook, CallContext{ToolName: "write_file"})
	if r.rejected {
		t.Fatalf("expected rejected=false")
	}
	if !strings.Contains(reqBody, `"WRITE_FILE"`) {
		t.Fatalf("expected body to contain transformed tool name, got %s", reqBody)
	}
}

func TestRunHTTPFailOpenOnInvalidJSONResponse(t *testing.T) {
	withDefaultClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`not-json`)),
			Header:     make(http.Header),
		}, nil
	}))

	hook := config.HookConfig{
		Type:    "http",
		When:    "block",
		Webhook: "audit",
		WebhookCfg: &config.WebhookConfig{
			URL: "http://example.com/hook",
		},
	}

	r := runHTTP(context.Background(), 0, hook, CallContext{FullName: "web_search"})
	if r.rejected {
		t.Fatalf("expected rejected=false")
	}
	if r.reason != "" {
		t.Fatalf("expected empty reason, got %s", r.reason)
	}
}

func TestRunHTTPContextCanceledBeforeRequest(t *testing.T) {
	var n int32
	withDefaultClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&n, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"allow":true}`)),
			Header:     make(http.Header),
		}, nil
	}))

	hook := config.HookConfig{
		Type:    "http",
		When:    "block",
		Webhook: "audit",
		WebhookCfg: &config.WebhookConfig{
			URL:   "http://example.com/hook",
			Retry: 1,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := runHTTP(ctx, 0, hook, CallContext{FullName: "web_search"})
	if r.rejected {
		t.Fatalf("expected rejected=false")
	}
	if atomic.LoadInt32(&n) != 0 {
		t.Fatalf("expected no request sent, got %d", n)
	}
}

func TestRunHTTPDefaultContentType(t *testing.T) {
	var contentType string
	withDefaultClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		contentType = req.Header.Get("Content-Type")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"allow":true}`)),
			Header:     make(http.Header),
		}, nil
	}))

	hook := config.HookConfig{
		Type:    "http",
		When:    "block",
		Webhook: "audit",
		WebhookCfg: &config.WebhookConfig{
			URL: "http://example.com/hook",
		},
	}

	r := runHTTP(context.Background(), 0, hook, CallContext{FullName: "web_search"})
	if r.rejected {
		t.Fatalf("expected rejected=false")
	}
	if contentType != "application/json" {
		t.Fatalf("expected application/json, got %s", contentType)
	}
}

func TestRunHTTPUsesWebhookTimeoutWithoutParentDeadline(t *testing.T) {
	var deadlineSet bool
	withDefaultClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		_, deadlineSet = req.Context().Deadline()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"allow":true}`)),
			Header:     make(http.Header),
		}, nil
	}))

	hook := config.HookConfig{
		Type:    "http",
		When:    "block",
		Webhook: "audit",
		WebhookCfg: &config.WebhookConfig{
			URL:     "http://example.com/hook",
			Timeout: "250ms",
		},
	}

	r := runHTTP(context.Background(), 0, hook, CallContext{FullName: "web_search"})
	if r.rejected {
		t.Fatalf("expected rejected=false")
	}
	if !deadlineSet {
		t.Fatal("expected webhook request context to have deadline")
	}
}
