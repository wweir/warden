package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/sower-proxy/deferlog/v2"
	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
	sel "github.com/wweir/warden/internal/selector"
	"github.com/wweir/warden/pkg/protocol/anthropic"
	"github.com/wweir/warden/pkg/protocol/openai"
)

// Gateway is the core AI Gateway component.
type Gateway struct {
	cfg        *config.ConfigStruct
	configPath string
	configHash string
	selector   *sel.Selector

	logger         reqlog.Logger
	broadcaster    *reqlog.Broadcaster
	dashboardStore *dashboardMetricsStore
	outputRates    *outputRateTracker
	handler        http.Handler
	reloadFn       func() error
	ctx            context.Context
	cancel         context.CancelFunc
}

const (
	dashboardMetricsSampleInterval = 2 * time.Second
	dashboardMetricsHistoryLimit   = 180
)

// SetReloadFn sets the function called to hot-reload the gateway.
func (g *Gateway) SetReloadFn(fn func() error) {
	g.reloadFn = fn
}

// NewGateway creates a new Gateway instance with routes registered once.
func NewGateway(cfg *config.ConfigStruct, configPath, configHash string) *Gateway {
	var err error
	defer func() { deferlog.DebugError(err, "create new gateway") }()

	ctx, cancel := context.WithCancel(context.Background())

	g := &Gateway{
		cfg:            cfg,
		configPath:     configPath,
		configHash:     configHash,
		selector:       sel.NewSelector(cfg),
		broadcaster:    reqlog.NewBroadcaster(),
		dashboardStore: newDashboardMetricsStore(dashboardMetricsSampleInterval, dashboardMetricsHistoryLimit),
		outputRates:    newOutputRateTracker(dashboardMetricsSampleInterval),
		ctx:            ctx,
		cancel:         cancel,
	}

	// Refresh models asynchronously to avoid blocking startup.
	// Model discovery failures are logged but don't prevent the service from starting.
	go g.selector.RefreshModels(cfg)
	g.dashboardStore.Start(ctx, g.collectDashboardCounters)

	g.logger = newLogger(cfg.Log)

	router := httprouter.New()
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
	router.HandleOPTIONS = false
	router.HandleMethodNotAllowed = false

	// register admin routes if password is configured
	if cfg.AdminPassword != "" {
		g.registerAdminRoutes(router)
	}

	// register metrics endpoint
	g.RegisterMetricsRoutes(router)

	for prefix, route := range cfg.Route {
		router.Handle(http.MethodGet, prefix+"/models",
			func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
				g.handleModels(w, r, route)
			})
		if g.shouldRegisterOpenAIEndpoint(route, config.RouteProtocolChat) {
			router.Handle(http.MethodPost, prefix+"/chat/completions",
				func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
					g.handleChatCompletion(w, r, route)
				})
		}
		if g.shouldRegisterOpenAIEndpoint(route, config.RouteProtocolResponsesStateless) {
			router.Handle(http.MethodPost, prefix+"/responses",
				func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
					g.handleResponses(w, r, route)
				})
		}
		if route.ConfiguredProtocol() == config.RouteProtocolAnthropic {
			router.Handle(http.MethodPost, prefix+"/messages",
				func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
					g.handleAnthropicMessages(w, r, route)
				})
		}
	}

	// fallback: match route prefix and proxy unhandled requests
	router.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// redirect root to admin panel for browser access
		if r.URL.Path == "/" && cfg.AdminPassword != "" {
			http.Redirect(w, r, "/_admin/", http.StatusFound)
			return
		}
		for prefix, route := range cfg.Route {
			if strings.HasPrefix(r.URL.Path, prefix+"/") {
				r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
				g.handleProxy(w, r, route)
				return
			}
		}
		http.NotFound(w, r)
	})

	g.handler = Chain(
		&RecoveryMiddleware{},
		&CORS{},
		&APIKeyAuthMiddleware{cfg: g.cfg},
		&PromMiddleware{gateway: g},
	).Process(router)

	return g
}

