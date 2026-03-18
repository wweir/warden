package gateway

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
	sel "github.com/wweir/warden/internal/selector"
	"github.com/wweir/warden/web"
	"gopkg.in/yaml.v3"
)

const redactedPlaceholder = "__REDACTED__"

const adminSSEHeartbeatInterval = 15 * time.Second

// registerAdminRoutes registers all /_admin/ routes with Basic Auth.
// Uses an internal http.ServeMux for sub-routing to avoid httprouter
// wildcard conflicts between /_admin/*filepath and /_admin/api/* routes.
func (g *Gateway) registerAdminRoutes(router *httprouter.Router) {
	auth := g.basicAuth

	adminFS, err := fs.Sub(web.AdminFS, "admin/dist")
	if err != nil {
		slog.Warn("Failed to load admin frontend", "error", err)
		return
	}
	fileServer := http.FileServer(http.FS(adminFS))
	readFS := adminFS.(fs.ReadFileFS)

	serveFSFile := func(w http.ResponseWriter, r *http.Request, name string) {
		brData, brErr := readFS.ReadFile(name + ".br")
		if brErr == nil {
			w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(name)))
			w.Header().Set("Vary", "Accept-Encoding")
			if selectAcceptedEncoding(r.Header.Get("Accept-Encoding"), []string{"br"}) == "br" {
				w.Header().Set("Content-Encoding", "br")
				w.Write(brData)
				return
			}
			// decompress on-the-fly for clients that don't support brotli
			if _, err := io.Copy(w, brotli.NewReader(bytes.NewReader(brData))); err != nil {
				slog.Error("brotli decompress failed", "file", name, "error", err)
			}
			return
		}
		r.URL.Path = "/" + name
		fileServer.ServeHTTP(w, r)
	}

	// internal mux for admin sub-routing
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		g.handleAdminStatus(w, r, nil)
	})
	mux.HandleFunc("GET /api/config/source", func(w http.ResponseWriter, r *http.Request) {
		g.handleAdminConfigSource(w, r, nil)
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		g.handleAdminConfigGet(w, r, nil)
	})
	mux.HandleFunc("PUT /api/config", func(w http.ResponseWriter, r *http.Request) {
		g.handleAdminConfigPut(w, r, nil)
	})
	mux.HandleFunc("GET /api/logs/stream", func(w http.ResponseWriter, r *http.Request) {
		g.handleLogStream(w, r, nil)
	})
	mux.HandleFunc("GET /api/tool-hooks/suggestions", func(w http.ResponseWriter, r *http.Request) {
		g.handleToolHookSuggestions(w, r, nil)
	})
	mux.HandleFunc("POST /api/restart", func(w http.ResponseWriter, r *http.Request) {
		g.handleRestart(w, r, nil)
	})
	mux.HandleFunc("POST /api/providers/health", func(w http.ResponseWriter, r *http.Request) {
		g.handleProviderHealth(w, r, nil)
	})
	mux.HandleFunc("GET /api/providers/detail", func(w http.ResponseWriter, r *http.Request) {
		g.handleProviderDetail(w, r, nil)
	})
	mux.HandleFunc("POST /api/providers/suppress", func(w http.ResponseWriter, r *http.Request) {
		g.handleProviderSuppress(w, r, nil)
	})
	mux.HandleFunc("POST /api/config/validate", func(w http.ResponseWriter, r *http.Request) {
		g.handleConfigValidate(w, r, nil)
	})
	mux.HandleFunc("GET /api/routes/detail", func(w http.ResponseWriter, r *http.Request) {
		g.handleRouteDetail(w, r, nil)
	})
	mux.HandleFunc("GET /api/metrics/stream", func(w http.ResponseWriter, r *http.Request) {
		g.handleMetricsStream(w, r, nil)
	})
	mux.HandleFunc("GET /api/apikeys", func(w http.ResponseWriter, r *http.Request) {
		g.handleAPIKeysList(w, r, nil)
	})
	mux.HandleFunc("POST /api/apikeys", func(w http.ResponseWriter, r *http.Request) {
		g.handleAPIKeysCreate(w, r, nil)
	})
	mux.HandleFunc("DELETE /api/apikeys", func(w http.ResponseWriter, r *http.Request) {
		g.handleAPIKeysDelete(w, r, nil)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fp := strings.TrimPrefix(r.URL.Path, "/")
		if fp != "" {
			if _, err := readFS.ReadFile(fp + ".br"); err == nil {
				serveFSFile(w, r, fp)
				return
			}
			if _, err := readFS.ReadFile(fp); err == nil {
				serveFSFile(w, r, fp)
				return
			}
		}
		// SPA fallback: serve index.html for non-asset paths
		serveFSFile(w, r, "index.html")
	})

	adminHandler := auth(func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/_admin")
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		mux.ServeHTTP(w, r)
	})

	router.Handle(http.MethodGet, "/_admin", adminHandler)
	router.Handle(http.MethodGet, "/_admin/*filepath", adminHandler)
	router.Handle(http.MethodPut, "/_admin/*filepath", adminHandler)
	router.Handle(http.MethodPost, "/_admin/*filepath", adminHandler)
}

