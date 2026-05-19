package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/wweir/warden/config"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	sel "github.com/wweir/warden/internal/selector"
)

func (h *Handler) verifyCLIProxyAuthFileOnline(ctx context.Context, req cliproxyAuthFileVerifyRequest) (cliproxyAuthFileVerifyResponse, error) {
	if h == nil || h.cfg == nil {
		return cliproxyAuthFileVerifyResponse{}, errors.New("cliproxy is not configured")
	}
	providerName := strings.TrimSpace(req.Provider)
	if providerName == "" {
		return cliproxyAuthFileVerifyResponse{}, errors.New("provider is required")
	}
	fileName := strings.TrimSpace(req.Filename)
	if fileName == "" {
		return cliproxyAuthFileVerifyResponse{}, errors.New("filename is required")
	}
	if _, err := validateCLIProxyAuthFileBasename(fileName); err != nil {
		return cliproxyAuthFileVerifyResponse{}, err
	}

	provCfg := h.cfg.Provider[providerName]
	if provCfg == nil {
		return cliproxyAuthFileVerifyResponse{}, fmt.Errorf("unknown provider: %s", providerName)
	}
	if provCfg.Backend != config.ProviderBackendCLIProxy {
		return cliproxyAuthFileVerifyResponse{}, errors.New("online auth validation is only available for cliproxy providers")
	}
	modes := config.ProviderFormats(provCfg)
	hasOpenAI := false
	for _, mode := range modes {
		if mode == config.ProviderFormatOpenAI {
			hasOpenAI = true
			break
		}
	}
	if !hasOpenAI {
		return cliproxyAuthFileVerifyResponse{}, errors.New("cliproxy online auth validation requires an openai-compatible provider")
	}

	dir, err := h.cliproxyAuthDir()
	if err != nil {
		return cliproxyAuthFileVerifyResponse{}, err
	}
	validation := readCLIProxyAuthValidation(filepath.Join(dir, fileName))
	if validation.Status == cliproxyAuthValidationInvalid {
		return cliproxyAuthFileVerifyResponse{}, fmt.Errorf("auth file is invalid: %s", validation.Message)
	}
	if !cliproxyAuthProviderMatchesBackend(validation.Provider, provCfg.BackendProvider) {
		return cliproxyAuthFileVerifyResponse{}, fmt.Errorf("auth file type %q does not match provider backend_provider %q", validation.Provider, provCfg.BackendProvider)
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = h.defaultCLIProxyAuthVerifyModel(ctx, providerName, provCfg)
	}
	if model == "" {
		return cliproxyAuthFileVerifyResponse{}, errors.New("online validation needs at least one discovered or configured model")
	}

	start := time.Now()
	err = sendCLIProxyAuthVerificationProbe(ctx, provCfg, model)
	latency := time.Since(start)
	resp := cliproxyAuthFileVerifyResponse{
		Filename:  fileName,
		Provider:  providerName,
		Model:     model,
		Protocol:  config.RouteProtocolResponses,
		Status:    "ok",
		LatencyMS: latency.Milliseconds(),
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Note:      "online request validates the current cliproxy provider credential pool, not a pinned auth file",
	}
	if err != nil {
		resp.Status = "error"
		resp.Error = err.Error()
	}
	return resp, nil
}

func (h *Handler) defaultCLIProxyAuthVerifyModel(ctx context.Context, providerName string, provCfg *config.ProviderConfig) string {
	if h != nil && h.selector != nil {
		for _, raw := range h.selector.ProviderModels(providerName) {
			if model := modelIDFromRawJSON(raw); model != "" {
				return model
			}
		}
	}
	for _, model := range provCfg.Models {
		if trimmed := strings.TrimSpace(model); trimmed != "" {
			return trimmed
		}
	}
	_, rawModels, err := sel.FetchModels(ctx, provCfg)
	if err == nil {
		for _, raw := range rawModels {
			if model := modelIDFromRawJSON(raw); model != "" {
				return model
			}
		}
	}
	return ""
}

func sendCLIProxyAuthVerificationProbe(ctx context.Context, provCfg *config.ProviderConfig, model string) error {
	modes := config.ProviderFormats(provCfg)
	hasOpenAI := false
	for _, mode := range modes {
		if mode == config.ProviderFormatOpenAI {
			hasOpenAI = true
			break
		}
	}
	if !hasOpenAI {
		return fmt.Errorf("provider family does not support cliproxy auth validation probe")
	}
	payload := map[string]any{
		"model": model,
		"input": "ping",
		"store": false,
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := probeContext(ctx)
	defer cancel()
	targetURL := upstreampkg.JoinBaseURLPath(provCfg.URL, upstreampkg.ProtocolEndpoint(string(config.ProviderFormatOpenAI), true))
	_, _, err := upstreampkg.SendRequest(ctx, nil, provCfg, targetURL, body, false)
	return err
}

func modelIDFromRawJSON(raw json.RawMessage) string {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err == nil {
		if id, _ := payload["id"].(string); strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id)
		}
	}
	var id string
	if err := json.Unmarshal(raw, &id); err == nil {
		return strings.TrimSpace(id)
	}
	return ""
}

func cliproxyAuthProviderMatchesBackend(authProvider, backendProvider string) bool {
	authProvider = strings.ToLower(strings.TrimSpace(authProvider))
	backendProvider = strings.ToLower(strings.TrimSpace(backendProvider))
	if authProvider == "" || backendProvider == "" {
		return false
	}
	if authProvider == backendProvider {
		return true
	}
	return authProvider == "gemini" && backendProvider == "gemini-cli" || authProvider == "gemini-cli" && backendProvider == "gemini"
}
