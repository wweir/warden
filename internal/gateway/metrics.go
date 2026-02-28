package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
)

var (
	// requestCounter counts total requests by route, provider, and status.
	requestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warden_requests_total",
			Help: "Total number of requests processed",
		},
		[]string{"route", "provider", "status"},
	)

	// requestDuration tracks request latency distribution.
	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "warden_request_duration_ms",
			Help:    "Request latency in milliseconds",
			Buckets: []float64{50, 100, 250, 500, 1000, 2500, 5000, 10000},
		},
		[]string{"route", "provider"},
	)

	// providerHealth tracks provider consecutive failures.
	providerHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warden_provider_health",
			Help: "Provider health status (consecutive failures)",
		},
		[]string{"provider"},
	)

	// providerSuppressed indicates if a provider is currently suppressed.
	providerSuppressed = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warden_provider_suppressed",
			Help: "Whether provider is suppressed (1 = suppressed, 0 = available)",
		},
		[]string{"provider"},
	)

	// tokenCounter tracks total tokens by provider and model.
	tokenCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warden_tokens_total",
			Help: "Total tokens processed by provider and model",
		},
		[]string{"provider", "model", "type"}, // type: prompt, completion
	)

	// tokenRate tracks tokens per second by provider and model.
	tokenRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warden_token_rate",
			Help: "Tokens per second for the last request by provider and model",
		},
		[]string{"provider", "model", "type"}, // type: prompt, completion
	)

	// providerFailovers counts failover events per provider.
	providerFailovers = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warden_provider_failovers_total",
			Help: "Total number of failover events triggered by provider",
		},
		[]string{"provider"},
	)

	// providerStreamErrors counts stream errors by provider and phase.
	providerStreamErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warden_provider_stream_errors_total",
			Help: "Total stream errors by provider and phase (pre_stream, in_stream)",
		},
		[]string{"provider", "phase"},
	)

	// providerSuccessRate tracks the success rate gauge per provider.
	providerSuccessRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warden_provider_success_rate",
			Help: "Provider success rate (0-100) from sliding window",
		},
		[]string{"provider"},
	)

	// providerAvgLatency tracks the average latency gauge per provider.
	providerAvgLatency = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warden_provider_avg_latency_ms",
			Help: "Provider average latency in milliseconds from sliding window",
		},
		[]string{"provider"},
	)
)

func init() {
	prometheus.MustRegister(
		requestCounter, requestDuration,
		providerHealth, providerSuppressed,
		tokenCounter, tokenRate,
		providerFailovers, providerStreamErrors,
		providerSuccessRate, providerAvgLatency,
	)
}

// MetricsHandler returns an HTTP handler for the /metrics endpoint.
func (g *Gateway) MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// recordRequestMetrics records request metrics.
func (g *Gateway) recordRequestMetrics(route, provider string, success bool, duration time.Duration) {
	status := "success"
	if !success {
		status = "failure"
	}
	requestCounter.WithLabelValues(route, provider, status).Inc()
	requestDuration.WithLabelValues(route, provider).Observe(float64(duration.Milliseconds()))
}

// updateProviderMetrics updates provider health metrics.
func (g *Gateway) updateProviderMetrics(cfg *config.ConfigStruct) {
	statuses := g.selector.ProviderStatuses()
	for _, s := range statuses {
		providerHealth.WithLabelValues(s.Name).Set(float64(s.ConsecutiveFailures))
		suppressed := 0.0
		if s.Suppressed {
			suppressed = 1.0
		}
		providerSuppressed.WithLabelValues(s.Name).Set(suppressed)

		// New metrics
		if s.TotalRequests > 0 {
			successRate := float64(s.SuccessCount) / float64(s.TotalRequests) * 100
			providerSuccessRate.WithLabelValues(s.Name).Set(successRate)
		} else {
			providerSuccessRate.WithLabelValues(s.Name).Set(100)
		}
		providerAvgLatency.WithLabelValues(s.Name).Set(s.AvgLatencyMs)
	}
}

// RecordFailoverMetric records a failover event in Prometheus metrics.
func (g *Gateway) RecordFailoverMetric(provider string) {
	providerFailovers.WithLabelValues(provider).Inc()
}

