package toolhook

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wweir/warden/config"
)

func TestRunAIUsesHookTimeoutWithoutParentDeadline(t *testing.T) {
	var deadlineSet bool
	withDefaultClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		_, deadlineSet = req.Context().Deadline()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"allow\":true}"}}]}`)),
			Header:     make(http.Header),
		}, nil
	}))

	hook := config.HookConfig{
		Type:            "ai",
		When:            "block",
		Route:           "/openai",
		Model:           "gpt-4o-mini",
		Prompt:          `{{.FullName}}`,
		TimeoutDuration: 250 * time.Millisecond,
	}

	r := runAI(context.Background(), 0, hook, CallContext{FullName: "filesystem__write_file"}, ":8080")
	if r.rejected {
		t.Fatalf("expected rejected=false")
	}
	if !deadlineSet {
		t.Fatal("expected AI request context to have deadline")
	}
}

func TestRunAIParsesStructuredTextContent(t *testing.T) {
	withDefaultClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":[{"type":"text","text":"{\"allow\":false,\"reason\":\"blocked\"}"}]}}]}`)),
			Header:     make(http.Header),
		}, nil
	}))

	hook := config.HookConfig{
		Type:            "ai",
		When:            "block",
		Route:           "/openai",
		Model:           "gpt-4o-mini",
		Prompt:          `{{.FullName}}`,
		TimeoutDuration: 250 * time.Millisecond,
	}

	r := runAI(context.Background(), 0, hook, CallContext{FullName: "filesystem__write_file"}, ":8080")
	if !r.rejected || r.reason != "blocked" {
		t.Fatalf("expected structured text response to reject, got %+v", r)
	}
}

func TestRunAIParsesMixedContentParts(t *testing.T) {
	withDefaultClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":[{"type":"reasoning","text":"ignore me"},{"type":"text","text":"{\"allow\":false,\"reason\":\"blocked\"}"}]}}]}`)),
			Header:     make(http.Header),
		}, nil
	}))

	hook := config.HookConfig{
		Type:            "ai",
		When:            "block",
		Route:           "/openai",
		Model:           "gpt-4o-mini",
		Prompt:          `{{.FullName}}`,
		TimeoutDuration: 250 * time.Millisecond,
	}

	r := runAI(context.Background(), 0, hook, CallContext{FullName: "filesystem__write_file"}, ":8080")
	if !r.rejected || r.reason != "blocked" {
		t.Fatalf("expected mixed content response to reject, got %+v", r)
	}
}
