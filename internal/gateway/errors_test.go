package gateway

import (
	"context"
	"fmt"
	"net"
	"testing"
)

func TestIsRetryableError_Nil(t *testing.T) {
	if IsRetryableError(nil) {
		t.Error("IsRetryableError(nil) = true, want false")
	}
}

func TestIsRetryableError_ContextCanceled(t *testing.T) {
	if IsRetryableError(context.Canceled) {
		t.Error("IsRetryableError(context.Canceled) = true, want false")
	}
}

func TestIsRetryableError_UpstreamRetryable(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{500, true},
		{502, true},
		{503, true},
		{429, true},
		{404, true},
		{403, true},
		{400, true},
		{401, false}, // auth error, not retryable
		{422, false}, // unprocessable entity, not retryable
	}
	for _, c := range cases {
		err := &UpstreamError{Code: c.code}
		got := IsRetryableError(err)
		if got != c.want {
			t.Errorf("IsRetryableError(&UpstreamError{Code: %d}) = %v, want %v", c.code, got, c.want)
		}
	}
}

func TestIsRetryableError_NetworkError(t *testing.T) {
	// net.OpError is retryable
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("connection refused")}
	if !IsRetryableError(opErr) {
		t.Error("IsRetryableError(net.OpError) = false, want true")
	}

	// DNS error is retryable
	dnsErr := &net.DNSError{Err: "no such host", Name: "example.com"}
	if !IsRetryableError(dnsErr) {
		t.Error("IsRetryableError(net.DNSError) = false, want true")
	}
}

func TestIsRetryableError_UnknownError(t *testing.T) {
	// Generic errors are not retryable
	if IsRetryableError(fmt.Errorf("some random error")) {
		t.Error("IsRetryableError(generic error) = true, want false")
	}
}

func TestIsRetryableError_UpstreamHTMLBody(t *testing.T) {
	// HTML body on 4xx is retryable (proxy error)
	err := &UpstreamError{Code: 422, Body: "<html><body>Bad Gateway</body></html>"}
	if !IsRetryableError(err) {
		t.Error("IsRetryableError(HTML body 422) = false, want true")
	}
}

func TestIsRetryableError_AnthropicOverloaded(t *testing.T) {
	err := &UpstreamError{
		Code: 529,
		Body: `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
	}
	if !IsRetryableError(err) {
		t.Error("IsRetryableError(Anthropic overloaded_error) = false, want true")
	}
}

func TestIsRetryableError_OpenAIRateLimit(t *testing.T) {
	err := &UpstreamError{
		Code: 429,
		Body: `{"error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`,
	}
	if !IsRetryableError(err) {
		t.Error("IsRetryableError(OpenAI rate_limit_error) = false, want true")
	}
}
