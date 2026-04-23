package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/wweir/warden/config"
	sel "github.com/wweir/warden/internal/selector"
)

func (h *Handler) HandleProviderHealth(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	provCfg, exists := h.cfg.Provider[body.Name]
	if !exists {
		http.Error(w, "unknown provider: "+body.Name, http.StatusNotFound)
		return
	}

	start := time.Now()
	_, rawModels, err := sel.FetchModels(r.Context(), provCfg)
	latency := time.Since(start)

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "error",
			"error":      err.Error(),
			"latency_ms": latency.Milliseconds(),
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":      "ok",
		"latency_ms":  latency.Milliseconds(),
		"model_count": len(rawModels),
	})
}

func (h *Handler) HandleProviderProtocolDetect(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
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
	provCfg := h.cfg.Provider[body.Name]
	if provCfg == nil {
		http.Error(w, "unknown provider: "+body.Name, http.StatusNotFound)
		return
	}

	displayProtocols := config.SupportedDisplayProtocols(provCfg)
	probe := sel.ProtocolProbe{}
	displayProtocols, probe = detectProviderDisplayProtocols(r.Context(), provCfg)
	if len(displayProtocols) == 0 {
		displayProtocols = config.SupportedDisplayProtocols(provCfg)
	}
	h.selector.SetDisplayProtocols(body.Name, displayProtocols, &probe)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":                 body.Name,
		"display_protocols":    displayProtocols,
		"candidate_protocols":  config.CandidateRouteProtocols(provCfg),
		"configured_protocols": config.SupportedRouteProtocols(provCfg),
		"probe":                probe,
	})
}

func (h *Handler) HandleProviderModelProtocolProbe(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var body struct {
		Name     string `json:"name"`
		Model    string `json:"model"`
		Protocol string `json:"protocol"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Name == "" || body.Model == "" || body.Protocol == "" {
		http.Error(w, "name, model, protocol are required", http.StatusBadRequest)
		return
	}
	provCfg := h.cfg.Provider[body.Name]
	if provCfg == nil {
		http.Error(w, "unknown provider: "+body.Name, http.StatusNotFound)
		return
	}

	probe := sel.ModelProtocolProbe{
		Model:     strings.TrimSpace(body.Model),
		Protocol:  strings.TrimSpace(body.Protocol),
		CheckedAt: time.Now(),
		Status:    "unsupported",
	}
	probe = probeProviderModelProtocol(r.Context(), provCfg, probe.Model, probe.Protocol)
	probe.Model = strings.TrimSpace(body.Model)
	probe.Protocol = strings.TrimSpace(body.Protocol)
	h.selector.UpsertModelProtocolProbe(body.Name, probe)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(probe)
}

func (h *Handler) HandleProviderDetail(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "name parameter required", http.StatusBadRequest)
		return
	}

	provCfg, exists := h.cfg.Provider[name]
	if !exists {
		http.Error(w, "unknown provider: "+name, http.StatusNotFound)
		return
	}

	status := h.selector.ProviderDetail(name)
	if status == nil {
		status = &sel.ProviderStatus{}
	}
	models := h.selector.ProviderModels(name)
	if models == nil {
		models = []json.RawMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":                  name,
		"url":                   provCfg.URL,
		"family":                provCfg.Family,
		"protocol":              provCfg.Protocol,
		"backend":               provCfg.Backend,
		"backend_provider":      provCfg.BackendProvider,
		"candidate_protocols":   config.CandidateRouteProtocols(provCfg),
		"configured_protocols":  config.SupportedRouteProtocols(provCfg),
		"supported_protocols":   config.SupportedRouteProtocols(provCfg),
		"display_protocols":     status.DisplayProtocols,
		"last_protocol_probe":   status.LastProtocolProbe,
		"timeout":               provCfg.Timeout,
		"has_api_key":           provCfg.APIKey.Value() != "",
		"responses_to_chat":     provCfg.ResponsesToChat,
		"anthropic_to_chat":     provCfg.AnthropicToChat,
		"models":                models,
		"model_protocol_probes": h.selector.ModelProtocolProbes(name),
		"status":                status,
	})
}

func (h *Handler) HandleProviderSuppress(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
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

	h.configMu.Lock()
	defer h.configMu.Unlock()

	prov, exists := h.cfg.Provider[body.Name]
	if !exists {
		http.Error(w, "unknown provider: "+body.Name, http.StatusNotFound)
		return
	}
	if !h.selector.SetManualSuppress(body.Name, body.Suppress) {
		http.Error(w, "failed to update provider", http.StatusInternalServerError)
		return
	}

	// Persist disabled state to config file
	prov.Disabled = body.Suppress
	var warning string
	yamlData, err := h.marshalRuntimeConfigYAML()
	if err != nil {
		warning = "runtime state updated but config persist failed: " + err.Error()
	} else if err := h.writeConfigFile(yamlData); err != nil {
		warning = "runtime state updated but config persist failed: " + err.Error()
	}

	resp := map[string]any{
		"name":              body.Name,
		"manual_suppressed": body.Suppress,
	}
	if warning != "" {
		resp["warning"] = warning
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
