package gateway

import (
	"bytes"
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
	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
	sel "github.com/wweir/warden/internal/selector"
	"github.com/wweir/warden/web"
	"gopkg.in/yaml.v3"
)

const redactedPlaceholder = "__REDACTED__"

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
	mux.HandleFunc("POST /api/restart", func(w http.ResponseWriter, r *http.Request) {
		g.handleRestart(w, r, nil)
	})
	mux.HandleFunc("POST /api/providers/health", func(w http.ResponseWriter, r *http.Request) {
		g.handleProviderHealth(w, r, nil)
	})
	mux.HandleFunc("GET /api/providers/detail", func(w http.ResponseWriter, r *http.Request) {
		g.handleProviderDetail(w, r, nil)
	})
	mux.HandleFunc("POST /api/config/validate", func(w http.ResponseWriter, r *http.Request) {
		g.handleConfigValidate(w, r, nil)
	})
	mux.HandleFunc("GET /api/routes/detail", func(w http.ResponseWriter, r *http.Request) {
		g.handleRouteDetail(w, r, nil)
	})
	mux.HandleFunc("GET /api/mcp/detail", func(w http.ResponseWriter, r *http.Request) {
		g.handleMcpDetail(w, r, nil)
	})
	mux.HandleFunc("POST /api/mcp/tool-call", func(w http.ResponseWriter, r *http.Request) {
		g.handleMcpToolCall(w, r, nil)
	})
	mux.HandleFunc("POST /api/mcp/tool-toggle", func(w http.ResponseWriter, r *http.Request) {
		g.handleMcpToolToggle(w, r, nil)
	})
	mux.HandleFunc("GET /api/metrics/stream", func(w http.ResponseWriter, r *http.Request) {
		g.handleMetricsStream(w, r, nil)
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
		if !ok || user != "admin" || subtle.ConstantTimeCompare([]byte(pass), []byte(g.cfg.AdminPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Warden Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r, ps)
	}
}

// handleAdminStatus streams provider statuses, routes, and MCP server info via SSE.
func (g *Gateway) handleAdminStatus(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
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
	type mcpStatus struct {
		Name      string `json:"name"`
		ToolCount int    `json:"tool_count"`
		Connected bool   `json:"connected"`
	}
	type routeInfo struct {
		Prefix    string   `json:"prefix"`
		Providers []string `json:"providers"`
		Tools     []string `json:"tools"`
	}

	providers := g.selector.ProviderStatuses()

	var routes []routeInfo
	for prefix, r := range g.cfg.Route {
		routes = append(routes, routeInfo{
			Prefix:    prefix,
			Providers: r.Providers,
			Tools:     r.Tools,
		})
	}
	slices.SortFunc(routes, func(a, b routeInfo) int {
		return strings.Compare(a.Prefix, b.Prefix)
	})

	var mcps []mcpStatus
	for name, client := range g.mcpClients {
		mcps = append(mcps, mcpStatus{
			Name:      name,
			ToolCount: len(client.CachedTools()),
			Connected: client.IsRunning(),
		})
	}
	slices.SortFunc(mcps, func(a, b mcpStatus) int {
		return strings.Compare(a.Name, b.Name)
	})

	data, _ := json.Marshal(map[string]any{
		"providers": providers,
		"routes":    routes,
		"mcp":       mcps,
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
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := g.broadcaster.Subscribe()
	defer g.broadcaster.Unsubscribe(ch)

	// send recent history first
	for _, rec := range g.broadcaster.Recent() {
		writeSSE(w, rec)
	}
	w.(http.Flusher).Flush()

	for {
		select {
		case rec := <-ch:
			writeSSE(w, rec)
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// writeSSE writes a single SSE event with JSON data.
func writeSSE(w http.ResponseWriter, r reqlog.Record) {
	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// injectSecrets writes real secret values from cfg into cfgMap,
// since json.Marshal masks deferlog.Secret fields as "***".
func injectSecrets(cfgMap map[string]any, cfg *config.ConfigStruct) {
	if cfg.AdminPassword != "" {
		cfgMap["admin_password"] = cfg.AdminPassword
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
		"name":          name,
		"url":           provCfg.URL,
		"protocol":      provCfg.Protocol,
		"timeout":       provCfg.Timeout,
		"has_api_key":   provCfg.APIKey.Value() != "",
		"model_aliases": provCfg.ModelAliases,
		"models":        models,
		"status":        status,
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

// handleMcpToolCall invokes a specific tool on an MCP server.
func (g *Gateway) handleMcpToolCall(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var body struct {
		MCP       string          `json:"mcp"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if body.MCP == "" || body.Tool == "" {
		http.Error(w, "mcp and tool are required", http.StatusBadRequest)
		return
	}

	client, exists := g.mcpClients[body.MCP]
	if !exists {
		http.Error(w, "unknown MCP server: "+body.MCP, http.StatusNotFound)
		return
	}
	if !client.IsRunning() {
		http.Error(w, "MCP server not connected: "+body.MCP, http.StatusServiceUnavailable)
		return
	}

	// validate tool name exists in cached tools
	toolFound := false
	for _, t := range client.CachedTools() {
		if t.Name == body.Tool {
			toolFound = true
			break
		}
	}
	if !toolFound {
		http.Error(w, "unknown tool: "+body.Tool, http.StatusNotFound)
		return
	}

	start := time.Now()
	result, err := client.CallTool(r.Context(), body.Tool, body.Arguments)
	duration := time.Since(start)

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{
			"status":      "error",
			"error":       err.Error(),
			"duration_ms": duration.Milliseconds(),
		})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"status":      "ok",
		"result":      result,
		"duration_ms": duration.Milliseconds(),
	})
}

// handleRouteDetail returns detailed information about a single route,
// including associated provider statistics and MCP tool statuses.
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
	for _, provName := range route.Providers {
		if status := g.selector.ProviderDetail(provName); status != nil {
			providers = append(providers, *status)
		}
	}
	if providers == nil {
		providers = []sel.ProviderStatus{}
	}

	type mcpToolStatus struct {
		Name      string `json:"name"`
		Connected bool   `json:"connected"`
		ToolCount int    `json:"tool_count"`
	}
	var tools []mcpToolStatus
	for _, toolName := range route.Tools {
		ts := mcpToolStatus{Name: toolName}
		if client, ok := g.mcpClients[toolName]; ok {
			ts.Connected = client.IsRunning()
			ts.ToolCount = len(client.CachedTools())
		}
		tools = append(tools, ts)
	}
	if tools == nil {
		tools = []mcpToolStatus{}
	}

	resp := map[string]any{
		"prefix":    prefix,
		"providers": providers,
		"tools":     tools,
	}
	if len(route.SystemPrompts) > 0 {
		resp["system_prompts"] = route.SystemPrompts
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleMcpDetail returns detailed information about a single MCP server.
func (g *Gateway) handleMcpDetail(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "name parameter required", http.StatusBadRequest)
		return
	}

	mcpCfg, exists := g.cfg.MCP[name]
	if !exists {
		http.Error(w, "unknown MCP server: "+name, http.StatusNotFound)
		return
	}

	client := g.mcpClients[name]

	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema any    `json:"input_schema,omitempty"`
		Disabled    bool   `json:"disabled"`
	}

	var tools []toolInfo
	connected := false
	if client != nil {
		connected = client.IsRunning()
		for _, t := range client.CachedTools() {
			disabled := false
			if mcpCfg.Tools != nil {
				if tc, ok := mcpCfg.Tools[t.Name]; ok {
					disabled = tc.Disabled
				}
			}
			tools = append(tools, toolInfo{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
				Disabled:    disabled,
			})
		}
	}
	if tools == nil {
		tools = []toolInfo{}
	}

	// determine which routes use this MCP server
	var routes []string
	for prefix, route := range g.cfg.Route {
		for _, toolName := range route.Tools {
			if toolName == name {
				routes = append(routes, prefix)
				break
			}
		}
	}

	resp := map[string]any{
		"name":      name,
		"command":   mcpCfg.Command,
		"args":      mcpCfg.Args,
		"connected": connected,
		"tools":     tools,
		"routes":    routes,
	}
	if mcpCfg.SSH != "" {
		resp["ssh"] = mcpCfg.SSH
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleMcpToolToggle enables or disables a specific tool within an MCP server.
// The disabled state is persisted in the in-memory MCPConfig.Tools map.
// To make it permanent, save the config via PUT /api/config.
func (g *Gateway) handleMcpToolToggle(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var body struct {
		MCP      string `json:"mcp"`
		Tool     string `json:"tool"`
		Disabled bool   `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.MCP == "" || body.Tool == "" {
		http.Error(w, "mcp and tool are required", http.StatusBadRequest)
		return
	}

	mcpCfg, exists := g.cfg.MCP[body.MCP]
	if !exists {
		http.Error(w, "unknown MCP server: "+body.MCP, http.StatusNotFound)
		return
	}

	if mcpCfg.Tools == nil {
		mcpCfg.Tools = make(map[string]*config.ToolConfig)
	}
	tc, ok := mcpCfg.Tools[body.Tool]
	if !ok {
		tc = &config.ToolConfig{}
		mcpCfg.Tools[body.Tool] = tc
	}
	tc.Disabled = body.Disabled

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"mcp":      body.MCP,
		"tool":     body.Tool,
		"disabled": body.Disabled,
	})
}

// handleMetricsStream serves SSE real-time metrics for dashboard charts.
func (g *Gateway) handleMetricsStream(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			data := g.collectMetricsData()
			jsonData, err := json.Marshal(data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// collectMetricsData collects all metrics needed for dashboard charts.
func (g *Gateway) collectMetricsData() map[string]any {
	g.updateProviderMetrics(g.cfg)

	type requestStat struct {
		Route    string `json:"route"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Endpoint string `json:"endpoint"`
		Status   string `json:"status"`
		Value    int    `json:"value"`
	}
	var requests []requestStat
	for _, met := range collectMetrics(requestCounter) {
		req := requestStat{Value: int(met.GetCounter().GetValue())}
		for _, l := range met.GetLabel() {
			switch l.GetName() {
			case "route":
				req.Route = l.GetValue()
			case "provider":
				req.Provider = l.GetValue()
			case "model":
				req.Model = l.GetValue()
			case "endpoint":
				req.Endpoint = l.GetValue()
			case "status":
				req.Status = l.GetValue()
			}
		}
		requests = append(requests, req)
	}

	type durationBucket struct {
		Route    string  `json:"route"`
		Provider string  `json:"provider"`
		Model    string  `json:"model"`
		Endpoint string  `json:"endpoint"`
		Le       float64 `json:"le"`
		Value    int     `json:"value"`
	}
	var durations []durationBucket
	for _, met := range collectMetrics(requestDuration) {
		for _, b := range met.GetHistogram().GetBucket() {
			if b.GetUpperBound() == float64(1<<63-1) {
				continue // skip +Inf bucket
			}
			db := durationBucket{Le: b.GetUpperBound(), Value: int(b.GetCumulativeCount())}
			for _, l := range met.GetLabel() {
				switch l.GetName() {
				case "route":
					db.Route = l.GetValue()
				case "provider":
					db.Provider = l.GetValue()
				case "model":
					db.Model = l.GetValue()
				case "endpoint":
					db.Endpoint = l.GetValue()
				}
			}
			durations = append(durations, db)
		}
	}

	type tokenStat struct {
		Provider string  `json:"provider"`
		Model    string  `json:"model"`
		Type     string  `json:"type"`
		Value    float64 `json:"value"`
	}
	var tokens []tokenStat
	for _, met := range collectMetrics(tokenCounter) {
		ts := tokenStat{Value: met.GetCounter().GetValue()}
		for _, l := range met.GetLabel() {
			switch l.GetName() {
			case "provider":
				ts.Provider = l.GetValue()
			case "model":
				ts.Model = l.GetValue()
			case "type":
				ts.Type = l.GetValue()
			}
		}
		tokens = append(tokens, ts)
	}

	type tokenRateStat struct {
		Route    string  `json:"route"`
		Provider string  `json:"provider"`
		Model    string  `json:"model"`
		Endpoint string  `json:"endpoint"`
		Type     string  `json:"type"`
		Value    float64 `json:"value"`
	}
	var rates []tokenRateStat
	for _, met := range collectMetrics(tokenRate) {
		rs := tokenRateStat{Value: met.GetGauge().GetValue()}
		for _, l := range met.GetLabel() {
			switch l.GetName() {
			case "route":
				rs.Route = l.GetValue()
			case "provider":
				rs.Provider = l.GetValue()
			case "model":
				rs.Model = l.GetValue()
			case "endpoint":
				rs.Endpoint = l.GetValue()
			case "type":
				rs.Type = l.GetValue()
			}
		}
		rates = append(rates, rs)
	}

	type quantileStat struct {
		Route    string  `json:"route"`
		Provider string  `json:"provider"`
		Model    string  `json:"model"`
		Endpoint string  `json:"endpoint"`
		Value    float64 `json:"value"`
		Count    uint64  `json:"count"`
	}

	var ttftP95 []quantileStat
	for _, met := range collectMetrics(streamTTFT) {
		entry := quantileStat{
			Value: histogramQuantile(0.95, met.GetHistogram().GetBucket()),
			Count: met.GetHistogram().GetSampleCount(),
		}
		for _, l := range met.GetLabel() {
			switch l.GetName() {
			case "route":
				entry.Route = l.GetValue()
			case "provider":
				entry.Provider = l.GetValue()
			case "model":
				entry.Model = l.GetValue()
			case "endpoint":
				entry.Endpoint = l.GetValue()
			}
		}
		ttftP95 = append(ttftP95, entry)
	}

	var throughputP99 []quantileStat
	for _, met := range collectMetrics(completionThroughput) {
		entry := quantileStat{
			Value: histogramQuantile(0.99, met.GetHistogram().GetBucket()),
			Count: met.GetHistogram().GetSampleCount(),
		}
		for _, l := range met.GetLabel() {
			switch l.GetName() {
			case "route":
				entry.Route = l.GetValue()
			case "provider":
				entry.Provider = l.GetValue()
			case "model":
				entry.Model = l.GetValue()
			case "endpoint":
				entry.Endpoint = l.GetValue()
			}
		}
		throughputP99 = append(throughputP99, entry)
	}

	return map[string]any{
		"requests_total":        requests,
		"request_duration":      durations,
		"tokens_total":          tokens,
		"token_rate":            rates,
		"stream_ttft_p95_ms":    ttftP95,
		"throughput_p99_tokens": throughputP99,
	}
}
