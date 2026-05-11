package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type cliproxyAuthValidation struct {
	Provider string
	Label    string
	Status   string
	Message  string
}

func validateAndNormalizeCLIProxyAuthContent(content, filename string) (string, string, cliproxyAuthValidation, error) {
	raw := strings.TrimSpace(content)
	if raw == "" {
		return "", "", cliproxyAuthValidation{}, errors.New("content is required")
	}

	payload, normalized, validation, err := validateCLIProxyAuthPayload([]byte(raw))
	if err != nil {
		return "", "", cliproxyAuthValidation{}, err
	}
	if validation.Status == cliproxyAuthValidationInvalid {
		return "", "", cliproxyAuthValidation{}, errors.New(validation.Message)
	}

	name := strings.TrimSpace(filename)
	if name == "" {
		name = buildCLIProxyAuthFilename(validation.Provider, payload, raw)
	}
	name = filepath.Base(name)
	if name != strings.TrimSpace(filename) && filename != "" {
		return "", "", cliproxyAuthValidation{}, errors.New("filename must be a plain basename")
	}
	if name == "." || name == string(filepath.Separator) || strings.Contains(name, "..") {
		return "", "", cliproxyAuthValidation{}, errors.New("filename must be a safe basename")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		name += ".json"
	}

	return name, normalized, validation, nil
}

func buildCLIProxyAuthFilename(provider string, payload map[string]any, content string) string {
	label := strings.TrimSpace(extractCLIProxyAuthLabel(payload))
	if label != "" {
		label = sanitizeFilenameToken(label)
	}
	provider = sanitizeFilenameToken(strings.TrimSpace(provider))
	if provider == "" {
		provider = "cliproxy-auth"
	}
	sum := sha256.Sum256([]byte(content))
	suffix := hex.EncodeToString(sum[:6])
	if label != "" {
		return provider + "-" + label + "-" + suffix + ".json"
	}
	return provider + "-" + suffix + ".json"
}

func extractCLIProxyAuthLabel(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload["label"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := payload["email"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if attrs, ok := payload["attributes"].(map[string]any); ok {
		if v, ok := attrs["label"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		if v, ok := attrs["email"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func sanitizeFilenameToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-_.")
	if out == "" {
		return ""
	}
	return out
}

func readCLIProxyAuthValidation(path string) cliproxyAuthValidation {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return cliproxyAuthValidation{
			Status:  cliproxyAuthValidationInvalid,
			Message: "auth file cannot be read or is empty",
		}
	}
	_, _, validation, err := validateCLIProxyAuthPayload(data)
	if err != nil {
		return cliproxyAuthValidation{
			Status:  cliproxyAuthValidationInvalid,
			Message: err.Error(),
		}
	}
	return validation
}

func validateCLIProxyAuthPayload(data []byte) (map[string]any, string, cliproxyAuthValidation, error) {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, "", cliproxyAuthValidation{}, fmt.Errorf("cliproxy auth content must be valid JSON: %w", err)
	}
	if payload == nil {
		return nil, "", cliproxyAuthValidation{}, errors.New("cliproxy auth JSON must be an object")
	}
	payload = normalizeCLIProxyAuthPayload(payload)
	provider := extractCLIProxyAuthProvider(payload)
	if provider == "" {
		return nil, "", cliproxyAuthValidation{}, errors.New("cliproxy auth JSON must include a non-empty type or provider field")
	}
	payload["type"] = provider

	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, "", cliproxyAuthValidation{}, fmt.Errorf("normalize cliproxy auth JSON: %w", err)
	}

	validation := validateCLIProxyAuthStructure(provider, payload)
	return payload, string(normalized), validation, nil
}

func normalizeCLIProxyAuthPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	if metadata, ok := payload["metadata"].(map[string]any); ok {
		provider := firstNonEmptyString(payload["type"], payload["provider"], metadata["type"], metadata["provider"])
		if provider != "" {
			normalized := make(map[string]any, len(metadata)+6)
			for key, value := range metadata {
				normalized[key] = value
			}
			normalized["type"] = provider
			for _, key := range []string{
				"label", "disabled", "prefix", "proxy_url", "priority", "note",
				"status", "status_message", "unavailable",
				"plan", "plan_type", "chatgpt_plan_type", "subscription_plan", "account_plan",
				"usage", "quota", "model_states", "limits", "remaining", "reset_at", "reset_after",
				"credits", "credit_balance", "credits_balance", "minimum_credit_amount_for_usage", "maximum_credits",
			} {
				if _, exists := normalized[key]; !exists {
					if value, ok := payload[key]; ok {
						normalized[key] = value
					}
				}
			}
			return normalized
		}
	}
	if _, ok := payload["type"].(string); !ok {
		if provider := firstNonEmptyString(payload["provider"], payload["auth_type"]); provider != "" {
			payload["type"] = provider
		} else if looksLikeCodexCLIAuth(payload) {
			payload["type"] = "codex"
		}
	}
	normalizeCodexCLIAuthPayload(payload)
	return payload
}

func looksLikeCodexCLIAuth(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if _, ok := payload["auth_mode"].(string); ok {
		if _, hasAPIKey := payload["OPENAI_API_KEY"]; hasAPIKey {
			return true
		}
		if _, hasTokens := payload["tokens"]; hasTokens {
			return true
		}
	}
	return false
}

func normalizeCodexCLIAuthPayload(payload map[string]any) {
	if payload == nil || extractCLIProxyAuthProvider(payload) != "codex" {
		return
	}
	if firstNonEmptyString(payload["access_token"]) == "" {
		if apiKey := firstNonEmptyString(payload["OPENAI_API_KEY"]); apiKey != "" {
			payload["access_token"] = apiKey
		}
	}
	if token, ok := payload["tokens"].(map[string]any); ok {
		for _, key := range []string{"access_token", "refresh_token", "id_token"} {
			if firstNonEmptyString(payload[key]) == "" {
				if value := firstNonEmptyString(token[key]); value != "" {
					payload[key] = value
				}
			}
		}
	}
}

func extractCLIProxyAuthProvider(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(firstNonEmptyString(payload["type"], payload["provider"], payload["auth_type"])))
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		if s, ok := value.(string); ok {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func mapStringAny(value any) (map[string]any, bool) {
	switch m := value.(type) {
	case map[string]any:
		return m, true
	case map[string]string:
		out := make(map[string]any, len(m))
		for key, value := range m {
			out[key] = value
		}
		return out, true
	default:
		return nil, false
	}
}

func boolFromAny(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "y":
			return true, true
		case "false", "0", "no", "n":
			return false, true
		}
	}
	return false, false
}