func (g *Gateway) shouldRegisterOpenAIEndpoint(route *config.RouteConfig, serviceProtocol string) bool {
	switch serviceProtocol {
	case config.RouteProtocolChat:
		return route.ConfiguredProtocol() == config.RouteProtocolChat
	case config.RouteProtocolResponsesStateless:
		return config.IsResponsesRouteProtocol(route.ConfiguredProtocol())
	default:
		return false
	}
}

// ServeHTTP implements http.Handler.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.handler.ServeHTTP(w, r)
}

// Close shuts down the gateway runtime and the request logger.
func (g *Gateway) Close() {
	g.cancel()
	if g.logger != nil {
		g.logger.Close()
	}
}

// Broadcaster returns the request log broadcaster for admin subscriptions.
func (g *Gateway) Broadcaster() *reqlog.Broadcaster {
	return g.broadcaster
}

// recordAndBroadcast logs a record to file (if enabled) and publishes to SSE subscribers.
func (g *Gateway) recordAndBroadcast(r reqlog.Record) {
	r.Sanitize()
	if g.logger != nil {
		g.logger.Log(r)
	}
	g.broadcaster.Publish(r)
}

// --- proxy ---

// isInferenceEndpoint checks if the path is an inference endpoint that should trigger failover.
// Only chat/completions, responses, and anthropic messages endpoints are considered inference endpoints.
func isInferenceEndpoint(path string) bool {
	// OpenAI: /chat/completions, /responses
	// Anthropic: /messages (but not /messages/count_tokens or other sub-endpoints)
	if strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/responses") {
		return true
	}
	if path == "/messages" || strings.HasSuffix(path, "/messages") {
		return true
	}
	return false
}