// RecordStreamErrorMetric records a stream error in Prometheus metrics.
func (g *Gateway) RecordStreamErrorMetric(provider, phase string) {
	providerStreamErrors.WithLabelValues(provider, phase).Inc()
}

// TokenUsage represents extracted token usage from a response.
type TokenUsage struct {
	PromptTokens     int64
	CompletionTokens int64
}

// RecordTokenMetrics records token usage metrics for a request.
// durationMs is the request duration in milliseconds, used to calculate token/s rate.
func (g *Gateway) RecordTokenMetrics(provider, model string, usage TokenUsage, durationMs int64) {
	recordTokenType := func(count int64, typ string) {
		if count <= 0 {
			return
		}
		tokenCounter.WithLabelValues(provider, model, typ).Add(float64(count))
		if durationMs > 0 {
			tokenRate.WithLabelValues(provider, model, typ).Set(float64(count) / (float64(durationMs) / 1000.0))
		}
	}
	recordTokenType(usage.PromptTokens, "prompt")
	recordTokenType(usage.CompletionTokens, "completion")
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

// collectMetrics drains a prometheus.Collector into a slice of *dto.Metric.
func collectMetrics(c prometheus.Collector) []*dto.Metric {
	ch := make(chan prometheus.Metric, 200)
	go func() {
		c.Collect(ch)
		close(ch)
	}()
	var result []*dto.Metric
	for m := range ch {
		met := &dto.Metric{}
		if err := m.Write(met); err == nil {
			result = append(result, met)
		}
	}
	return result
}

// promMiddleware records metrics for each request.
// It reads route/provider info from response headers set by business handlers.
func (g *Gateway) promMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &promResponseWriter{ResponseWriter: w}

		next.ServeHTTP(wrapped, r)

		// Skip metrics for non-business routes (admin, metrics endpoints)
		if strings.HasPrefix(r.URL.Path, "/_admin") || r.URL.Path == "/metrics" {
			return
		}

		// Read route/provider from headers set by business handlers
		route := wrapped.Header().Get("X-Route")
		provider := wrapped.Header().Get("X-Provider")
		if route == "" {
			return
		}

		duration := time.Since(start)
		success := wrapped.StatusCode() < 500

		g.recordRequestMetrics(route, provider, success, duration)
	})
}

// RegisterMetricsRoutes registers the /metrics endpoint.
func (g *Gateway) RegisterMetricsRoutes(router interface {
	Handle(method, path string, handle httprouter.Handle)
}) {
	router.Handle(http.MethodGet, "/metrics", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		g.updateProviderMetrics(g.cfg)
		g.MetricsHandler().ServeHTTP(w, r)
	})
}

// ExtractTokenUsage extracts token usage from a response body.
// Supports OpenAI Chat Completions, OpenAI Responses API, and Anthropic Messages API formats.
// Returns zero values if no usage information is found.
func ExtractTokenUsage(respBody json.RawMessage) TokenUsage {
	if len(respBody) == 0 {
		return TokenUsage{}
	}

	jsonStr := string(respBody)

	// Try OpenAI Chat Completions format: usage.prompt_tokens, usage.completion_tokens
	usage := gjson.Get(jsonStr, "usage")
	if usage.Exists() && usage.IsObject() {
		promptTokens := usage.Get("prompt_tokens")
		completionTokens := usage.Get("completion_tokens")

		if promptTokens.Exists() || completionTokens.Exists() {
			return TokenUsage{
				PromptTokens:     int64(promptTokens.Int()),
				CompletionTokens: int64(completionTokens.Int()),
			}
		}
	}

	// Try Anthropic format: usage.input_tokens, usage.output_tokens
	inputTokens := gjson.Get(jsonStr, "usage.input_tokens")
	outputTokens := gjson.Get(jsonStr, "usage.output_tokens")

	if inputTokens.Exists() || outputTokens.Exists() {
		return TokenUsage{
			PromptTokens:     int64(inputTokens.Int()),
			CompletionTokens: int64(outputTokens.Int()),
		}
	}

	return TokenUsage{}
}