func validateCLIProxyAuthStructure(provider string, payload map[string]any) cliproxyAuthValidation {
	validation := cliproxyAuthValidation{
		Provider: provider,
		Label:    extractCLIProxyAuthLabel(payload),
		Status:   cliproxyAuthValidationValid,
		Message:  "offline structure check passed",
	}
	if disabled, _ := payload["disabled"].(bool); disabled {
		validation.Status = cliproxyAuthValidationWarning
		validation.Message = "auth file is disabled"
		return validation
	}

	auth := &cliproxyauth.Auth{
		Provider:   provider,
		Attributes: extractCLIProxyAuthAttributes(payload),
		Metadata:   payload,
	}
	if expiresAt, ok := auth.ExpirationTime(); ok && !expiresAt.IsZero() && time.Now().After(expiresAt) {
		validation.Status = cliproxyAuthValidationWarning
		validation.Message = "auth file appears expired from local metadata"
		return validation
	}
	if hasCLIProxyAuthCredentialSignal(auth, payload) {
		return validation
	}

	validation.Status = cliproxyAuthValidationWarning
	validation.Message = "no common credential field was found; runtime may reject this auth file"
	return validation
}

func extractCLIProxyAuthAttributes(payload map[string]any) map[string]string {
	attrs := make(map[string]string)
	if payload == nil {
		return attrs
	}
	if apiKey, ok := payload["api_key"].(string); ok && strings.TrimSpace(apiKey) != "" {
		attrs["api_key"] = strings.TrimSpace(apiKey)
	}
	if rawAttrs, ok := payload["attributes"].(map[string]any); ok {
		for key, rawValue := range rawAttrs {
			value, ok := rawValue.(string)
			if !ok {
				continue
			}
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				attrs[key] = trimmed
			}
		}
	}
	return attrs
}

func hasCLIProxyAuthCredentialSignal(auth *cliproxyauth.Auth, payload map[string]any) bool {
	if kind, info := auth.AccountInfo(); strings.TrimSpace(kind) != "" && strings.TrimSpace(info) != "" {
		return true
	}
	for _, key := range []string{"access_token", "refresh_token", "id_token", "api_key"} {
		if v, ok := payload[key].(string); ok && strings.TrimSpace(v) != "" {
			return true
		}
	}
	if token, ok := payload["token"].(map[string]any); ok {
		for _, key := range []string{"access_token", "refresh_token", "id_token"} {
			if v, ok := token[key].(string); ok && strings.TrimSpace(v) != "" {
				return true
			}
		}
	}
	return false
}
