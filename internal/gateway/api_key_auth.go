package gateway

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/wweir/warden/config"
)

type APIKeyAuthMiddleware struct {
	cfg *config.ConfigStruct
}

func (m *APIKeyAuthMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m == nil || m.cfg == nil || len(m.cfg.APIKeys) == 0 || !requiresAPIKeyAuth(m.cfg, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		name, ok := authenticateAPIKey(m.cfg.APIKeys, r.Header)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("WWW-Authenticate", `Bearer realm="Warden"`)
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{
					"message": "invalid api key",
					"type":    "invalid_request_error",
				},
			})
			return
		}

		sanitized := r.Clone(withAPIKeyName(r.Context(), name))
		sanitized.Header = r.Header.Clone()
		removeClientCredentialHeaders(sanitized.Header)
		next.ServeHTTP(w, sanitized)
	})
}

func requiresAPIKeyAuth(cfg *config.ConfigStruct, path string) bool {
	if cfg == nil {
		return false
	}
	for prefix := range cfg.Route {
		if strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func authenticateAPIKey(keys map[string]config.SecretString, headers http.Header) (string, bool) {
	token := extractAPIKey(headers)
	if token == "" {
		return "", false
	}

	var matched string
	for name, key := range keys {
		if subtle.ConstantTimeCompare([]byte(token), []byte(key.Value())) == 1 {
			matched = name
		}
	}
	if matched == "" {
		return "", false
	}
	return matched, true
}

func extractAPIKey(headers http.Header) string {
	if headers == nil {
		return ""
	}

	auth := strings.TrimSpace(headers.Get("Authorization"))
	if auth != "" {
		parts := strings.Fields(auth)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}

	if token := strings.TrimSpace(headers.Get("Api-Key")); token != "" {
		return token
	}
	if token := strings.TrimSpace(headers.Get("X-Api-Key")); token != "" {
		return token
	}
	return ""
}
