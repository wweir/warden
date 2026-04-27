package selector

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestIsRetryableError_Nil(t *testing.T) {
	if IsRetryableError(nil) {
		t.Error("IsRetryableError(nil) = true, want false")
	}
}

func TestIsRetryableError_ContextTermination(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "canceled", err: context.Canceled},
		{name: "wrapped canceled", err: fmt.Errorf("send request: %w", context.Canceled)},
		{name: "deadline exceeded", err: context.DeadlineExceeded},
		{name: "wrapped deadline exceeded", err: fmt.Errorf("send request: %w", context.DeadlineExceeded)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if IsRetryableError(tt.err) {
				t.Errorf("IsRetryableError(%v) = true, want false", tt.err)
			}
		})
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
		{400, false},
		{401, true},
		{422, false},
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
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("connection refused")}
	if !IsRetryableError(opErr) {
		t.Error("IsRetryableError(net.OpError) = false, want true")
	}

	dnsErr := &net.DNSError{Err: "no such host", Name: "example.com"}
	if !IsRetryableError(dnsErr) {
		t.Error("IsRetryableError(net.DNSError) = false, want true")
	}
}

func TestIsRetryableError_UnknownError(t *testing.T) {
	if IsRetryableError(fmt.Errorf("some random error")) {
		t.Error("IsRetryableError(generic error) = true, want false")
	}
}

func TestIsRetryableError_UpstreamHTMLBody(t *testing.T) {
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

func TestIsRetryableError_OpenAIModelNotFound(t *testing.T) {
	err := &UpstreamError{
		Code: 404,
		Body: `{"error":{"type":"invalid_request_error","code":"model_not_found","message":"The model does not exist"}}`,
	}
	if !IsRetryableError(err) {
		t.Error("IsRetryableError(OpenAI model_not_found) = false, want true")
	}
}

func TestIsRetryableError_BadRequestModelNotFound(t *testing.T) {
	err := &UpstreamError{
		Code: 400,
		Body: `{"error":{"type":"invalid_request_error","code":"model_not_found","message":"The model does not exist"}}`,
	}
	if !IsRetryableError(err) {
		t.Error("IsRetryableError(400 model_not_found) = false, want true")
	}
}

func TestIsRetryableError_BadRequestValidation(t *testing.T) {
	err := &UpstreamError{
		Code: 400,
		Body: `{"error":"'input_type' parameter is required for asymmetric models"}`,
	}
	if IsRetryableError(err) {
		t.Error("IsRetryableError(400 input_type validation error) = true, want false")
	}
}

func TestIsRetryableError_OpenAIInsufficientQuota(t *testing.T) {
	err := &UpstreamError{
		Code: 429,
		Body: `{"error":{"type":"insufficient_quota","message":"You exceeded your current quota"}}`,
	}
	if !IsRetryableError(err) {
		t.Error("IsRetryableError(OpenAI insufficient_quota) = false, want true")
	}
}

func TestIsRetryableError_OpenAIServerError(t *testing.T) {
	err := &UpstreamError{
		Code: 500,
		Body: `{"error":{"type":"server_error","message":"The server had an error"}}`,
	}
	if !IsRetryableError(err) {
		t.Error("IsRetryableError(OpenAI server_error) = false, want true")
	}
}

func TestIsRetryableError_AnthropicAPIError(t *testing.T) {
	err := &UpstreamError{
		Code: 500,
		Body: `{"type":"error","error":{"type":"api_error","message":"Internal server error"}}`,
	}
	if !IsRetryableError(err) {
		t.Error("IsRetryableError(Anthropic api_error) = false, want true")
	}
}

func TestIsRetryableError_AnthropicRateLimit(t *testing.T) {
	err := &UpstreamError{
		Code: 429,
		Body: `{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`,
	}
	if !IsRetryableError(err) {
		t.Error("IsRetryableError(Anthropic rate_limit_error) = false, want true")
	}
}

func TestIsRetryableError_KeyModelAccessDenied401(t *testing.T) {
	err := &UpstreamError{
		Code: 401,
		Body: `{"error":{"message":"key not allowed to access model","type":"key_model_access_denied","param":"model","code":"401"}}`,
	}
	if !IsRetryableError(err) {
		t.Error("IsRetryableError(key_model_access_denied 401) = false, want true")
	}
}

func TestIsRetryableError_NonRetryable4xx(t *testing.T) {
	cases := []struct {
		code int
		body string
	}{
		{422, `{"error":{"type":"invalid_request_error","message":"Unprocessable"}}`},
	}
	for _, c := range cases {
		err := &UpstreamError{Code: c.code, Body: c.body}
		if IsRetryableError(err) {
			t.Errorf("IsRetryableError(&UpstreamError{Code: %d}) = true, want false", c.code)
		}
	}
}

func TestIsRetryableError_NewAPIError(t *testing.T) {
	err := &UpstreamError{
		Code: 200,
		Body: `{"type":"error","error":{"type":"new_api_error","message":"model overloaded"}}`,
	}
	if !IsRetryableError(err) {
		t.Error("IsRetryableError(new_api_error) = false, want true")
	}
}

func TestParseErrorBody_Anthropic(t *testing.T) {
	body := `{"type":"error","error":{"type":"new_api_error","message":"model overloaded"}}`
	errType, errMsg := ParseErrorBody(body)
	if errType != "new_api_error" {
		t.Errorf("ParseErrorBody type = %q, want new_api_error", errType)
	}
	if errMsg != "model overloaded" {
		t.Errorf("ParseErrorBody message = %q, want model overloaded", errMsg)
	}
}

func TestParseErrorBody_OpenAI(t *testing.T) {
	body := `{"error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`
	errType, errMsg := ParseErrorBody(body)
	if errType != "rate_limit_error" {
		t.Errorf("ParseErrorBody type = %q, want rate_limit_error", errType)
	}
	if errMsg != "Rate limit exceeded" {
		t.Errorf("ParseErrorBody message = %q, want 'Rate limit exceeded'", errMsg)
	}
}

func TestParseErrorBody_NoError(t *testing.T) {
	body := `{"id":"chatcmpl-123","choices":[],"model":"gpt-4"}`
	errType, errMsg := ParseErrorBody(body)
	if errType != "" || errMsg != "" {
		t.Errorf("ParseErrorBody = (%q, %q), want empty strings", errType, errMsg)
	}
}

func TestParseErrorBody_EmptyBody(t *testing.T) {
	errType, errMsg := ParseErrorBody("")
	if errType != "" || errMsg != "" {
		t.Errorf("ParseErrorBody = (%q, %q), want empty strings", errType, errMsg)
	}
}

func TestFormatModelsFetchHTTPError_InternalErrorIsSanitized(t *testing.T) {
	msg := formatModelsFetchHTTPError(500, []byte(`{"error":{"message":"Panic detected, error: runtime error: index out of range [0] with length 0","type":"new_api_panic"}}`))
	if msg != "HTTP 500 upstream internal error" {
		t.Fatalf("formatModelsFetchHTTPError() = %q, want sanitized internal error", msg)
	}
	if strings.Contains(strings.ToLower(msg), "panic") {
		t.Fatalf("formatModelsFetchHTTPError() leaked panic detail: %q", msg)
	}
}

func TestFormatModelsFetchHTTPError_UsesParsedMessage(t *testing.T) {
	msg := formatModelsFetchHTTPError(403, []byte(`{"error":{"message":"invalid api key","type":"auth_error"}}`))
	if msg != "HTTP 403 invalid api key" {
		t.Fatalf("formatModelsFetchHTTPError() = %q, want parsed message", msg)
	}
}
