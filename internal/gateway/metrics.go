package gateway

import (
	"net/http"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	requestctxpkg "github.com/wweir/warden/internal/gateway/requestctx"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
	tokenusagepkg "github.com/wweir/warden/internal/gateway/tokenusage"
)

// RecordFailoverMetric records a failover event in Prometheus metrics.
func (g *Gateway) RecordFailoverMetric(labels telemetrypkg.Labels) {
	telemetrypkg.RecordFailoverMetric(labels)
}

// RecordStreamErrorMetric records a stream error in Prometheus metrics.
func (g *Gateway) RecordStreamErrorMetric(labels telemetrypkg.Labels, phase string) {
	telemetrypkg.RecordStreamErrorMetric(labels, phase)
}

// RecordTokenMetrics records token usage metrics for a request.
func (g *Gateway) RecordTokenMetrics(labels telemetrypkg.Labels, usage tokenusagepkg.Observation, durationMs int64) {
	telemetrypkg.RecordTokenMetrics(labels, usage, durationMs, g.outputRates, time.Now())
}

// RecordTTFTMetric records streaming time-to-first-token in milliseconds.
func (g *Gateway) RecordTTFTMetric(labels telemetrypkg.Labels, ttft time.Duration) {
	telemetrypkg.RecordTTFTMetric(labels, ttft)
}

// PromMiddleware is the middleware that records Prometheus metrics.
type PromMiddleware struct {
	gateway *Gateway
}

// Process implements the Middleware interface.
func (m *PromMiddleware) Process(next http.Handler) http.Handler {
	return m.gateway.promMiddleware(next)
}

type promResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *promResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *promResponseWriter) StatusCode() int {
	if w.statusCode == 0 {
		return http.StatusOK
	}
	return w.statusCode
}

func (w *promResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// promMiddleware records metrics for each request.
// It reads route/provider info from response headers set by business handlers.
func (g *Gateway) promMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &promResponseWriter{ResponseWriter: w}

		next.ServeHTTP(wrapped, r)

		if strings.HasPrefix(r.URL.Path, "/_admin") || r.URL.Path == "/metrics" {
			return
		}

		labels := telemetrypkg.MetricLabelsFromHeader(wrapped.Header())
		labels.APIKey = requestctxpkg.APIKeyNameFromContext(r.Context())
		if labels.Route == "" {
			return
		}

		duration := time.Since(start)
		success := wrapped.StatusCode() < 500
		telemetrypkg.RecordRequestMetrics(labels, success, duration)
	})
}

// RegisterMetricsRoutes registers the /metrics endpoint.
func (g *Gateway) RegisterMetricsRoutes(router interface {
	Handle(method, path string, handle httprouter.Handle)
}) {
	router.Handle(http.MethodGet, "/metrics", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		telemetrypkg.UpdateProviderMetrics(g.selector.ProviderStatuses())
		telemetrypkg.MetricsHandler().ServeHTTP(w, r)
	})
}
