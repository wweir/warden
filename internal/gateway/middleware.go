package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Middleware is the middleware interface.
type Middleware interface {
	Process(next http.Handler) http.Handler
}

// RecoveryMiddleware recovers from panics and returns 500.
type RecoveryMiddleware struct{}

func (m *RecoveryMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("Panic recovered", "error", err)
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "Internal server error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// CORS handles Cross-Origin Resource Sharing headers.
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

// Chain composes multiple middlewares into one.
func Chain(middlewares ...Middleware) Middleware {
	return &ChainMiddleware{middlewares}
}

type ChainMiddleware struct {
	middlewares []Middleware
}

func (c *ChainMiddleware) Process(next http.Handler) http.Handler {
	// apply middlewares in reverse order
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		next = c.middlewares[i].Process(next)
	}
	return next
}
