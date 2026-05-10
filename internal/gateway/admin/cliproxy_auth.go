package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/wweir/warden/config"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	sel "github.com/wweir/warden/internal/selector"
)

const cliproxyAuthMaxContentSize = 1 << 20

const (
	cliproxyAuthValidationValid   = "valid"
	cliproxyAuthValidationWarning = "warning"
	cliproxyAuthValidationInvalid = "invalid"
)

type cliproxyAuthFileMeta struct {
	Filename          string `json:"filename"`
	Provider          string `json:"provider"`
	Label             string `json:"label,omitempty"`
	Size              int64  `json:"size"`
	Modified          string `json:"modified"`
	ValidationStatus  string `json:"validation_status"`
	ValidationMessage string `json:"validation_message,omitempty"`
}

type cliproxyAuthFileListResponse struct {
	AuthDir string                 `json:"auth_dir"`
	Files   []cliproxyAuthFileMeta `json:"files"`
}

type cliproxyAuthFileCreateRequest struct {
	Content  string `json:"content"`
	Filename string `json:"filename,omitempty"`
}

type cliproxyAuthFileCreateResponse struct {
	File cliproxyAuthFileMeta `json:"file"`
}

type cliproxyAuthFileVerifyRequest struct {
	Provider string `json:"provider"`
	Filename string `json:"filename"`
	Model    string `json:"model,omitempty"`
}

type cliproxyAuthFileVerifyResponse struct {
	Filename  string `json:"filename"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Protocol  string `json:"protocol"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
	CheckedAt string `json:"checked_at"`
	Note      string `json:"note,omitempty"`
}

func (h *Handler) HandleCLIProxyAuthFilesList(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	dir, err := h.cliproxyAuthDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	files, err := listCLIProxyAuthFiles(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cliproxyAuthFileListResponse{
		AuthDir: dir,
		Files:   files,
	})
}

func (h *Handler) HandleCLIProxyAuthFileCreate(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	dir, err := h.cliproxyAuthDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req cliproxyAuthFileCreateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, cliproxyAuthMaxContentSize)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	fileName, content, validation, err := validateAndNormalizeCLIProxyAuthContent(req.Content, req.Filename)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.cliproxyAuthMu.Lock()
	defer h.cliproxyAuthMu.Unlock()

	if _, err := writeCLIProxyAuthFile(dir, fileName, content); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cliproxyAuthFileCreateResponse{
		File: cliproxyAuthFileMeta{
			Filename:          fileName,
			Provider:          validation.Provider,
			Label:             validation.Label,
			ValidationStatus:  validation.Status,
			ValidationMessage: validation.Message,
		},
	})
}

func (h *Handler) HandleCLIProxyAuthFileDelete(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	dir, err := h.cliproxyAuthDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	fileName, err := validateCLIProxyAuthFileBasename(r.URL.Query().Get("filename"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.cliproxyAuthMu.Lock()
	defer h.cliproxyAuthMu.Unlock()

	targetPath := filepath.Join(dir, fileName)
	if err := os.Remove(targetPath); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "auth file not found: "+fileName, http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Errorf("delete cliproxy auth file: %w", err).Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"deleted":  true,
		"filename": fileName,
	})
}

func (h *Handler) HandleCLIProxyAuthFileVerify(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req cliproxyAuthFileVerifyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, cliproxyAuthMaxContentSize)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	result, err := h.verifyCLIProxyAuthFileOnline(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (h *Handler) cliproxyAuthDir() (string, error) {
	if h == nil || h.cfg == nil {
		return "", errors.New("cliproxy is not configured")
	}
	dir := config.DefaultCLIProxyAuthDir
	if h.cfg.CLIProxy != nil {
		dir = strings.TrimSpace(h.cfg.CLIProxy.AuthDir)
	}
	if dir == "" {
		dir = config.DefaultCLIProxyAuthDir
	}
	if strings.Contains(dir, "\x00") {
		return "", errors.New("cliproxy.auth_dir contains invalid characters")
	}
	return dir, nil
}

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
	if provCfg.Protocol != config.ProviderProtocolOpenAI {
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
		Protocol:  config.RouteProtocolResponsesStateless,
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
	if provCfg.Protocol != config.ProviderProtocolOpenAI {
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
	_, _, err := upstreampkg.SendRequest(ctx, nil, provCfg, upstreampkg.ProtocolEndpoint(provCfg.Protocol, true), body, false)
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

func validateCLIProxyAuthFileBasename(filename string) (string, error) {
	name := strings.TrimSpace(filename)
	if name == "" {
		return "", errors.New("filename is required")
	}
	if filepath.Base(name) != name || strings.Contains(name, "..") {
		return "", errors.New("filename must be a safe JSON basename")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		return "", errors.New("filename must end with .json")
	}
	return name, nil
}

func listCLIProxyAuthFiles(dir string) ([]cliproxyAuthFileMeta, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []cliproxyAuthFileMeta{}, nil
		}
		return nil, fmt.Errorf("read cliproxy auth_dir: %w", err)
	}

	files := make([]cliproxyAuthFileMeta, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		info, errInfo := entry.Info()
		if errInfo != nil {
			return nil, fmt.Errorf("stat cliproxy auth file %s: %w", entry.Name(), errInfo)
		}
		validation := readCLIProxyAuthValidation(filepath.Join(dir, entry.Name()))
		files = append(files, cliproxyAuthFileMeta{
			Filename:          entry.Name(),
			Provider:          validation.Provider,
			Label:             validation.Label,
			Size:              info.Size(),
			Modified:          info.ModTime().UTC().Format(time.RFC3339),
			ValidationStatus:  validation.Status,
			ValidationMessage: validation.Message,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Filename < files[j].Filename
	})
	return files, nil
}

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
			for _, key := range []string{"label", "disabled", "prefix", "proxy_url", "priority", "note"} {
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

func writeCLIProxyAuthFile(dir, filename, content string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create cliproxy auth_dir: %w", err)
	}
	targetPath := filepath.Join(dir, filename)
	tmpPath := targetPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(strings.TrimSpace(content)), 0o600); err != nil {
		return "", fmt.Errorf("write cliproxy auth file: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("rename cliproxy auth file: %w", err)
	}
	if err := os.Chmod(targetPath, 0o600); err != nil {
		return "", fmt.Errorf("chmod cliproxy auth file: %w", err)
	}
	return targetPath, nil
}
