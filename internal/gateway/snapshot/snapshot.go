package snapshot

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/wweir/warden/config"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
	sel "github.com/wweir/warden/internal/selector"
)

func CollectMetricsData(statuses []sel.ProviderStatus, outputRates *telemetrypkg.OutputRateTracker, dashboardStore *telemetrypkg.DashboardMetricsStore) map[string]any {
	telemetrypkg.UpdateProviderMetrics(statuses)

	type requestStat struct {
		Route          string `json:"route"`
		Protocol       string `json:"protocol,omitempty"`
		RouteModel     string `json:"route_model,omitempty"`
		MatchedPattern string `json:"matched_pattern,omitempty"`
		Provider       string `json:"provider,omitempty"`
		ProviderModel  string `json:"provider_model,omitempty"`
		Model          string `json:"model,omitempty"`
		Endpoint       string `json:"endpoint"`
		Status         string `json:"status"`
		Value          int    `json:"value"`
	}
	collectRequestStats := func(collector *prometheus.CounterVec) []requestStat {
		var rows []requestStat
		for _, met := range telemetrypkg.CollectMetrics(collector) {
			row := requestStat{Value: int(met.GetCounter().GetValue())}
			for _, l := range met.GetLabel() {
				switch l.GetName() {
				case "route":
					row.Route = l.GetValue()
				case "protocol":
					row.Protocol = l.GetValue()
				case "route_model":
					row.RouteModel = l.GetValue()
					row.Model = l.GetValue()
				case "matched_pattern":
					row.MatchedPattern = l.GetValue()
				case "provider":
					row.Provider = l.GetValue()
				case "provider_model":
					row.ProviderModel = l.GetValue()
					row.Model = l.GetValue()
				case "endpoint":
					row.Endpoint = l.GetValue()
				case "status":
					row.Status = l.GetValue()
				}
			}
			rows = append(rows, row)
		}
		return rows
	}

	type durationBucket struct {
		Route          string  `json:"route"`
		Protocol       string  `json:"protocol,omitempty"`
		RouteModel     string  `json:"route_model,omitempty"`
		MatchedPattern string  `json:"matched_pattern,omitempty"`
		Provider       string  `json:"provider,omitempty"`
		ProviderModel  string  `json:"provider_model,omitempty"`
		Model          string  `json:"model,omitempty"`
		Endpoint       string  `json:"endpoint"`
		Le             float64 `json:"le"`
		Value          int     `json:"value"`
	}
	collectDurationStats := func(collector *prometheus.HistogramVec) []durationBucket {
		var rows []durationBucket
		for _, met := range telemetrypkg.CollectMetrics(collector) {
			for _, b := range met.GetHistogram().GetBucket() {
				if b.GetUpperBound() == float64(1<<63-1) {
					continue
				}
				row := durationBucket{Le: b.GetUpperBound(), Value: int(b.GetCumulativeCount())}
				for _, l := range met.GetLabel() {
					switch l.GetName() {
					case "route":
						row.Route = l.GetValue()
					case "protocol":
						row.Protocol = l.GetValue()
					case "route_model":
						row.RouteModel = l.GetValue()
						row.Model = l.GetValue()
					case "matched_pattern":
						row.MatchedPattern = l.GetValue()
					case "provider":
						row.Provider = l.GetValue()
					case "provider_model":
						row.ProviderModel = l.GetValue()
						row.Model = l.GetValue()
					case "endpoint":
						row.Endpoint = l.GetValue()
					}
				}
				rows = append(rows, row)
			}
		}
		return rows
	}

	type tokenStat struct {
		Route          string  `json:"route,omitempty"`
		Protocol       string  `json:"protocol,omitempty"`
		RouteModel     string  `json:"route_model,omitempty"`
		MatchedPattern string  `json:"matched_pattern,omitempty"`
		Provider       string  `json:"provider,omitempty"`
		ProviderModel  string  `json:"provider_model,omitempty"`
		Model          string  `json:"model,omitempty"`
		Type           string  `json:"type"`
		Value          float64 `json:"value"`
	}
	collectTokenStats := func(collector *prometheus.CounterVec) []tokenStat {
		var rows []tokenStat
		for _, met := range telemetrypkg.CollectMetrics(collector) {
			row := tokenStat{Value: met.GetCounter().GetValue()}
			for _, l := range met.GetLabel() {
				switch l.GetName() {
				case "route":
					row.Route = l.GetValue()
				case "protocol":
					row.Protocol = l.GetValue()
				case "route_model":
					row.RouteModel = l.GetValue()
					row.Model = l.GetValue()
				case "matched_pattern":
					row.MatchedPattern = l.GetValue()
				case "provider":
					row.Provider = l.GetValue()
				case "provider_model":
					row.ProviderModel = l.GetValue()
					row.Model = l.GetValue()
				case "type":
					row.Type = l.GetValue()
				}
			}
			rows = append(rows, row)
		}
		return rows
	}

	type tokenRateStat struct {
		Route          string  `json:"route,omitempty"`
		Protocol       string  `json:"protocol,omitempty"`
		RouteModel     string  `json:"route_model,omitempty"`
		MatchedPattern string  `json:"matched_pattern,omitempty"`
		Provider       string  `json:"provider,omitempty"`
		ProviderModel  string  `json:"provider_model,omitempty"`
		Model          string  `json:"model,omitempty"`
		Endpoint       string  `json:"endpoint"`
		Type           string  `json:"type"`
		Value          float64 `json:"value"`
	}
	var providerRates []tokenRateStat
	routeRateMap := map[string]tokenRateStat{}
	if outputRates != nil {
		for _, entry := range outputRates.Snapshot(time.Now()) {
			providerRates = append(providerRates, tokenRateStat{
				Route:          entry.Route,
				Protocol:       entry.Protocol,
				RouteModel:     entry.RouteModel,
				MatchedPattern: entry.MatchedPattern,
				Provider:       entry.Provider,
				ProviderModel:  entry.ProviderModel,
				Model:          entry.ProviderModel,
				Endpoint:       entry.Endpoint,
				Type:           entry.Type,
				Value:          entry.Value,
			})
			key := entry.Route + "\x00" + entry.Protocol + "\x00" + entry.RouteModel + "\x00" + entry.MatchedPattern + "\x00" + entry.Endpoint + "\x00" + entry.Type
			row := routeRateMap[key]
			row.Route = entry.Route
			row.Protocol = entry.Protocol
			row.RouteModel = entry.RouteModel
			row.MatchedPattern = entry.MatchedPattern
			row.Model = entry.RouteModel
			row.Endpoint = entry.Endpoint
			row.Type = entry.Type
			row.Value += entry.Value
			routeRateMap[key] = row
		}
	}
	routeRates := make([]tokenRateStat, 0, len(routeRateMap))
	for _, row := range routeRateMap {
		routeRates = append(routeRates, row)
	}

	type quantileStat struct {
		Route          string  `json:"route,omitempty"`
		Protocol       string  `json:"protocol,omitempty"`
		RouteModel     string  `json:"route_model,omitempty"`
		MatchedPattern string  `json:"matched_pattern,omitempty"`
		Provider       string  `json:"provider,omitempty"`
		ProviderModel  string  `json:"provider_model,omitempty"`
		Model          string  `json:"model,omitempty"`
		Endpoint       string  `json:"endpoint"`
		Value          float64 `json:"value"`
		Count          uint64  `json:"count"`
	}
	collectQuantiles := func(collector *prometheus.HistogramVec, q float64) []quantileStat {
		var rows []quantileStat
		for _, met := range telemetrypkg.CollectMetrics(collector) {
			row := quantileStat{
				Value: telemetrypkg.HistogramQuantile(q, met.GetHistogram().GetBucket()),
				Count: met.GetHistogram().GetSampleCount(),
			}
			for _, l := range met.GetLabel() {
				switch l.GetName() {
				case "route":
					row.Route = l.GetValue()
				case "protocol":
					row.Protocol = l.GetValue()
				case "route_model":
					row.RouteModel = l.GetValue()
					row.Model = l.GetValue()
				case "matched_pattern":
					row.MatchedPattern = l.GetValue()
				case "provider":
					row.Provider = l.GetValue()
				case "provider_model":
					row.ProviderModel = l.GetValue()
					row.Model = l.GetValue()
				case "endpoint":
					row.Endpoint = l.GetValue()
				}
			}
			rows = append(rows, row)
		}
		return rows
	}

	providerRequests := collectRequestStats(telemetrypkg.ProviderRequestCounter)
	routeRequests := collectRequestStats(telemetrypkg.RouteRequestCounter)
	providerDurations := collectDurationStats(telemetrypkg.ProviderRequestDuration)
	routeDurations := collectDurationStats(telemetrypkg.RouteRequestDuration)
	providerTokens := collectTokenStats(telemetrypkg.ProviderTokenCounter)
	routeTokens := collectTokenStats(telemetrypkg.RouteTokenCounter)
	providerTTFT := collectQuantiles(telemetrypkg.ProviderStreamTTFT, 0.95)
	routeTTFT := collectQuantiles(telemetrypkg.RouteStreamTTFT, 0.95)
	providerThroughput := collectQuantiles(telemetrypkg.ProviderCompletionThroughput, 0.99)
	routeThroughput := collectQuantiles(telemetrypkg.RouteCompletionThroughput, 0.99)

	realtime := telemetrypkg.DashboardRealtimeSnapshot{}
	if dashboardStore != nil {
		realtime = dashboardStore.Snapshot()
	}

	return map[string]any{
		"requests_total":                 providerRequests,
		"request_duration":               providerDurations,
		"tokens_total":                   providerTokens,
		"token_rate":                     providerRates,
		"stream_ttft_p95_ms":             providerTTFT,
		"throughput_p99_tokens":          providerThroughput,
		"route_requests_total":           routeRequests,
		"route_request_duration":         routeDurations,
		"route_tokens_total":             routeTokens,
		"route_token_rate":               routeRates,
		"route_stream_ttft_p95_ms":       routeTTFT,
		"route_throughput_p99_tokens":    routeThroughput,
		"provider_requests_total":        providerRequests,
		"provider_request_duration":      providerDurations,
		"provider_tokens_total":          providerTokens,
		"provider_token_rate":            providerRates,
		"provider_stream_ttft_p95_ms":    providerTTFT,
		"provider_throughput_p99_tokens": providerThroughput,
		"realtime":                       realtime,
	}
}

