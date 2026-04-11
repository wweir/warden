package httpmw

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/wweir/warden/config"
	requestctxpkg "github.com/wweir/warden/internal/gateway/requestctx"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
)

type Middleware interface {
	Process(next http.Handler) http.Handler
}

type Recovery struct{}

func (m *Recovery) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("Panic recovered", "error", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "Internal server error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type CORS struct{}

func (c *CORS) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func Chain(middlewares ...Middleware) Middleware {
	return &ChainMiddleware{middlewares: middlewares}
}

type ChainMiddleware struct {
	middlewares []Middleware
}

func (c *ChainMiddleware) Process(next http.Handler) http.Handler {
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		next = c.middlewares[i].Process(next)
	}
	return next
}

type APIKeyAuth struct {
	Cfg *config.ConfigStruct
}

func (m *APIKeyAuth) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := matchedRouteConfig(m, r.URL.Path)
		if route == nil || len(route.APIKeys) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		name, ok := authenticateAPIKey(route.APIKeys, r.Header)
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

		sanitized := r.Clone(requestctxpkg.WithAPIKeyName(r.Context(), name))
		sanitized.Header = r.Header.Clone()
		upstreampkg.RemoveClientCredentialHeaders(sanitized.Header)
		next.ServeHTTP(w, sanitized)
	})
}

func matchedRouteConfig(m *APIKeyAuth, path string) *config.RouteConfig {
	if m == nil || m.Cfg == nil {
		return nil
	}
	var (
		matched    *config.RouteConfig
		matchedLen int
	)
	for prefix, route := range m.Cfg.Route {
		if !strings.HasPrefix(path, prefix+"/") {
			continue
		}
		if len(prefix) > matchedLen {
			matched = route
			matchedLen = len(prefix)
		}
	}
	return matched
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
