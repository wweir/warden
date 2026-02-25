package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
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

// IsRetryable determines whether this error warrants failover to another provider.
//
// Retryable by HTTP status code:
//   - 403: forbidden — provider may reject specific models/keys, another provider may accept
//   - 429: rate limit / quota exhausted — different provider may have capacity
//   - 500+: server errors, overloaded (including 502/503/504, Cloudflare 520-524, Anthropic 529)
//
// Retryable by response body content (for 4xx that are actually proxy/capacity issues):
//   - Non-JSON body (HTML error pages from nginx/Cloudflare) on any error status
//   - Anthropic: overloaded_error, api_error, rate_limit_error
//   - OpenAI: server_error, rate_limit_error, insufficient_quota, model_not_found
func (e *UpstreamError) IsRetryable() bool {
	// 5xx, 429 and 403 are always retryable
	if e.Code >= 500 || e.Code == 400 || e.Code == 429 || e.Code == 403 {
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
	return isRetryableByBody(body)
}

// isRetryableByBody checks parsed error body for known retryable error patterns.
func isRetryableByBody(body string) bool {
	// try Anthropic format: {"type":"error","error":{"type":"...", ...}}
	var anthropic struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &anthropic) == nil && anthropic.Error.Type != "" {
		switch anthropic.Error.Type {
		case "overloaded_error", "api_error", "rate_limit_error":
			return true
		}
	}

	// try OpenAI format: {"error":{"type":"...", "code":"..."}}
	var openai struct {
		Error struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &openai) == nil {
		switch openai.Error.Type {
		case "server_error", "rate_limit_error", "insufficient_quota":
			return true
		}
		switch openai.Error.Code {
		case "server_error", "rate_limit", "insufficient_quota", "model_not_found":
			return true
		}
	}

	return false
}

// ErrProviderNotFound is returned when no available provider is found for a route.
var ErrProviderNotFound = fmt.Errorf("provider not found")
