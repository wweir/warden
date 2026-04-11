package telemetry

import (
	"math"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	tokenusagepkg "github.com/wweir/warden/internal/gateway/tokenusage"
	sel "github.com/wweir/warden/internal/selector"
)

var (
	RouteRequestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "warden_route_requests_total", Help: "Total number of requests processed by route model"},
		[]string{"route", "protocol", "route_model", "matched_pattern", "endpoint", "status"},
	)
	ProviderRequestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "warden_provider_requests_total", Help: "Total number of requests processed by provider model"},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "endpoint", "status"},
	)
	APIKeyRequestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "warden_apikey_requests_total", Help: "Total number of requests processed by client API key"},
		[]string{"api_key", "route", "protocol", "route_model", "matched_pattern", "endpoint", "status"},
	)
	RouteRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "warden_route_request_duration_ms", Help: "Route-model request latency in milliseconds", Buckets: []float64{50, 100, 250, 500, 1000, 2500, 5000, 10000}},
		[]string{"route", "protocol", "route_model", "matched_pattern", "endpoint"},
	)
	ProviderRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "warden_request_duration_ms", Help: "Provider-model request latency in milliseconds", Buckets: []float64{50, 100, 250, 500, 1000, 2500, 5000, 10000}},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "endpoint"},
	)
	ProviderHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "warden_provider_health", Help: "Provider health status (consecutive failures)"},
		[]string{"provider"},
	)
	ProviderSuppressed = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "warden_provider_suppressed", Help: "Whether provider is suppressed (1 = suppressed, 0 = available)"},
		[]string{"provider"},
	)
	RouteTokenCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "warden_route_tokens_total", Help: "Total tokens processed by route model"},
		[]string{"route", "protocol", "route_model", "matched_pattern", "type"},
	)
	ProviderTokenCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "warden_provider_tokens_total", Help: "Total tokens processed by provider model"},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "type"},
	)
	APIKeyTokenCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "warden_apikey_tokens_total", Help: "Total tokens processed by client API key"},
		[]string{"api_key", "route", "protocol", "route_model", "matched_pattern", "type"},
	)
	RouteTokenObservationCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "warden_route_token_observations_total", Help: "Token usage observation coverage by route model"},
		[]string{"route", "protocol", "route_model", "matched_pattern", "endpoint", "completeness", "source"},
	)
	ProviderTokenObservationCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "warden_provider_token_observations_total", Help: "Token usage observation coverage by provider model"},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "endpoint", "completeness", "source"},
	)
	APIKeyTokenObservationCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "warden_apikey_token_observations_total", Help: "Token usage observation coverage by client API key"},
		[]string{"api_key", "route", "protocol", "route_model", "matched_pattern", "endpoint", "completeness", "source"},
	)
	RouteTokenRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "warden_route_token_rate", Help: "Tokens per second for the last request by route model"},
		[]string{"route", "protocol", "route_model", "matched_pattern", "endpoint", "type"},
	)
	ProviderTokenRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "warden_provider_token_rate", Help: "Tokens per second for the last request by provider model"},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "endpoint", "type"},
	)
	RouteStreamTTFT = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "warden_route_stream_ttft_ms", Help: "Streaming time-to-first-token in milliseconds by route model", Buckets: []float64{50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000}},
		[]string{"route", "protocol", "route_model", "matched_pattern", "endpoint"},
	)
	ProviderStreamTTFT = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "warden_stream_ttft_ms", Help: "Streaming time-to-first-token in milliseconds by provider model", Buckets: []float64{50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000}},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "endpoint"},
	)
	RouteCompletionThroughput = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "warden_route_completion_throughput_tps", Help: "Completion token throughput by route model", Buckets: []float64{1, 2, 5, 10, 20, 40, 80, 160, 320, 640, 1280}},
		[]string{"route", "protocol", "route_model", "matched_pattern", "endpoint"},
	)
	ProviderCompletionThroughput = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "warden_completion_throughput_tps", Help: "Completion token throughput in tokens per second by provider model", Buckets: []float64{1, 2, 5, 10, 20, 40, 80, 160, 320, 640, 1280}},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "endpoint"},
	)
	RouteFailovers = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "warden_route_failovers_total", Help: "Total number of failover events by route model"},
		[]string{"route", "protocol", "route_model", "matched_pattern"},
	)
	ProviderFailovers = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "warden_provider_failovers_total", Help: "Total number of failover events triggered by provider model"},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern"},
	)
	RouteStreamErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "warden_route_stream_errors_total", Help: "Total stream errors by route model and phase"},
		[]string{"route", "protocol", "route_model", "matched_pattern", "phase"},
	)
	ProviderStreamErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "warden_provider_stream_errors_total", Help: "Total stream errors by provider model and phase (pre_stream, in_stream)"},
		[]string{"provider", "provider_model", "route", "route_model", "matched_pattern", "phase"},
	)
	ProviderSuccessRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "warden_provider_success_rate", Help: "Provider success rate (0-100) from sliding window"},
		[]string{"provider"},
	)
	ProviderAvgLatency = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "warden_provider_avg_latency_ms", Help: "Provider average latency in milliseconds from sliding window"},
		[]string{"provider"},
	)
)