// basicAuth wraps a handler with HTTP Basic Auth.
func (g *Gateway) basicAuth(next httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || subtle.ConstantTimeCompare([]byte(pass), []byte(g.cfg.AdminPassword.Value())) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Warden Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r, ps)
	}
}

// handleAdminStatus streams provider statuses and route info via SSE.
func (g *Gateway) handleAdminStatus(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	flusher, ok := prepareSSEWriter(w)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// send initial data immediately
	g.writeStatusSSE(w)
	flusher.Flush()

	for {
		select {
		case <-ticker.C:
			g.writeStatusSSE(w)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (g *Gateway) writeStatusSSE(w http.ResponseWriter) {
	type routeInfo struct {
		Prefix    string   `json:"prefix"`
		Protocol  string   `json:"protocol"`
		Providers []string `json:"providers"`
		Models    []string `json:"models"`
		HookCount int      `json:"hook_count"`
	}

	providers := g.selector.ProviderStatuses()

	var routes []routeInfo
	for prefix, r := range g.cfg.Route {
		routes = append(routes, routeInfo{
			Prefix:    prefix,
			Protocol:  r.Protocol,
			Providers: r.ProviderNames(),
			Models:    r.PublicModels(),
			HookCount: len(r.Hooks),
		})
	}
	slices.SortFunc(routes, func(a, b routeInfo) int {
		return strings.Compare(a.Prefix, b.Prefix)
	})

	data, _ := json.Marshal(map[string]any{
		"providers": providers,
		"routes":    routes,
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// handleAdminConfigSource returns information about the config source.
// It indicates whether the config was loaded from a file and if it can be saved.
func (g *Gateway) handleAdminConfigSource(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"source_type": map[string]any{
			"file": g.configPath != "",
		},
		"config_path": g.configPath,
		"config_hash": g.configHash,
	})
}

// handleAdminConfigGet returns the current config with API keys masked.
func (g *Gateway) handleAdminConfigGet(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// marshal to map for masking
	data, err := json.Marshal(g.cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var cfgMap map[string]any
	json.Unmarshal(data, &cfgMap)

	// mask sensitive fields
	maskAPIKeys(cfgMap)

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(cfgMap)
}

// maskAPIKeys recursively masks api_key and admin_password fields.
func maskAPIKeys(m map[string]any) {
	for k, v := range m {
		if k == "api_keys" {
			if keys, ok := v.(map[string]any); ok {
				for name, raw := range keys {
					if s, ok := raw.(string); ok && s != "" {
						keys[name] = redactedPlaceholder
					}
				}
			}
		}
		if k == "api_key" || k == "admin_password" {
			if s, ok := v.(string); ok && s != "" {
				m[k] = redactedPlaceholder
			}
		}
		if sub, ok := v.(map[string]any); ok {
			maskAPIKeys(sub)
		}
	}
}

// handleAdminConfigPut updates the config from JSON body.
func (g *Gateway) handleAdminConfigPut(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var err error
	defer func() { deferlog.DebugError(err, "admin config update") }()

	if g.configPath == "" {
		http.Error(w, "no config file path configured", http.StatusBadRequest)
		return
	}

	var body json.RawMessage
	if err = json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// save as YAML
	var cfgMap any
	if err = json.Unmarshal(body, &cfgMap); err != nil {
		http.Error(w, "invalid config: "+err.Error(), http.StatusBadRequest)
		return
	}

	// restore masked secrets from current config
	if newMap, ok := cfgMap.(map[string]any); ok {
		currentData, _ := json.Marshal(g.cfg)
		var currentMap map[string]any
		if json.Unmarshal(currentData, &currentMap) == nil {
			// inject real secret values since json.Marshal masks them via Secret.MarshalText
			injectSecrets(currentMap, g.cfg)
			sanitizeConfigJSON(newMap, currentMap)
		}
		// drop api_key for OAuth-based providers (qwen/copilot use config_dir credentials)
		dropOAuthProviderAPIKey(newMap)
	}

	yamlData, err := yaml.Marshal(cfgMap)
	if err != nil {
		http.Error(w, "encode config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// hash check
	if g.configHash != "" {
		current, readErr := os.ReadFile(g.configPath)
		if readErr == nil {
			currentHash := fmt.Sprintf("%x", sha256.Sum256(current))
			if currentHash != g.configHash {
				http.Error(w, "config file changed externally, please reload", http.StatusConflict)
				return
			}
		}
	}

	if err = os.WriteFile(g.configPath, yamlData, 0o644); err != nil {
		http.Error(w, "write config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	g.configHash = fmt.Sprintf("%x", sha256.Sum256(yamlData))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleLogStream serves SSE real-time log events.
func (g *Gateway) handleLogStream(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	flusher, ok := prepareSSEWriter(w)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := g.broadcaster.Subscribe()
	defer g.broadcaster.Unsubscribe(ch)
	heartbeat := time.NewTicker(adminSSEHeartbeatInterval)
	defer heartbeat.Stop()

	writeSSEComment(w, "stream-open")
	flusher.Flush()

	// send recent history first
	for _, rec := range g.broadcaster.Recent() {
		writeSSE(w, rec)
	}
	flusher.Flush()

	for {
		select {
		case rec := <-ch:
			writeSSE(w, rec)
			flusher.Flush()
		case <-heartbeat.C:
			writeSSEComment(w, "keepalive")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (g *Gateway) handleToolHookSuggestions(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buildToolHookSuggestionsForRoute(g.broadcaster.Recent(), strings.TrimSpace(r.URL.Query().Get("route"))))
}

// writeSSE writes a single SSE event with JSON data.
func writeSSE(w http.ResponseWriter, r reqlog.Record) {
	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func prepareSSEWriter(w http.ResponseWriter) (http.Flusher, bool) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	return flusher, ok
}

func writeSSEComment(w http.ResponseWriter, comment string) {
	if comment == "" {
		comment = "keepalive"
	}
	fmt.Fprintf(w, ": %s\n\n", comment)
}

// injectSecrets writes real secret values from cfg into cfgMap,
// since json.Marshal masks SecretString fields as "***".
func injectSecrets(cfgMap map[string]any, cfg *config.ConfigStruct) {
	if cfg.AdminPassword != "" {
		cfgMap["admin_password"] = cfg.AdminPassword.Value()
	}
	// inject api_keys
	if len(cfg.APIKeys) > 0 {
		apiKeysMap, _ := cfgMap["api_keys"].(map[string]any)
		for name, key := range cfg.APIKeys {
			if key != "" && apiKeysMap != nil {
				apiKeysMap[name] = key.Value()
			}
		}
	}
	providerMap, _ := cfgMap["provider"].(map[string]any)
	for name, prov := range cfg.Provider {
		if prov.APIKey.Value() == "" {
			continue
		}
		if pm, ok := providerMap[name].(map[string]any); ok {
			pm["api_key"] = prov.APIKey.Value()
		}
	}
}

// sanitizeConfigJSON replaces redacted placeholder values with their current values to prevent overwriting secrets.
func sanitizeConfigJSON(newCfg map[string]any, currentCfg map[string]any) {
	for k, v := range newCfg {
		if s, ok := v.(string); ok && (s == redactedPlaceholder || s == "***") {
			if current, exists := currentCfg[k]; exists {
				newCfg[k] = current
			}
		}
		if sub, ok := v.(map[string]any); ok {
			if csub, ok := currentCfg[k].(map[string]any); ok {
				sanitizeConfigJSON(sub, csub)
			}
		}
	}
}

// dropOAuthProviderAPIKey removes api_key from providers that use OAuth credentials
// (qwen, copilot), since they authenticate via config_dir and should not store an api_key.
func dropOAuthProviderAPIKey(cfgMap map[string]any) {
	providerMap, _ := cfgMap["provider"].(map[string]any)
	for _, v := range providerMap {
		pm, ok := v.(map[string]any)
		if !ok {
			continue
		}
		proto, _ := pm["protocol"].(string)
		if proto == "qwen" || proto == "copilot" {
			delete(pm, "api_key")
		}
	}
}

// handleRestart triggers a hot-reload of the gateway via the configured reload function.
func (g *Gateway) handleRestart(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	if g.reloadFn == nil {
		http.Error(w, "reload not available", http.StatusInternalServerError)
		return
	}
	if err := g.reloadFn(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleProviderHealth tests connectivity to a provider by calling fetchModels.
func (g *Gateway) handleProviderHealth(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	provCfg, exists := g.cfg.Provider[body.Name]
	if !exists {
		http.Error(w, "unknown provider: "+body.Name, http.StatusNotFound)
		return
	}

	start := time.Now()
	_, rawModels, err := sel.FetchModels(provCfg)
	latency := time.Since(start)

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "error",
			"error":      err.Error(),
			"latency_ms": latency.Milliseconds(),
		})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"status":      "ok",
		"latency_ms":  latency.Milliseconds(),
		"model_count": len(rawModels),
	})
}

// handleProviderDetail returns detailed information about a single provider.
func (g *Gateway) handleProviderDetail(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "name parameter required", http.StatusBadRequest)
		return
	}

	provCfg, exists := g.cfg.Provider[name]
	if !exists {
		http.Error(w, "unknown provider: "+name, http.StatusNotFound)
		return
	}

	status := g.selector.ProviderDetail(name)
	models := g.selector.ProviderModels(name)
	if models == nil {
		models = []json.RawMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"name":              name,
		"url":               provCfg.URL,
		"protocol":          provCfg.Protocol,
		"timeout":           provCfg.Timeout,
		"has_api_key":       provCfg.APIKey.Value() != "",
		"responses_to_chat": provCfg.ResponsesToChat,
		"models":            models,
		"status":            status,
	})
}

// handleProviderSuppress sets or clears manual suppression for a provider.
func (g *Gateway) handleProviderSuppress(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var body struct {
		Name     string `json:"name"`
		Suppress bool   `json:"suppress"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	if _, exists := g.cfg.Provider[body.Name]; !exists {
		http.Error(w, "unknown provider: "+body.Name, http.StatusNotFound)
		return
	}

	if !g.selector.SetManualSuppress(body.Name, body.Suppress) {
		http.Error(w, "failed to update provider", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"name":              body.Name,
		"manual_suppressed": body.Suppress,
	})
}

// handleConfigValidate validates a JSON config body without saving.
func (g *Gateway) handleConfigValidate(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var cfgStruct config.ConfigStruct
	if err := json.NewDecoder(r.Body).Decode(&cfgStruct); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"valid": false, "error": "invalid JSON: " + err.Error()})
		return
	}

	if err := cfgStruct.Validate(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"valid": false, "error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"valid": true})
}

// handleRouteDetail returns detailed information about a single route,
// including associated provider statistics.
func (g *Gateway) handleRouteDetail(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	prefix := r.URL.Query().Get("prefix")
	if prefix == "" {
		http.Error(w, "prefix parameter required", http.StatusBadRequest)
		return
	}

	route, exists := g.cfg.Route[prefix]
	if !exists {
		http.Error(w, "unknown route: "+prefix, http.StatusNotFound)
		return
	}

	var providers []sel.ProviderStatus
	for _, provName := range route.ProviderNames() {
		if status := g.selector.ProviderDetail(provName); status != nil {
			providers = append(providers, *status)
		}
	}
	if providers == nil {
		providers = []sel.ProviderStatus{}
	}

	type routeModelDetail struct {
		Name          string   `json:"name"`
		SystemPrompt  string   `json:"system_prompt,omitempty"`
		UpstreamNames []string `json:"upstreams,omitempty"`
		Wildcard      bool     `json:"wildcard"`
		Pattern       string   `json:"pattern,omitempty"`
	}
	var exactModels []routeModelDetail
	for _, name := range route.PublicModels() {
		matched := route.MatchModel(name)
		if matched == nil {
			continue
		}
		row := routeModelDetail{Name: name, SystemPrompt: matched.SystemPrompt}
		for _, upstream := range matched.Upstreams {
			row.UpstreamNames = append(row.UpstreamNames, upstream.Provider+":"+upstream.UpstreamModel)
		}
		exactModels = append(exactModels, row)
	}
	var wildcardModels []routeModelDetail
	for _, wildcard := range route.CompiledWildcardModels() {
		row := routeModelDetail{
			Name:         wildcard.Pattern,
			SystemPrompt: wildcard.SystemPrompt,
			Wildcard:     true,
			Pattern:      wildcard.Pattern,
		}
		for _, upstream := range wildcard.Upstreams {
			row.UpstreamNames = append(row.UpstreamNames, upstream.Provider)
		}
		wildcardModels = append(wildcardModels, row)
	}

	resp := map[string]any{
		"prefix":          prefix,
		"protocol":        route.Protocol,
		"providers":       providers,
		"models":          route.PublicModels(),
		"exact_models":    exactModels,
		"wildcard_models": wildcardModels,
		"hook_count":      len(route.Hooks),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleMetricsStream serves SSE real-time metrics for dashboard charts.
func (g *Gateway) handleMetricsStream(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	flusher, ok := prepareSSEWriter(w)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	writeMetrics := func() {
		data := g.collectMetricsData()
		jsonData, err := json.Marshal(data)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", jsonData)
		flusher.Flush()
	}

	writeMetrics()

	for {
		select {
		case <-ticker.C:
			writeMetrics()
		case <-r.Context().Done():
			return
		}
	}
}

// collectMetricsData collects all metrics needed for dashboard charts.
func (g *Gateway) collectMetricsData() map[string]any {
	g.updateProviderMetrics(g.cfg)

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
		for _, met := range collectMetrics(collector) {
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
		for _, met := range collectMetrics(collector) {
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
		for _, met := range collectMetrics(collector) {
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
	for _, entry := range g.outputRates.Snapshot(time.Now()) {
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
		for _, met := range collectMetrics(collector) {
			row := quantileStat{
				Value: histogramQuantile(q, met.GetHistogram().GetBucket()),
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

	providerRequests := collectRequestStats(providerRequestCounter)
	routeRequests := collectRequestStats(routeRequestCounter)
	providerDurations := collectDurationStats(providerRequestDuration)
	routeDurations := collectDurationStats(routeRequestDuration)
	providerTokens := collectTokenStats(providerTokenCounter)
	routeTokens := collectTokenStats(routeTokenCounter)
	providerTTFT := collectQuantiles(providerStreamTTFT, 0.95)
	routeTTFT := collectQuantiles(routeStreamTTFT, 0.95)
	providerThroughput := collectQuantiles(providerCompletionThroughput, 0.99)
	routeThroughput := collectQuantiles(routeCompletionThroughput, 0.99)

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
		"realtime":                       g.dashboardStore.Snapshot(),
	}
}

func (g *Gateway) collectDashboardCounters() dashboardCounterSample {
	sample := dashboardCounterSample{
		Timestamp:    time.Now(),
		OutputByProv: make(map[string]float64),
		RouteReqs:    make(map[string]float64),
		RouteFails:   make(map[string]float64),
		RouteOutput:  make(map[string]float64),
	}

	for _, met := range collectMetrics(routeRequestCounter) {
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

	for _, met := range collectMetrics(routeTokenCounter) {
		sample.Tokens += met.GetCounter().GetValue()
	}

	for _, entry := range g.outputRates.Snapshot(sample.Timestamp) {
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

	for _, met := range collectMetrics(routeFailovers) {
		sample.Failovers += met.GetCounter().GetValue()
	}

	for _, met := range collectMetrics(routeStreamErrors) {
		sample.StreamErrors += met.GetCounter().GetValue()
	}

	return sample
}

// handleAPIKeysList returns all API keys (masked).
func (g *Gateway) handleAPIKeysList(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	type usageStats struct {
		TotalRequests    int64 `json:"total_requests"`
		SuccessRequests  int64 `json:"success_requests"`
		FailureRequests  int64 `json:"failure_requests"`
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	}

	usageByKey := map[string]*usageStats{}
	for _, met := range collectMetrics(apiKeyRequestCounter) {
		var (
			key    string
			status string
		)
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
	for _, met := range collectMetrics(apiKeyTokenCounter) {
		var (
			key string
			typ string
		)
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

	keys := make([]map[string]any, 0, len(g.cfg.APIKeys))
	for name := range g.cfg.APIKeys {
		usage := usageByKey[name]
		if usage == nil {
			usage = &usageStats{}
		}
		keys = append(keys, map[string]any{
			"name":  name,
			"usage": usage,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"keys": keys,
	})
}

// handleAPIKeysCreate generates a new API key.
func (g *Gateway) handleAPIKeysCreate(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// Generate a random API key
	key := generateAPIKey()

	// Store in config
	if g.cfg.APIKeys == nil {
		g.cfg.APIKeys = make(map[string]config.SecretString)
	}
	g.cfg.APIKeys[body.Name] = config.SecretString(key)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"name": body.Name,
		"key":  key, // Only return the key on creation
	})
}

// handleAPIKeysDelete removes an API key.
func (g *Gateway) handleAPIKeysDelete(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	if _, exists := g.cfg.APIKeys[body.Name]; !exists {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	delete(g.cfg.APIKeys, body.Name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

// generateAPIKey creates a random API key with "wk_" prefix.
func generateAPIKey() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const keyLen = 32

	b := make([]byte, keyLen)
	// crypto/rand.Read fills b with random bytes
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		// Fallback to time-based (shouldn't happen)
		for i := range b {
			b[i] = charset[i%len(charset)]
		}
	}

	// Convert random bytes to charset characters
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}

	return "wk_" + string(b)
}
