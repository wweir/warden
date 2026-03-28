package admin

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/julienschmidt/httprouter"
	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/config"
	"gopkg.in/yaml.v3"
)

func (h *Handler) HandleAdminConfigSource(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"source_type": map[string]any{
			"file": h.configPathValue() != "",
		},
		"config_path": h.configPathValue(),
		"config_hash": h.configHashValue(),
	})
}

func (h *Handler) HandleAdminConfigGet(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	data, err := json.Marshal(h.cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var cfgMap map[string]any
	_ = json.Unmarshal(data, &cfgMap)
	MaskAPIKeys(cfgMap)

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(cfgMap)
}

func (h *Handler) HandleAdminConfigPut(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var err error
	defer func() { deferlog.DebugError(err, "admin config update") }()

	configPath := h.configPathValue()
	if configPath == "" {
		http.Error(w, "no config file path configured", http.StatusBadRequest)
		return
	}

	var body json.RawMessage
	if err = json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	var cfgMap any
	if err = json.Unmarshal(body, &cfgMap); err != nil {
		http.Error(w, "invalid config: "+err.Error(), http.StatusBadRequest)
		return
	}

	if newMap, ok := cfgMap.(map[string]any); ok {
		currentData, _ := json.Marshal(h.cfg)
		var currentMap map[string]any
		if json.Unmarshal(currentData, &currentMap) == nil {
			InjectSecrets(currentMap, h.cfg)
			SanitizeConfigJSON(newMap, currentMap)
		}
		DropOAuthProviderAPIKey(newMap)
		NormalizeSecretConfigJSON(newMap)
		NormalizeProviderConfigJSON(newMap)
		NormalizePromptConfigJSON(newMap)
	}

	yamlData, err := yaml.Marshal(cfgMap)
	if err != nil {
		http.Error(w, "encode config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if currentHash := h.configHashValue(); currentHash != "" {
		current, readErr := os.ReadFile(configPath)
		if readErr == nil {
			if got := fmt.Sprintf("%x", sha256.Sum256(current)); got != currentHash {
				http.Error(w, "config file changed externally, please reload", http.StatusConflict)
				return
			}
		}
	}

	if err = os.WriteFile(configPath, yamlData, 0o644); err != nil {
		http.Error(w, "write config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.setConfigHash(fmt.Sprintf("%x", sha256.Sum256(yamlData)))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) HandleConfigValidate(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var cfgStruct config.ConfigStruct
	if err := json.NewDecoder(r.Body).Decode(&cfgStruct); err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"valid": false, "error": "invalid JSON: " + err.Error()})
		return
	}

	if err := cfgStruct.Validate(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"valid": false, "error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"valid": true})
}
