package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/wweir/warden/config"
	sel "github.com/wweir/warden/internal/selector"
)

const adminSSEHeartbeatInterval = 15 * time.Second

func (h *Handler) HandleAdminStatus(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	flusher, ok := prepareSSEWriter(w)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	h.WriteStatusSSE(w)
	flusher.Flush()

	for {
		select {
		case <-ticker.C:
			h.WriteStatusSSE(w)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (h *Handler) WriteStatusSSE(w http.ResponseWriter) {
	type providerInfo struct {
		sel.ProviderStatus
		Protocol            string   `json:"protocol"`
		CandidateProtocols  []string `json:"candidate_protocols"`
		SupportedProtocols  []string `json:"supported_protocols"`
		ConfiguredProtocols []string `json:"configured_protocols"`
	}
	type routeInfo struct {
		Prefix    string   `json:"prefix"`
		Protocol  string   `json:"protocol"`
		Providers []string `json:"providers"`
		Models    []string `json:"models"`
		HookCount int      `json:"hook_count"`
	}

	statuses := h.selector.ProviderStatuses()
	providers := make([]providerInfo, 0, len(statuses))
	for _, status := range statuses {
		provCfg := h.cfg.Provider[status.Name]
		if provCfg == nil {
			providers = append(providers, providerInfo{ProviderStatus: status})
			continue
		}
		providers = append(providers, providerInfo{
			ProviderStatus:      status,
			Protocol:            provCfg.Protocol,
			CandidateProtocols:  config.CandidateRouteProtocols(provCfg),
			SupportedProtocols:  config.SupportedRouteProtocols(provCfg),
			ConfiguredProtocols: config.SupportedRouteProtocols(provCfg),
		})
	}

	routes := make([]routeInfo, 0, len(h.cfg.Route))
	for prefix, route := range h.cfg.Route {
		routes = append(routes, routeInfo{
			Prefix:    prefix,
			Protocol:  route.ConfiguredProtocol(),
			Providers: route.ProviderNames(),
			Models:    route.PublicModels(),
			HookCount: len(route.Hooks),
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

func (h *Handler) HandleLogStream(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	flusher, ok := prepareSSEWriter(w)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := h.broadcaster.Subscribe()
	defer h.broadcaster.Unsubscribe(ch)
	heartbeat := time.NewTicker(adminSSEHeartbeatInterval)
	defer heartbeat.Stop()

	writeSSEComment(w, "stream-open")
	flusher.Flush()

	for _, rec := range h.broadcaster.Recent() {
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

func (h *Handler) HandleMetricsStream(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	flusher, ok := prepareSSEWriter(w)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	writeMetrics := func() {
		if h.collectMetricsData == nil {
			return
		}
		data := h.collectMetricsData()
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

func (h *Handler) HandleToolHookSuggestions(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(buildToolHookSuggestionsForRoute(h.broadcaster.Recent(), strings.TrimSpace(r.URL.Query().Get("route"))))
}

func (h *Handler) HandleRestart(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	if h.reloadFn == nil {
		http.Error(w, "reload not available", http.StatusInternalServerError)
		return
	}
	if err := h.reloadFn(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
