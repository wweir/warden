package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/wweir/warden/config"
)

func (h *Handler) HandleAPIKeysList(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	keys := []map[string]any{}
	if h.listAPIKeys != nil {
		keys = h.listAPIKeys()
	} else {
		for prefix, route := range h.cfg.Route {
			for name := range route.CloneAPIKeys() {
				keys = append(keys, map[string]any{
					"route": prefix,
					"name":  name,
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
}

func (h *Handler) HandleAPIKeysCreate(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var body struct {
		Route string `json:"route"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	body.Route = strings.TrimSpace(body.Route)
	body.Name = strings.TrimSpace(body.Name)
	if body.Route == "" {
		http.Error(w, "route is required", http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	route, ok := h.cfg.Route[body.Route]
	if !ok || route == nil {
		http.Error(w, "route not found", http.StatusNotFound)
		return
	}

	key, err := GenerateAPIKey()
	if err != nil {
		http.Error(w, "generate api key: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if ok := route.AddAPIKey(body.Name, config.SecretString(key)); !ok {
		http.Error(w, "key already exists", http.StatusConflict)
		return
	}
	if err := h.cfg.Validate(); err != nil {
		_, _ = route.DeleteAPIKey(body.Name)
		http.Error(w, "invalid config: "+err.Error(), http.StatusBadRequest)
		return
	}
	yamlData, err := h.marshalRuntimeConfigYAML()
	if err != nil {
		_, _ = route.DeleteAPIKey(body.Name)
		http.Error(w, "encode config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.writeConfigFile(yamlData); err != nil {
		_, _ = route.DeleteAPIKey(body.Name)
		status := http.StatusInternalServerError
		if err == errNoConfigPath {
			status = http.StatusBadRequest
		}
		if err == errConfigChangedExternally {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"route": body.Route,
		"name":  body.Name,
		"key":   key,
	})
}

func (h *Handler) HandleAPIKeysDelete(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var body struct {
		Route string `json:"route"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	body.Route = strings.TrimSpace(body.Route)
	body.Name = strings.TrimSpace(body.Name)
	if body.Route == "" {
		http.Error(w, "route is required", http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	route, ok := h.cfg.Route[body.Route]
	if !ok || route == nil {
		http.Error(w, "route not found", http.StatusNotFound)
		return
	}
	previous, exists := route.DeleteAPIKey(body.Name)
	if !exists {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	if err := h.cfg.Validate(); err != nil {
		route.SetAPIKey(body.Name, previous)
		http.Error(w, "invalid config: "+err.Error(), http.StatusBadRequest)
		return
	}
	yamlData, err := h.marshalRuntimeConfigYAML()
	if err != nil {
		route.SetAPIKey(body.Name, previous)
		http.Error(w, "encode config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.writeConfigFile(yamlData); err != nil {
		route.SetAPIKey(body.Name, previous)
		status := http.StatusInternalServerError
		if err == errNoConfigPath {
			status = http.StatusBadRequest
		}
		if err == errConfigChangedExternally {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
