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
	sel "github.com/wweir/warden/internal/selector"
	"github.com/wweir/warden/internal/mcp"
	"github.com/wweir/warden/internal/reqlog"
	"github.com/wweir/warden/pkg/protocol/anthropic"
	"github.com/wweir/warden/pkg/protocol/openai"
)

// Gateway is the core AI Gateway component.
type Gateway struct {
	cfg         *config.ConfigStruct
	configPath  string
	configHash  string
	selector    *sel.Selector

	mcpClients  map[string]*mcp.Client
	logger      reqlog.Logger
	broadcaster *reqlog.Broadcaster
	handler     http.Handler
	reloadFn    func() error
	ctx         context.Context
	cancel      context.CancelFunc
}

// SetReloadFn sets the function called to hot-reload the gateway.
func (g *Gateway) SetReloadFn(fn func() error) {
	g.reloadFn = fn
}

// NewGateway creates a new Gateway instance with routes registered once.
func NewGateway(cfg *config.ConfigStruct, configPath, configHash string) *Gateway {
	var err error
	defer func() { deferlog.DebugError(err, "create new gateway") }()

	ctx, cancel := context.WithCancel(context.Background())

	mcpClients := make(map[string]*mcp.Client)
	for name, mcpCfg := range cfg.MCP {
		client, err := mcp.NewClient(mcpCfg)
		if err != nil {
			slog.Warn("Failed to create MCP client", "name", name, "error", err)
			continue
		}
		if err := client.Start(ctx, mcpCfg); err != nil {
			slog.Warn("Failed to start MCP client", "name", name, "error", err)
			continue
		}
		mcpClients[name] = client
	}

	g := &Gateway{
		cfg:         cfg,
		configPath:  configPath,
		configHash:  configHash,
		selector:    sel.NewSelector(cfg),
		mcpClients:  mcpClients,
		broadcaster: reqlog.NewBroadcaster(),
		ctx:         ctx,
		cancel:      cancel,
	}

	g.selector.RefreshModels(cfg)

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
		router.Handle(http.MethodPost, prefix+"/chat/completions",
			func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
				g.handleChatCompletion(w, r, route)
			})
		router.Handle(http.MethodPost, prefix+"/responses",
			func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
				g.handleResponses(w, r, route)
			})
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
		&PromMiddleware{gateway: g},
	).Process(router)

	return g
}

// ServeHTTP implements http.Handler.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.handler.ServeHTTP(w, r)
}

// Close shuts down all MCP clients and the request logger.
func (g *Gateway) Close() {
	g.cancel()
	for name, c := range g.mcpClients {
		slog.Info("Stopping MCP client", "name", name)
		if err := c.Close(); err != nil {
			slog.Warn("Failed to stop MCP client", "name", name, "error", err)
		}
	}
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
	// Set headers for metrics middleware
	w.Header().Set("X-Route", route.Prefix)
	w.Header().Set("X-Endpoint", strings.TrimPrefix(r.URL.Path, "/"))

	startTime := time.Now()
	reqID := reqlog.GenerateID()

	// always buffer request body for model extraction and alias resolution
	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.Body.Close()

	// extract model from request body for provider selection (best-effort; non-JSON bodies are fine)
	model := gjson.GetBytes(reqBody, "model").String()
	w.Header().Set("X-Model", model)

	var excluded []string
	authRetried := map[string]bool{}
	provCfg, err := g.selector.Select(g.cfg, route, model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set provider header for metrics middleware
	w.Header().Set("X-Provider", provCfg.Name)

	allowFailover := isInferenceEndpoint(r.URL.Path)

	for {
		logRequest(r, provCfg.Name, model)

		// resolve model alias to real model name for upstream request
		provReqBody := prepareRawBody(reqBody, provCfg, model)

		targetURL := provCfg.URL + r.URL.Path
		if r.URL.RawQuery != "" {
			targetURL += "?" + r.URL.RawQuery
		}

		proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(provReqBody))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		proxyReq.Header = r.Header.Clone()
		proxyReq.Header.Del("Accept-Encoding")
		sel.SetAuthHeaders(proxyReq.Header, provCfg)

		upstreamStart := time.Now()
		resp, err := provCfg.HTTPClient(0).Do(proxyReq)
		latency := time.Since(upstreamStart)
		if err != nil {
			g.selector.RecordOutcome(provCfg.Name, err, latency)
			// Only failover for inference endpoints
			if allowFailover {
				if next := g.tryFailover(err, provCfg.Name, &excluded, route, model); next != nil {
					provCfg = next
					continue
				}
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// check for upstream errors (non-200 or error in 200 body)
		var upErr *sel.UpstreamError
		if resp.StatusCode != http.StatusOK {
			upErr = &sel.UpstreamError{Code: resp.StatusCode, Body: string(respBody)}
		} else if errType, _ := sel.ParseErrorBody(string(respBody)); errType != "" && sel.IsRetryableByBody(string(respBody)) {
			upErr = &sel.UpstreamError{Code: resp.StatusCode, Body: string(respBody)}
		}

		if upErr != nil {
			g.selector.RecordOutcome(provCfg.Name, upErr, latency)
			if tryAuthRetry(upErr, provCfg, authRetried) {
				continue
			}
			// Only failover for inference endpoints
			if allowFailover {
				if next := g.tryFailover(upErr, provCfg.Name, &excluded, route, model); next != nil {
					provCfg = next
					continue
				}
			}
		} else {
			g.selector.RecordOutcome(provCfg.Name, nil, latency)
		}

		maps.Copy(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		if _, writeErr := w.Write(respBody); writeErr != nil {
			slog.Warn("Failed to write proxy response", "error", writeErr)
		}

		durationMs := time.Since(startTime).Milliseconds()
		logResp := assembleProxyResponse(provCfg.Protocol, respBody)

		// Extract token usage for metrics (only for LLM responses)
		if resp.StatusCode == http.StatusOK && len(logResp) > 0 {
			usage := ExtractTokenUsage(logResp)
			g.RecordTokenMetrics(provCfg.Name, model, usage, durationMs)
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
func assembleProxyResponse(protocol string, body []byte) []byte {
	trimmed := strings.TrimSpace(string(body))
	if !strings.HasPrefix(trimmed, "event:") && !strings.HasPrefix(trimmed, "data:") {
		return body
	}
	if protocol == "anthropic" {
		if assembled := anthropic.AssembleStream(body); assembled != nil {
			return assembled
		}
		return body
	}
	if assembled, err := openai.AssembleChatStream(body); err == nil {
		return assembled
	}
	return body
}

// --- tool collection ---

// collectTools gathers all enabled MCP tools for a route using cached tool list.
func (g *Gateway) collectTools(_ context.Context, route *config.RouteConfig) ([]mcp.Tool, []string) {
	var available []mcp.Tool
	for _, toolName := range route.Tools {
		if !route.IsToolEnabled(toolName) {
			continue
		}
		client, exists := g.mcpClients[toolName]
		if !exists {
			continue
		}
		mcpCfg := g.cfg.MCP[toolName]
		for _, t := range client.CachedTools() {
			// skip tools explicitly disabled in per-tool config
			if mcpCfg != nil {
				if tc, ok := mcpCfg.Tools[t.Name]; ok && tc.Disabled {
					continue
				}
			}
			t.Name = toolName + "__" + t.Name
			available = append(available, t)
		}
	}

	names := make([]string, len(available))
	for i, t := range available {
		names[i] = t.Name
	}
	return available, names
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
