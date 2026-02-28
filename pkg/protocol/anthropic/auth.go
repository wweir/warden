package anthropic

import "net/http"

// SetAuthHeaders sets Anthropic-specific authentication headers.
// Also sets Bearer auth for proxies that expose OpenAI-compatible /v1/models.
func SetAuthHeaders(h http.Header, apiKey string) {
	h.Set("x-api-key", apiKey)
	h.Set("anthropic-version", "2023-06-01")
	h.Set("Authorization", "Bearer "+apiKey)
}
