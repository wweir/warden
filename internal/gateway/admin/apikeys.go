package admin

import (
	"encoding/json"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/wweir/warden/config"
)

func (h *Handler) HandleAPIKeysList(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	keys := make([]map[string]any, 0, len(h.cfg.APIKeys))
	if h.listAPIKeys != nil {
		keys = h.listAPIKeys()
	} else {
		for name := range h.cfg.APIKeys {
			keys = append(keys, map[string]any{"name": name})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
}

func (h *Handler) HandleAPIKeysCreate(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
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

	key := GenerateAPIKey()
	if h.cfg.APIKeys == nil {
		h.cfg.APIKeys = make(map[string]config.SecretString)
	}
	h.cfg.APIKeys[body.Name] = config.SecretString(key)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"name": body.Name,
		"key":  key,
	})
}

func (h *Handler) HandleAPIKeysDelete(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
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
	if _, exists := h.cfg.APIKeys[body.Name]; !exists {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	delete(h.cfg.APIKeys, body.Name)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
