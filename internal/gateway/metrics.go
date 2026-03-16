package gateway

import (
	"encoding/json"
	"math"
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
	routeRequestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warden_route_requests_total",
			Help: "Total number of requests processed by route model",
		},
		[]string{"route", "protocol", "route_model", "matched_pattern", "endpoint", "status"},
	)

	providerRequestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warden_provider_requests_total",
			Help: "Total number of requests processed by provider model",
		},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "endpoint", "status"},
	)

	routeRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "warden_route_request_duration_ms",
			Help:    "Route-model request latency in milliseconds",
			Buckets: []float64{50, 100, 250, 500, 1000, 2500, 5000, 10000},
		},
		[]string{"route", "protocol", "route_model", "matched_pattern", "endpoint"},
	)

	providerRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "warden_request_duration_ms",
			Help:    "Provider-model request latency in milliseconds",
			Buckets: []float64{50, 100, 250, 500, 1000, 2500, 5000, 10000},
		},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "endpoint"},
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

	routeTokenCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warden_route_tokens_total",
			Help: "Total tokens processed by route model",
		},
		[]string{"route", "protocol", "route_model", "matched_pattern", "type"},
	)

	providerTokenCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warden_provider_tokens_total",
			Help: "Total tokens processed by provider model",
		},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "type"},
	)

	routeTokenRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warden_route_token_rate",
			Help: "Tokens per second for the last request by route model",
		},
		[]string{"route", "protocol", "route_model", "matched_pattern", "endpoint", "type"},
	)

	providerTokenRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warden_provider_token_rate",
			Help: "Tokens per second for the last request by provider model",
		},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "endpoint", "type"},
	)

	routeStreamTTFT = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "warden_route_stream_ttft_ms",
			Help:    "Streaming time-to-first-token in milliseconds by route model",
			Buckets: []float64{50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000},
		},
		[]string{"route", "protocol", "route_model", "matched_pattern", "endpoint"},
	)

	providerStreamTTFT = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "warden_stream_ttft_ms",
			Help:    "Streaming time-to-first-token in milliseconds by provider model",
			Buckets: []float64{50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000},
		},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "endpoint"},
	)

	routeCompletionThroughput = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "warden_route_completion_throughput_tps",
			Help:    "Completion token throughput by route model",
			Buckets: []float64{1, 2, 5, 10, 20, 40, 80, 160, 320, 640, 1280},
		},
		[]string{"route", "protocol", "route_model", "matched_pattern", "endpoint"},
	)

	providerCompletionThroughput = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "warden_completion_throughput_tps",
			Help:    "Completion token throughput in tokens per second by provider model",
			Buckets: []float64{1, 2, 5, 10, 20, 40, 80, 160, 320, 640, 1280},
		},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "endpoint"},
	)

	routeFailovers = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warden_route_failovers_total",
			Help: "Total number of failover events by route model",
		},
		[]string{"route", "protocol", "route_model", "matched_pattern"},
	)

	providerFailovers = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warden_provider_failovers_total",
			Help: "Total number of failover events triggered by provider model",
		},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern"},
	)

	routeStreamErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warden_route_stream_errors_total",
			Help: "Total stream errors by route model and phase",
		},
		[]string{"route", "protocol", "route_model", "matched_pattern", "phase"},
	)

	providerStreamErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warden_provider_stream_errors_total",
			Help: "Total stream errors by provider model and phase (pre_stream, in_stream)",
		},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "phase"},
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
		routeRequestCounter, providerRequestCounter,
		routeRequestDuration, providerRequestDuration,
		providerHealth, providerSuppressed,
		routeTokenCounter, providerTokenCounter,
		routeTokenRate, providerTokenRate,
		routeStreamTTFT, providerStreamTTFT,
		routeCompletionThroughput, providerCompletionThroughput,
		routeFailovers, providerFailovers,
		routeStreamErrors, providerStreamErrors,
		providerSuccessRate, providerAvgLatency,
	)
}