func CollectDashboardCounters(outputRates *telemetrypkg.OutputRateTracker) telemetrypkg.DashboardCounterSample {
	sample := telemetrypkg.DashboardCounterSample{
		Timestamp:    time.Now(),
		OutputByProv: make(map[string]float64),
		RouteReqs:    make(map[string]float64),
		RouteFails:   make(map[string]float64),
		RouteOutput:  make(map[string]float64),
	}

	for _, met := range telemetrypkg.CollectMetrics(telemetrypkg.RouteRequestCounter) {
		value := met.GetCounter().GetValue()
		sample.Requests += value
		route := ""
		failed := false
		for _, label := range met.GetLabel() {
			if label.GetName() == "route" {
				route = label.GetValue()
				continue
			}
			if label.GetName() == "status" && label.GetValue() == "failure" {
				sample.Failures += value
				failed = true
			}
		}
		if route != "" {
			sample.RouteReqs[route] += value
			if failed {
				sample.RouteFails[route] += value
			}
		}
	}

	for _, met := range telemetrypkg.CollectMetrics(telemetrypkg.RouteTokenCounter) {
		sample.Tokens += met.GetCounter().GetValue()
	}

	if outputRates != nil {
		for _, entry := range outputRates.Snapshot(sample.Timestamp) {
			if entry.Type != "completion" {
				continue
			}
			sample.OutputRate += entry.Value
			if entry.Provider != "" {
				sample.OutputByProv[entry.Provider] += entry.Value
			}
			if entry.Route != "" {
				sample.RouteOutput[entry.Route] += entry.Value
			}
		}
	}

	for _, met := range telemetrypkg.CollectMetrics(telemetrypkg.RouteFailovers) {
		sample.Failovers += met.GetCounter().GetValue()
	}

	for _, met := range telemetrypkg.CollectMetrics(telemetrypkg.RouteStreamErrors) {
		sample.StreamErrors += met.GetCounter().GetValue()
	}

	return sample
}

