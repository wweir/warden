package gateway

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
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

	// internal mux for admin sub-routing
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		g.handleAdminStatus(w, r, nil)
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
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fp := strings.TrimPrefix(r.URL.Path, "/")
		if fp != "" {
			if _, err := adminFS.(fs.ReadFileFS).ReadFile(fp); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback: serve index.html for non-asset paths
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
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

// handleAdminStatus returns provider statuses, routes, and MCP server info.
func (g *Gateway) handleAdminStatus(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"providers": providers,
		"routes":    routes,
		"mcp":       mcps,
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
			sanitizeConfigJSON(newMap, currentMap)
		}
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

// sanitizeConfigJSON replaces redacted placeholder values with their current values to prevent overwriting secrets.
func sanitizeConfigJSON(newCfg map[string]any, currentCfg map[string]any) {
	for k, v := range newCfg {
		if s, ok := v.(string); ok && s == redactedPlaceholder {
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

// adminFSWithFallback wraps an fs.FS to return index.html for non-existent files (SPA support).
func hasFile(fsys fs.FS, name string) bool {
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		name = "."
	}
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	f.Close()
	return true
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
	_, rawModels, err := fetchModels(provCfg)
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
		"name":           name,
		"url":            provCfg.URL,
		"protocol":       provCfg.Protocol,
		"timeout":        provCfg.Timeout,
		"has_api_key":    provCfg.APIKey.Value() != "",
		"model_aliases":  provCfg.ModelAliases,
		"models":         models,
		"status":         status,
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

	var providers []ProviderStatus
	for _, provName := range route.Providers {
		if status := g.selector.ProviderDetail(provName); status != nil {
			providers = append(providers, *status)
		}
	}
	if providers == nil {
		providers = []ProviderStatus{}
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
	}

	var tools []toolInfo
	connected := false
	if client != nil {
		connected = client.IsRunning()
		for _, t := range client.CachedTools() {
			tools = append(tools, toolInfo{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
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
