package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Middleware 是中间件接口
type Middleware interface {
	Process(next http.Handler) http.Handler
}

// LoggingMiddleware 请求日志中间件
type LoggingMiddleware struct{}

func (m *LoggingMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Info("Request received",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
		)
		next.ServeHTTP(w, r)
	})
}

// RecoveryMiddleware 恢复中间件
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

// CORS 跨域中间件
type CORS struct {
	AllowOrigins []string
	AllowMethods []string
	AllowHeaders []string
}

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

// Chain 中间件链
func Chain(middlewares ...Middleware) Middleware {
	return &ChainMiddleware{middlewares}
}

type ChainMiddleware struct {
	middlewares []Middleware
}

func (c *ChainMiddleware) Process(next http.Handler) http.Handler {
	// 反向顺序应用中间件
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		next = c.middlewares[i].Process(next)
	}
	return next
}