func ListAPIKeysPayload(apiKeys map[string]config.SecretString) []map[string]any {
	type usageStats struct {
		TotalRequests    int64 `json:"total_requests"`
		SuccessRequests  int64 `json:"success_requests"`
		FailureRequests  int64 `json:"failure_requests"`
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	}

	usageByKey := map[string]*usageStats{}
	for _, met := range telemetrypkg.CollectMetrics(telemetrypkg.APIKeyRequestCounter) {
		var key string
		var status string
		for _, label := range met.GetLabel() {
			switch label.GetName() {
			case "api_key":
				key = label.GetValue()
			case "status":
				status = label.GetValue()
			}
		}
		if key == "" {
			continue
		}
		row := usageByKey[key]
		if row == nil {
			row = &usageStats{}
			usageByKey[key] = row
		}
		value := int64(met.GetCounter().GetValue())
		row.TotalRequests += value
		if status == "success" {
			row.SuccessRequests += value
		}
		if status == "failure" {
			row.FailureRequests += value
		}
	}

	for _, met := range telemetrypkg.CollectMetrics(telemetrypkg.APIKeyTokenCounter) {
		var key string
		var typ string
		for _, label := range met.GetLabel() {
			switch label.GetName() {
			case "api_key":
				key = label.GetValue()
			case "type":
				typ = label.GetValue()
			}
		}
		if key == "" {
			continue
		}
		row := usageByKey[key]
		if row == nil {
			row = &usageStats{}
			usageByKey[key] = row
		}
		value := int64(met.GetCounter().GetValue())
		switch typ {
		case "prompt":
			row.PromptTokens += value
		case "completion":
			row.CompletionTokens += value
		}
	}

	keys := make([]map[string]any, 0, len(apiKeys))
	for name := range apiKeys {
		usage := usageByKey[name]
		if usage == nil {
			usage = &usageStats{}
		}
		keys = append(keys, map[string]any{
			"name":  name,
			"usage": usage,
		})
	}
	return keys
}
