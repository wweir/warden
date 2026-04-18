package admin

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
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

	h.configMu.Lock()
	defer h.configMu.Unlock()

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

	if err = h.writeConfigFile(yamlData); err != nil {
		if errors.Is(err, errNoConfigPath) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, errConfigChangedExternally) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, "write config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

var (
	errNoConfigPath            = errors.New("no config file path configured")
	errConfigChangedExternally = errors.New("config file changed externally, please reload")
)

func (h *Handler) writeConfigFile(yamlData []byte) error {
	configPath := h.configPathValue()
	if configPath == "" {
		return errNoConfigPath
	}
	if currentHash := h.configHashValue(); currentHash != "" {
		current, readErr := os.ReadFile(configPath)
		if readErr == nil {
			if got := fmt.Sprintf("%x", sha256.Sum256(current)); got != currentHash {
				return errConfigChangedExternally
			}
		}
	}
	if err := os.WriteFile(configPath, yamlData, 0o644); err != nil {
		return err
	}
	h.setConfigHash(fmt.Sprintf("%x", sha256.Sum256(yamlData)))
	return nil
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

func cloneConfig(cfg *config.ConfigStruct) (*config.ConfigStruct, error) {
	currentData, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	var cloned config.ConfigStruct
	if err := json.Unmarshal(currentData, &cloned); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	return &cloned, nil
}

func marshalConfigYAML(cfg *config.ConfigStruct) ([]byte, error) {
	currentData, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal runtime config: %w", err)
	}

	var cfgMap map[string]any
	if err := json.Unmarshal(currentData, &cfgMap); err != nil {
		return nil, fmt.Errorf("decode runtime config: %w", err)
	}

	InjectSecrets(cfgMap, cfg)
	DropOAuthProviderAPIKey(cfgMap)
	NormalizeSecretConfigJSON(cfgMap)
	NormalizeProviderConfigJSON(cfgMap)
	NormalizePromptConfigJSON(cfgMap)

	yamlData, err := yaml.Marshal(cfgMap)
	if err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return yamlData, nil
}

func (h *Handler) marshalRuntimeConfigYAML() ([]byte, error) {
	return marshalConfigYAML(h.cfg)
}