func init() {
	prometheus.MustRegister(
		RouteRequestCounter, ProviderRequestCounter, APIKeyRequestCounter,
		RouteRequestDuration, ProviderRequestDuration,
		ProviderHealth, ProviderSuppressed,
		RouteTokenCounter, ProviderTokenCounter, APIKeyTokenCounter,
		RouteTokenObservationCounter, ProviderTokenObservationCounter, APIKeyTokenObservationCounter,
		RouteTokenRate, ProviderTokenRate,
		RouteStreamTTFT, ProviderStreamTTFT,
		RouteCompletionThroughput, ProviderCompletionThroughput,
		RouteFailovers, ProviderFailovers,
		RouteStreamErrors, ProviderStreamErrors,
		ProviderSuccessRate, ProviderAvgLatency,
	)
}

func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

func RecordRequestMetrics(labels Labels, success bool, duration time.Duration) {
	status := "success"
	if !success {
		status = "failure"
	}
	RouteRequestCounter.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, labels.Endpoint, status).Inc()
	ProviderRequestCounter.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, labels.Endpoint, status).Inc()
	if labels.APIKey != "" {
		APIKeyRequestCounter.WithLabelValues(labels.APIKey, labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, labels.Endpoint, status).Inc()
	}
	RouteRequestDuration.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, labels.Endpoint).Observe(float64(duration.Milliseconds()))
	ProviderRequestDuration.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, labels.Endpoint).Observe(float64(duration.Milliseconds()))
}

func UpdateProviderMetrics(statuses []sel.ProviderStatus) {
	for _, s := range statuses {
		ProviderHealth.WithLabelValues(s.Name).Set(float64(s.ConsecutiveFailures))
		suppressed := 0.0
		if s.Suppressed {
			suppressed = 1.0
		}
		ProviderSuppressed.WithLabelValues(s.Name).Set(suppressed)
		if s.TotalRequests > 0 {
			successRate := float64(s.SuccessCount) / float64(s.TotalRequests) * 100
			ProviderSuccessRate.WithLabelValues(s.Name).Set(successRate)
		} else {
			ProviderSuccessRate.WithLabelValues(s.Name).Set(100)
		}
		ProviderAvgLatency.WithLabelValues(s.Name).Set(s.AvgLatencyMs)
	}
}

func RecordFailoverMetric(labels Labels) {
	RouteFailovers.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern).Inc()
	ProviderFailovers.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern).Inc()
}

func RecordStreamErrorMetric(labels Labels, phase string) {
	RouteStreamErrors.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, phase).Inc()
	ProviderStreamErrors.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, phase).Inc()
}

func RecordTokenMetrics(labels Labels, usage tokenusagepkg.Observation, durationMs int64, outputRates *OutputRateTracker, now time.Time) {
	RouteTokenObservationCounter.WithLabelValues(
		labels.Route,
		labels.Protocol,
		labels.RouteModel,
		labels.MatchedPattern,
		labels.Endpoint,
		usage.CompletenessLabel(),
		usage.SourceLabel(),
	).Inc()
	ProviderTokenObservationCounter.WithLabelValues(
		labels.Provider,
		labels.ProviderModel,
		labels.Route,
		labels.RouteModel,
		labels.MatchedPattern,
		labels.Endpoint,
		usage.CompletenessLabel(),
		usage.SourceLabel(),
	).Inc()
	if labels.APIKey != "" {
		APIKeyTokenObservationCounter.WithLabelValues(
			labels.APIKey,
			labels.Route,
			labels.Protocol,
			labels.RouteModel,
			labels.MatchedPattern,
			labels.Endpoint,
			usage.CompletenessLabel(),
			usage.SourceLabel(),
		).Inc()
	}
	if !usage.IsExact() {
		return
	}

	recordTokenType := func(count int64, typ string) {
		if count <= 0 {
			return
		}
		RouteTokenCounter.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, typ).Add(float64(count))
		ProviderTokenCounter.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, typ).Add(float64(count))
		if labels.APIKey != "" {
			APIKeyTokenCounter.WithLabelValues(labels.APIKey, labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, typ).Add(float64(count))
		}
		if durationMs > 0 {
			value := float64(count) / (float64(durationMs) / 1000.0)
			RouteTokenRate.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, labels.Endpoint, typ).Set(value)
			ProviderTokenRate.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, labels.Endpoint, typ).Set(value)
			if outputRates != nil {
				outputRates.Record(labels, typ, value, now)
			}
		}
	}
	recordTokenType(usage.PromptTokens, "prompt")
	recordTokenType(usage.CompletionTokens, "completion")
	if usage.CompletionTokens > 0 && durationMs > 0 {
		value := float64(usage.CompletionTokens) / (float64(durationMs) / 1000.0)
		RouteCompletionThroughput.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, labels.Endpoint).Observe(value)
		ProviderCompletionThroughput.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, labels.Endpoint).Observe(value)
	}
}

func RecordTTFTMetric(labels Labels, ttft time.Duration) {
	if ttft <= 0 {
		return
	}
	RouteStreamTTFT.WithLabelValues(labels.Route, labels.Protocol, labels.RouteModel, labels.MatchedPattern, labels.Endpoint).Observe(float64(ttft.Milliseconds()))
	ProviderStreamTTFT.WithLabelValues(labels.Provider, labels.ProviderModel, labels.Route, labels.RouteModel, labels.MatchedPattern, labels.Endpoint).Observe(float64(ttft.Milliseconds()))
}

func CollectMetrics(c prometheus.Collector) []*dto.Metric {
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

func HistogramQuantile(quantile float64, buckets []*dto.Bucket) float64 {
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
