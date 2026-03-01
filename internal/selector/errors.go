package selector

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

// UpstreamError represents an HTTP error from the upstream LLM API.
type UpstreamError struct {
	Code int
	Body string
}

func (e *UpstreamError) Error() string {
	body := e.Body
	if len(body) > 200 {
		body = body[:200] + "..."
	}
	return fmt.Sprintf("upstream error: %d %s", e.Code, body)
}

// IsAuthError returns true if this is a 401 Unauthorized error.
func (e *UpstreamError) IsAuthError() bool {
	return e.Code == http.StatusUnauthorized
}

// IsRetryable determines whether this error warrants failover to another provider.
//
// Retryable by HTTP status code:
//   - 400: bad request — some providers reject valid requests (e.g. unsupported params), another may accept
//   - 403: forbidden — provider may reject specific models/keys, another provider may accept
//   - 404: endpoint not found — provider does not support this API (e.g. /responses)
//   - 429: rate limit / quota exhausted — different provider may have capacity
//   - 500+: server errors, overloaded (including 502/503/504, Cloudflare 520-524, Anthropic 529)
//
// Retryable by response body content (for 4xx that are actually proxy/capacity issues):
//   - Non-JSON body (HTML error pages from nginx/Cloudflare) on any error status
//   - Anthropic: overloaded_error, api_error, rate_limit_error
//   - OpenAI: server_error, rate_limit_error, insufficient_quota, model_not_found
func (e *UpstreamError) IsRetryable() bool {
	// 5xx, 429, 404, 403 and 400 are always retryable
	if e.Code >= 500 || e.Code == 400 || e.Code == 429 || e.Code == 404 || e.Code == 403 {
		return true
	}

	// non-JSON body (HTML from proxy layer like nginx/Cloudflare) on error status
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return false
	}
	if body[0] == '<' || strings.HasPrefix(body, "<!DOCTYPE") {
		return true
	}

	// parse JSON error body for provider-specific retryable error types
	return IsRetryableByBody(body)
}

// IsRetryableByBody checks parsed error body for known retryable error patterns.
func IsRetryableByBody(body string) bool {
	errType := gjson.Get(body, "error.type").String()
	if errType != "" {
		switch errType {
		case "overloaded_error", "api_error", "rate_limit_error", "new_api_error",
			"server_error", "insufficient_quota":
			return true
		}
	}
	errCode := gjson.Get(body, "error.code").String()
	switch errCode {
	case "server_error", "rate_limit", "insufficient_quota", "model_not_found":
		return true
	}
	return false
}

// ParseErrorBody checks if a response body contains an error object.
// Returns the error type and message if found, empty strings otherwise.
// Supports both Anthropic and OpenAI error formats.
func ParseErrorBody(body string) (errorType, errorMsg string) {
	body = strings.TrimSpace(body)
	if body == "" || body[0] != '{' {
		return "", ""
	}

	// Anthropic: {"type":"error","error":{"type":"...", "message":"..."}}
	if gjson.Get(body, "type").String() == "error" {
		errType := gjson.Get(body, "error.type").String()
		if errType != "" {
			return errType, gjson.Get(body, "error.message").String()
		}
	}

	// OpenAI: {"error":{"type":"...", "message":"..."}}
	errType := gjson.Get(body, "error.type").String()
	if errType != "" {
		return errType, gjson.Get(body, "error.message").String()
	}

	return "", ""
}

// IsRetryableError determines if an error should trigger failover to another provider.
// It handles three categories:
//  1. UpstreamError: uses UpstreamError.IsRetryable()
//  2. Network/timeout errors: always retryable
//  3. Client cancellation (context.Canceled): not retryable
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Client cancellation is not retryable
	if err == context.Canceled {
		return false
	}

	// UpstreamError: use its IsRetryable method
	if ue, ok := err.(*UpstreamError); ok {
		return ue.IsRetryable()
	}

	// Network and timeout errors are retryable
	if isNetworkError(err) {
		return true
	}

	// Unknown error types: conservatively not retryable
	return false
}

// isNetworkError checks if the error is a network-level failure.
func isNetworkError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

// ErrProviderNotFound is returned when no available provider is found for a route.
var ErrProviderNotFound = fmt.Errorf("provider not found")