// MetricsHandler returns an HTTP handler for the /metrics endpoint.
func (g *Gateway) MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// recordRequestMetrics records request metrics.
func (g *Gateway) recordRequestMetrics(labels requestMetricLabels, success bool, duration time.Duration) {
	status := "success"
	if !success {
		status = "failure"
	}
	routeRequestCounter.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, labels.Endpoint, status).Inc()
	providerRequestCounter.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, labels.Endpoint, status).Inc()
	routeRequestDuration.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, labels.Endpoint).Observe(float64(duration.Milliseconds()))
	providerRequestDuration.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, labels.Endpoint).Observe(float64(duration.Milliseconds()))
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
func (g *Gateway) RecordFailoverMetric(labels requestMetricLabels) {
	routeFailovers.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern).Inc()
	providerFailovers.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern).Inc()
}

// RecordStreamErrorMetric records a stream error in Prometheus metrics.
func (g *Gateway) RecordStreamErrorMetric(labels requestMetricLabels, phase string) {
	routeStreamErrors.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, phase).Inc()
	providerStreamErrors.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, phase).Inc()
}

// TokenUsage represents extracted token usage from a response.
type TokenUsage struct {
	PromptTokens     int64
	CompletionTokens int64
}

// RecordTokenMetrics records token usage metrics for a request.
// durationMs is the request duration in milliseconds, used to calculate token/s rate.
func (g *Gateway) RecordTokenMetrics(labels requestMetricLabels, usage TokenUsage, durationMs int64) {
	recordTokenType := func(count int64, typ string) {
		if count <= 0 {
			return
		}
		routeTokenCounter.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, typ).Add(float64(count))
		providerTokenCounter.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, typ).Add(float64(count))
		if durationMs > 0 {
			value := float64(count) / (float64(durationMs) / 1000.0)
			routeTokenRate.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, labels.Endpoint, typ).Set(value)
			providerTokenRate.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, labels.Endpoint, typ).Set(value)
			if g.outputRates != nil {
				g.outputRates.Record(labels, typ, value, time.Now())
			}
		}
	}
	recordTokenType(usage.PromptTokens, "prompt")
	recordTokenType(usage.CompletionTokens, "completion")
	if usage.CompletionTokens > 0 && durationMs > 0 {
		routeCompletionThroughput.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, labels.Endpoint).
			Observe(float64(usage.CompletionTokens) / (float64(durationMs) / 1000.0))
		providerCompletionThroughput.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, labels.Endpoint).
			Observe(float64(usage.CompletionTokens) / (float64(durationMs) / 1000.0))
	}
}

// RecordTTFTMetric records streaming time-to-first-token in milliseconds.
func (g *Gateway) RecordTTFTMetric(labels requestMetricLabels, ttft time.Duration) {
	if ttft <= 0 {
		return
	}
	routeStreamTTFT.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, labels.Endpoint).Observe(float64(ttft.Milliseconds()))
	providerStreamTTFT.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, labels.Endpoint).Observe(float64(ttft.Milliseconds()))
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

func histogramQuantile(quantile float64, buckets []*dto.Bucket) float64 {
	if quantile <= 0 || len(buckets) == 0 {
		return 0
	}
	total := float64(buckets[len(buckets)-1].GetCumulativeCount())
	if total <= 0 {
		return 0
	}
	rank := quantile * total
	prevUpper := 0.0
	prevCount := 0.0
	for _, b := range buckets {
		upper := b.GetUpperBound()
		cum := float64(b.GetCumulativeCount())
		if cum >= rank {
			bucketCount := cum - prevCount
			if bucketCount <= 0 {
				if math.IsInf(upper, 1) {
					return prevUpper
				}
				return upper
			}
			pos := (rank - prevCount) / bucketCount
			if pos < 0 {
				pos = 0
			}
			if pos > 1 {
				pos = 1
			}
			if math.IsInf(upper, 1) {
				return prevUpper
			}
			return prevUpper + (upper-prevUpper)*pos
		}
		prevUpper = upper
		prevCount = cum
	}
	return prevUpper
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

		// Read route/provider/model/endpoint from headers set by business handlers
		labels := metricLabelsFromHeader(wrapped.Header())
		if labels.Route == "" {
			return
		}

		duration := time.Since(start)
		success := wrapped.StatusCode() < 500

		g.recordRequestMetrics(labels, success, duration)
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
