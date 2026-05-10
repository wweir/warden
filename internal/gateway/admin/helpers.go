package admin

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
)

const RedactedPlaceholder = "__REDACTED__"
const clearProviderAPIKeyMarker = "__clear_api_key__"

func writeSSE(w http.ResponseWriter, r reqlog.Record) {
	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func prepareSSEWriter(w http.ResponseWriter) (http.Flusher, bool) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	return flusher, ok
}

func writeSSEComment(w http.ResponseWriter, comment string) {
	if comment == "" {
		comment = "keepalive"
	}
	fmt.Fprintf(w, ": %s\n\n", comment)
}

// MaskAPIKeys recursively masks api_key and admin_password fields.
func MaskAPIKeys(m map[string]any) {
	for k, v := range m {
		if k == "api_keys" {
			if keys, ok := v.(map[string]any); ok {
				for name, raw := range keys {
					if s, ok := raw.(string); ok && s != "" {
						keys[name] = RedactedPlaceholder
					}
				}
			}
		}
		if k == "api_key" || k == "admin_password" {
			if s, ok := v.(string); ok && s != "" {
				m[k] = RedactedPlaceholder
			}
		}
		if sub, ok := v.(map[string]any); ok {
			MaskAPIKeys(sub)
		}
	}
}

// InjectSecrets writes plaintext secret values from cfg into cfgMap so masked
// fields can be restored before a single storage-format normalization pass.
func InjectSecrets(cfgMap map[string]any, cfg *config.ConfigStruct) {
	if cfg.AdminPassword != "" {
		cfgMap["admin_password"] = cfg.AdminPassword.Value()
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
	routeMap, _ := cfgMap["route"].(map[string]any)
	for name, route := range cfg.Route {
		apiKeys := route.CloneAPIKeys()
		if len(apiKeys) == 0 {
			continue
		}
		rm, ok := routeMap[name].(map[string]any)
		if !ok {
			continue
		}
		apiKeysMap, _ := rm["api_keys"].(map[string]any)
		if apiKeysMap == nil {
			continue
		}
		for keyName, keyValue := range apiKeys {
			if keyValue != "" {
				apiKeysMap[keyName] = keyValue.Value()
			}
		}
	}
}

// SanitizeConfigJSON replaces redacted placeholder values with their current values to prevent overwriting secrets.
func SanitizeConfigJSON(newCfg map[string]any, currentCfg map[string]any) {
	for k, v := range newCfg {
		if s, ok := v.(string); ok && (s == RedactedPlaceholder || s == "***") {
			if current, exists := currentCfg[k]; exists {
				newCfg[k] = current
			}
		}
		if sub, ok := v.(map[string]any); ok {
			if csub, ok := currentCfg[k].(map[string]any); ok {
				SanitizeConfigJSON(sub, csub)
			}
		}
	}

	preserveProviderSecrets(newCfg, currentCfg)
}

func preserveProviderSecrets(newCfg map[string]any, currentCfg map[string]any) {
	newProviders, ok := newCfg["provider"].(map[string]any)
	if !ok {
		return
	}
	currentProviders, _ := currentCfg["provider"].(map[string]any)
	for name, raw := range newProviders {
		newProvider, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		currentProvider, _ := currentProviders[name].(map[string]any)
		if currentProvider == nil {
			continue
		}
		preserveProviderAPIKey(newProvider, currentProvider)
	}
}

func preserveProviderAPIKey(newProvider map[string]any, currentProvider map[string]any) {
	if clearAPIKey, _ := newProvider[clearProviderAPIKeyMarker].(bool); clearAPIKey {
		return
	}
	currentAPIKey, _ := currentProvider["api_key"].(string)
	if strings.TrimSpace(currentAPIKey) == "" {
		return
	}

	if _, hasAPIKey := newProvider["api_key"]; hasAPIKey {
		return
	}

	if command, ok := newProvider["api_key_command"].(string); ok && strings.TrimSpace(command) != "" {
		return
	}
	if configDir, ok := newProvider["config_dir"].(string); ok && strings.TrimSpace(configDir) != "" {
		return
	}

	newProvider["api_key"] = currentAPIKey
}

func NormalizeSecretConfigJSON(cfgMap map[string]any) {
	if secret, ok := cfgMap["admin_password"].(string); ok && secret != "" {
		cfgMap["admin_password"] = config.NormalizeSecretStorage(secret)
	}

	providerMap, _ := cfgMap["provider"].(map[string]any)
	for _, raw := range providerMap {
		providerCfg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		secret, ok := providerCfg["api_key"].(string)
		if !ok || secret == "" {
			continue
		}
		providerCfg["api_key"] = config.NormalizeSecretStorage(secret)
	}

	routeMap, _ := cfgMap["route"].(map[string]any)
	for _, raw := range routeMap {
		routeCfg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		apiKeysMap, _ := routeCfg["api_keys"].(map[string]any)
		for name, keyValue := range apiKeysMap {
			secret, ok := keyValue.(string)
			if !ok || secret == "" {
				continue
			}
			apiKeysMap[name] = config.NormalizeSecretStorage(secret)
		}
	}
}

func NormalizeProviderConfigJSON(cfgMap map[string]any) {
	providerMap, _ := cfgMap["provider"].(map[string]any)
	for _, v := range providerMap {
		pm, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if protocol, ok := pm["protocol"].(string); ok && strings.TrimSpace(protocol) == "" {
			delete(pm, "protocol")
		}
		if clearAPIKey, _ := pm[clearProviderAPIKeyMarker].(bool); clearAPIKey {
			delete(pm, "api_key")
		}
		delete(pm, clearProviderAPIKeyMarker)
	}
}

// NormalizePromptConfigJSON removes storage-only false prompt flags.
// Runtime behavior treats missing prompt_enabled + empty system_prompt as disabled,
// so writing explicit false only creates a noisier TOML representation.
func NormalizePromptConfigJSON(cfgMap map[string]any) {
	NormalizePromptConfigValue(cfgMap)
}

func NormalizePromptConfigValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			NormalizePromptConfigValue(child)
		}
		promptEnabledRaw, hasPromptEnabled := typed["prompt_enabled"]
		promptEnabled, ok := promptEnabledRaw.(bool)
		if !hasPromptEnabled || !ok || promptEnabled {
			return
		}
		systemPrompt, _ := typed["system_prompt"].(string)
		if strings.TrimSpace(systemPrompt) == "" {
			delete(typed, "prompt_enabled")
		}
	case []any:
		for _, child := range typed {
			NormalizePromptConfigValue(child)
		}
	}
}

var apiKeyRandomReader io.Reader = rand.Reader

// GenerateAPIKey creates a random API key with "wk_" prefix.
func GenerateAPIKey() (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const keyLen = 32

	b := make([]byte, keyLen)
	if _, err := io.ReadFull(apiKeyRandomReader, b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}

	return "wk_" + string(b), nil
}
