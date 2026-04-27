package admin

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/julienschmidt/httprouter"
	"github.com/wweir/warden/config"
	sel "github.com/wweir/warden/internal/selector"
)

func (h *Handler) HandleRouteDetail(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	prefix := r.URL.Query().Get("prefix")
	if prefix == "" {
		http.Error(w, "prefix parameter required", http.StatusBadRequest)
		return
	}

	route, exists := h.cfg.Route[prefix]
	if !exists {
		http.Error(w, "unknown route: "+prefix, http.StatusNotFound)
		return
	}

	providers := make([]sel.ProviderStatus, 0, len(route.ProviderNames()))
	for _, provName := range route.ProviderNames() {
		if status := h.selector.ProviderDetail(provName); status != nil {
			providers = append(providers, *status)
		}
	}

	type routeModelDetail struct {
		Name          string   `json:"name"`
		Targets       []string `json:"targets,omitempty"`
		MatchedModels []string `json:"matched_models,omitempty"`
		PromptEnabled bool     `json:"prompt_enabled"`
		SystemPrompt  string   `json:"system_prompt,omitempty"`
		Wildcard      bool     `json:"wildcard"`
		Pattern       string   `json:"pattern,omitempty"`
	}

	exactModels := make([]routeModelDetail, 0, len(route.PublicModels()))
	for _, name := range route.PublicModels() {
		matched := route.MatchModel(name)
		if matched == nil || matched.Wildcard {
			continue
		}
		row := routeModelDetail{
			Name:          name,
			PromptEnabled: matched.PromptEnabled,
			SystemPrompt:  matched.SystemPrompt,
			Targets:       make([]string, 0, len(matched.Upstreams)),
		}
		for _, upstream := range matched.Upstreams {
			row.Targets = append(row.Targets, upstream.Provider+":"+upstream.UpstreamModel)
		}
		exactModels = append(exactModels, row)
	}

	wildcardModels := make([]routeModelDetail, 0, len(route.CompiledWildcardModels()))
	wildcardMatches := h.routeWildcardMatches(route)
	for _, wildcard := range route.CompiledWildcardModels() {
		row := routeModelDetail{
			Name:          wildcard.Pattern,
			PromptEnabled: wildcard.PromptEnabled,
			SystemPrompt:  wildcard.SystemPrompt,
			Wildcard:      true,
			Pattern:       wildcard.Pattern,
			Targets:       make([]string, 0, len(wildcard.Upstreams)),
			MatchedModels: wildcardMatches[wildcard.Pattern],
		}
		for _, upstream := range wildcard.Upstreams {
			row.Targets = append(row.Targets, upstream.Provider)
		}
		wildcardModels = append(wildcardModels, row)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"prefix":            prefix,
		"protocol":          route.ConfiguredProtocol(),
		"service_protocols": route.EffectiveServiceProtocols(),
		"providers":         providers,
		"models":            route.PublicModels(),
		"exact_models":      exactModels,
		"wildcard_models":   wildcardModels,
		"hook_count":        len(route.Hooks),
	})
}

func (h *Handler) routeWildcardMatches(route *config.RouteConfig) map[string][]string {
	out := make(map[string][]string)
	seen := make(map[string]map[string]bool)

	for _, raw := range h.selector.Models(route) {
		id := routeModelID(raw)
		if id == "" {
			continue
		}
		matched := route.MatchModel(id)
		if matched == nil || !matched.Wildcard || matched.Pattern == "" {
			continue
		}
		if !route.MatchWildcardModel(matched.Pattern, id) {
			continue
		}
		if seen[matched.Pattern] == nil {
			seen[matched.Pattern] = make(map[string]bool)
		}
		if seen[matched.Pattern][id] {
			continue
		}
		seen[matched.Pattern][id] = true
		out[matched.Pattern] = append(out[matched.Pattern], id)
	}

	for pattern := range out {
		slices.Sort(out[pattern])
	}
	return out
}

func routeModelID(raw json.RawMessage) string {
	var entry struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &entry)
	return entry.ID
}
