package admin

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/julienschmidt/httprouter"
	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/config"
)

const configFileMode = 0o600

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
		NormalizeSecretConfigJSON(newMap)
		NormalizeProviderConfigJSON(newMap)
		NormalizePromptConfigJSON(newMap)
	}

	configData, err := marshalConfigMap(cfgMap)
	if err != nil {
		http.Error(w, "encode config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err = h.writeConfigFile(configData); err != nil {
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

func (h *Handler) writeConfigFile(configData []byte) error {
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
	if err := os.WriteFile(configPath, configData, configFileMode); err != nil {
		return err
	}
	if err := os.Chmod(configPath, configFileMode); err != nil {
		return err
	}
	h.setConfigHash(fmt.Sprintf("%x", sha256.Sum256(configData)))
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

func marshalConfigMap(cfgMap any) ([]byte, error) {
	normalizeTOMLNumbers(cfgMap)
	tomlData, err := toml.Marshal(cfgMap)
	if err != nil {
		return nil, fmt.Errorf("encode toml config: %w", err)
	}
	return tomlData, nil
}

func normalizeTOMLNumbers(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if normalized, ok := normalizeTOMLNumberValue(child); ok {
				typed[key] = normalized
				continue
			}
			normalizeTOMLNumbers(child)
		}
	case []any:
		for i, child := range typed {
			if normalized, ok := normalizeTOMLNumberValue(child); ok {
				typed[i] = normalized
				continue
			}
			normalizeTOMLNumbers(child)
		}
	}
}

func normalizeTOMLNumberValue(value any) (any, bool) {
	number, ok := value.(float64)
	if !ok || math.Trunc(number) != number {
		return nil, false
	}
	return int64(number), true
}

func marshalConfigFile(cfg *config.ConfigStruct) ([]byte, error) {
	currentData, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal runtime config: %w", err)
	}

	var cfgMap map[string]any
	if err := json.Unmarshal(currentData, &cfgMap); err != nil {
		return nil, fmt.Errorf("decode runtime config: %w", err)
	}

	InjectSecrets(cfgMap, cfg)
	NormalizeSecretConfigJSON(cfgMap)
	NormalizeProviderConfigJSON(cfgMap)
	NormalizePromptConfigJSON(cfgMap)

	return marshalConfigMap(cfgMap)
}

func (h *Handler) marshalConfigFile(cfg *config.ConfigStruct) ([]byte, error) {
	return marshalConfigFile(cfg)
}

func (h *Handler) marshalRuntimeConfigFile() ([]byte, error) {
	return h.marshalConfigFile(h.cfg)
}