// handleProxy transparently forwards non-chat/responses requests.
func (g *Gateway) handleProxy(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	r = r.WithContext(withRouteHooks(withClientRequest(r.Context(), r), route.Hooks))

	startTime := time.Now()
	reqID := reqlog.GenerateID()

	// always buffer request body for model extraction and route-model rewrite
	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.Body.Close()

	// extract model from request body for provider selection (best-effort; non-JSON bodies are fine)
	model := gjson.GetBytes(reqBody, "model").String()
	stream := gjson.GetBytes(reqBody, "stream").Bool()

	var excluded []string
	authRetried := map[string]bool{}
	var failovers []reqlog.Failover
	explicitProvider := r.Header.Get("X-Provider")
	allowFailover := isInferenceEndpoint(r.URL.Path)
	serviceProtocol := routeServiceProtocol(r.URL.Path, reqBody)
	endpoint := strings.TrimPrefix(r.URL.Path, "/")
	if serviceProtocol != "" && !route.SupportsServiceProtocol(serviceProtocol) {
		http.Error(w, unsupportedRouteProtocolMessage(route.ConfiguredProtocol(), serviceProtocol), http.StatusBadRequest)
		return
	}
	if serviceProtocol == config.RouteProtocolResponsesStateful {
		allowFailover = false
	}
	var resolved *resolvedRouteTarget
	var provCfg *config.ProviderConfig
	var target *sel.RouteTarget
	if model != "" {
		resolved, err = g.selectRouteTarget(route, serviceProtocol, model, explicitProvider, excluded)
		if err != nil {
			writeModelSelectionError(w, err)
			return
		}
		provCfg = resolved.prov
		target = resolved.target
	} else {
		for _, providerName := range route.ProviderNames() {
			if explicitProvider != "" && providerName != explicitProvider {
				continue
			}
			candidate := g.cfg.Provider[providerName]
			if candidate == nil {
				continue
			}
			if serviceProtocol != "" && !config.ProviderSupportsConfiguredProtocol(candidate, serviceProtocol) {
				continue
			}
			provCfg = candidate
			break
		}
		if provCfg == nil {
			http.Error(w, "provider not found for route", http.StatusBadRequest)
			return
		}
	}

	metricLabels := buildMetricLabels(route, serviceProtocol, endpoint, target)
	metricLabels.APIKey = apiKeyNameFromContext(r.Context())
	if target == nil {
		metricLabels.Provider = provCfg.Name
	}
	applyMetricHeaders(w, metricLabels)

	for {
		logRequest(r, provCfg.Name, model)

		provReqBody := prepareRawBody(reqBody, target)

		targetURL := provCfg.URL + r.URL.Path
		if r.URL.RawQuery != "" {
			targetURL += "?" + r.URL.RawQuery
		}

		proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(provReqBody))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		proxyReq.Header = buildProxyRequestHeaders(r, allowFailover)
		sel.SetAuthHeaders(proxyReq.Header, provCfg)

		upstreamStart := time.Now()
		resp, err := provCfg.HTTPClient(0).Do(proxyReq)
		latency := time.Since(upstreamStart)
		if err != nil {
			// Only record errors for inference endpoints to avoid suppressing providers on non-core URL failures
			if allowFailover {
				g.selector.RecordOutcome(provCfg.Name, err, latency)
			}
			// Only failover for inference endpoints
			if allowFailover && resolved != nil && explicitProvider == "" {
				if next := g.tryFailover(err, resolved, &excluded, route, serviceProtocol, endpoint, model, &failovers); next != nil {
					resolved = next
					provCfg = next.prov
					target = next.target
					metricLabels = buildMetricLabels(route, serviceProtocol, endpoint, target)
					applyMetricHeaders(w, metricLabels)
					continue
				}
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		contentEncoding := normalizeContentEncoding(resp.Header.Get("Content-Encoding"))
		decodedRespBody, decodeErr := decodeResponseBody(contentEncoding, respBody)
		inspectableBody := decodeErr == nil
		errBody := string(respBody)
		if inspectableBody {
			errBody = string(decodedRespBody)
		} else {
			errBody = compressedBodyPlaceholder(contentEncoding, len(respBody))
		}

		// check for upstream errors (non-200 or error in 200 body)
		var upErr *sel.UpstreamError
		if resp.StatusCode != http.StatusOK {
			upErr = &sel.UpstreamError{Code: resp.StatusCode, Body: errBody}
		} else if inspectableBody {
			if errType, _ := sel.ParseErrorBody(string(decodedRespBody)); errType != "" && sel.IsRetryableByBody(string(decodedRespBody)) {
				upErr = &sel.UpstreamError{Code: resp.StatusCode, Body: string(decodedRespBody)}
			}
		}

		if upErr != nil {
			// Only record errors for inference endpoints to avoid suppressing providers on non-core URL failures
			if allowFailover {
				g.selector.RecordOutcome(provCfg.Name, upErr, latency)
			}
			if tryAuthRetry(upErr, provCfg, authRetried) {
				continue
			}
			// Only failover for inference endpoints
			if allowFailover && resolved != nil && explicitProvider == "" {
				if next := g.tryFailover(upErr, resolved, &excluded, route, serviceProtocol, endpoint, model, &failovers); next != nil {
					resolved = next
					provCfg = next.prov
					target = next.target
					metricLabels = buildMetricLabels(route, serviceProtocol, endpoint, target)
					applyMetricHeaders(w, metricLabels)
					continue
				}
			}
		} else {
			g.selector.RecordOutcome(provCfg.Name, nil, latency)
			if stream {
				g.RecordTTFTMetric(metricLabels, latency)
			}
		}

		maps.Copy(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		if _, writeErr := w.Write(respBody); writeErr != nil {
			slog.Warn("Failed to write proxy response", "error", writeErr)
		}

		durationMs := time.Since(startTime).Milliseconds()
		logResp := []byte(errBody)
		if inspectableBody {
			logResp = assembleProxyResponse(serviceProtocol, provCfg.Protocol, decodedRespBody)
		}

		// Extract token usage for metrics (only for LLM responses)
		if resp.StatusCode == http.StatusOK && inspectableBody && len(logResp) > 0 {
			usage := ExtractTokenUsage(logResp)
			endpoint := strings.TrimPrefix(r.URL.Path, route.Prefix)
			endpoint = strings.TrimPrefix(endpoint, "/")
			g.RecordTokenMetrics(metricLabels, usage, durationMs)
		}

		rec := reqlog.Record{
			Timestamp:   startTime,
			RequestID:   reqID,
			Route:       route.Prefix,
			Endpoint:    r.URL.Path,
			Model:       model,
			Provider:    provCfg.Name,
			UserAgent:   r.UserAgent(),
			DurationMs:  durationMs,
			Fingerprint: reqlog.BuildFingerprint(reqBody),
			Request:     reqBody,
			Response:    logResp,
			Failovers:   failovers,
		}
		if upErr != nil {
			rec.Error = upErr.Error()
		}
		g.recordAndBroadcast(rec)
		return
	}
}

// assembleProxyResponse converts a raw proxy response body for logging.
// If the body is SSE, it assembles the stream into a single JSON object.
// For Anthropic SSE, it extracts the response.completed event.
// For other protocols, it uses OpenAI Chat Completions stream assembly.
// Non-SSE bodies are returned as-is.
func assembleProxyResponse(serviceProtocol, upstreamProtocol string, body []byte) []byte {
	trimmed := strings.TrimSpace(string(body))
	if !strings.HasPrefix(trimmed, "event:") && !strings.HasPrefix(trimmed, "data:") {
		return body
	}
	if serviceProtocol == config.RouteProtocolAnthropic {
		if assembled := anthropic.AssembleStream(body); assembled != nil {
			return assembled
		}
		return marshalRawStreamForLog(body)
	}
	if config.IsResponsesRouteProtocol(serviceProtocol) {
		if assembled, err := openai.AssembleResponsesStream(body); err == nil {
			return assembled
		}
		return marshalRawStreamForLog(body)
	}
	clientBody := convertStreamIfNeeded(upstreamProtocol, body)
	if assembled, err := openai.AssembleChatStream(clientBody); err == nil {
		return assembled
	}
	return marshalRawStreamForLog(clientBody)
}

func marshalRawStreamForLog(body []byte) []byte {
	data, err := json.Marshal(strings.TrimSpace(string(body)))
	if err != nil {
		return json.RawMessage(`""`)
	}
	return data
}

func routeServiceProtocol(path string, reqBody []byte) string {
	if strings.HasSuffix(path, "/messages") || path == "/messages" {
		return config.RouteProtocolAnthropic
	}
	if strings.HasSuffix(path, "/chat/completions") {
		return config.RouteProtocolChat
	}
	if strings.HasSuffix(path, "/responses") {
		return responsesRequestProtocol(reqBody)
	}
	return ""
}

func unsupportedRouteProtocolMessage(routeProtocol, serviceProtocol string) string {
	switch serviceProtocol {
	case config.RouteProtocolResponsesStateful:
		return unsupportedResponsesProtocolMessage(routeProtocol, serviceProtocol)
	case config.RouteProtocolResponsesStateless:
		return unsupportedResponsesProtocolMessage(routeProtocol, serviceProtocol)
	case config.RouteProtocolChat:
		return "route protocol " + routeProtocol + " does not support chat requests"
	case config.RouteProtocolAnthropic:
		return "route protocol " + routeProtocol + " does not support anthropic messages requests"
	default:
		return "route does not support this request protocol"
	}
}

// --- models ---

// handleModels returns an aggregated list of models from all providers in the route.
func (g *Gateway) handleModels(w http.ResponseWriter, _ *http.Request, route *config.RouteConfig) {
	// Don't set metrics headers - this is a metadata endpoint, not a business request

	models := g.selector.Models(g.cfg, route)
	if models == nil {
		models = []json.RawMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   models,
	})
}
