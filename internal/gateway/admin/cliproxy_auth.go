package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	sel "github.com/wweir/warden/internal/selector"
)

const cliproxyAuthMaxContentSize = 1 << 20
const cliproxyAuthUsageCacheTTL = 30 * time.Second

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

type cliproxyAuthFileUsageResponse struct {
	Filename        string                     `json:"filename"`
	Provider        string                     `json:"provider"`
	Label           string                     `json:"label,omitempty"`
	AccountKind     string                     `json:"account_kind,omitempty"`
	AccountInfo     string                     `json:"account_info,omitempty"`
	Status          string                     `json:"status"`
	Summary         []cliproxyAuthUsageMetric  `json:"summary,omitempty"`
	Data            map[string]json.RawMessage `json:"data,omitempty"`
	CheckedAt       string                     `json:"checked_at"`
	Cached          bool                       `json:"cached"`
	CacheTTLSeconds int                        `json:"cache_ttl_seconds"`
	Note            string                     `json:"note,omitempty"`
}

type cliproxyAuthUsageMetric struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type cliproxyAuthUsageCacheEntry struct {
	response    cliproxyAuthFileUsageResponse
	expiresAt   time.Time
	fileModTime time.Time
	fileSize    int64
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
	h.clearCLIProxyAuthUsageCache(filepath.Join(dir, fileName))

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
	h.clearCLIProxyAuthUsageCache(targetPath)

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

func (h *Handler) HandleCLIProxyAuthFileUsage(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
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

	result, err := h.readCLIProxyAuthFileUsage(filepath.Join(dir, fileName))
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

func (h *Handler) readCLIProxyAuthFileUsage(path string) (cliproxyAuthFileUsageResponse, error) {
	now := time.Now()
	fileInfo, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return cliproxyAuthFileUsageResponse{}, fmt.Errorf("auth file not found: %s", filepath.Base(path))
		}
		return cliproxyAuthFileUsageResponse{}, fmt.Errorf("stat cliproxy auth file %s: %w", filepath.Base(path), statErr)
	}
	if h != nil {
		h.cliproxyUsageMu.Lock()
		if entry, ok := h.cliproxyUsageCache[path]; ok && now.Before(entry.expiresAt) && entry.fileSize == fileInfo.Size() && entry.fileModTime.Equal(fileInfo.ModTime()) {
			resp := cloneCLIProxyAuthFileUsageResponse(entry.response)
			resp.Cached = true
			h.cliproxyUsageMu.Unlock()
			h.mergeCLIProxyRuntimeUsage(&resp)
			return resp, nil
		}
		h.cliproxyUsageMu.Unlock()
	}

	resp, err := readCLIProxyAuthFileUsage(path, now)
	if err != nil {
		return cliproxyAuthFileUsageResponse{}, err
	}

	if h != nil {
		h.cliproxyUsageMu.Lock()
		if h.cliproxyUsageCache == nil {
			h.cliproxyUsageCache = map[string]cliproxyAuthUsageCacheEntry{}
		}
		h.cliproxyUsageCache[path] = cliproxyAuthUsageCacheEntry{
			response:    cloneCLIProxyAuthFileUsageResponse(resp),
			expiresAt:   now.Add(cliproxyAuthUsageCacheTTL),
			fileModTime: fileInfo.ModTime(),
			fileSize:    fileInfo.Size(),
		}
		h.cliproxyUsageMu.Unlock()
	}
	h.mergeCLIProxyRuntimeUsage(&resp)
	return resp, nil
}

func (h *Handler) clearCLIProxyAuthUsageCache(path string) {
	if h == nil {
		return
	}
	h.cliproxyUsageMu.Lock()
	defer h.cliproxyUsageMu.Unlock()
	if path == "" {
		h.cliproxyUsageCache = map[string]cliproxyAuthUsageCacheEntry{}
		return
	}
	delete(h.cliproxyUsageCache, path)
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

func readCLIProxyAuthFileUsage(path string, now time.Time) (cliproxyAuthFileUsageResponse, error) {
	fileName := filepath.Base(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cliproxyAuthFileUsageResponse{}, fmt.Errorf("auth file not found: %s", fileName)
		}
		return cliproxyAuthFileUsageResponse{}, fmt.Errorf("read cliproxy auth file %s: %w", fileName, err)
	}
	payload, normalized, validation, err := validateCLIProxyAuthPayload(data)
	if err != nil {
		return cliproxyAuthFileUsageResponse{}, err
	}

	accountAuth := &cliproxyauth.Auth{
		Provider:   validation.Provider,
		Attributes: extractCLIProxyAuthAttributes(payload),
		Metadata:   payload,
	}
	accountKind, accountInfo := accountAuth.AccountInfo()
	if accountKind == "api_key" {
		accountInfo = "configured"
	}
	summary, usageData, status, note := summarizeCLIProxyAuthUsage([]byte(normalized), payload, accountAuth)
	resp := cliproxyAuthFileUsageResponse{
		Filename:        fileName,
		Provider:        validation.Provider,
		Label:           validation.Label,
		AccountKind:     accountKind,
		AccountInfo:     accountInfo,
		Status:          status,
		Summary:         summary,
		Data:            usageData,
		CheckedAt:       now.UTC().Format(time.RFC3339),
		CacheTTLSeconds: int(cliproxyAuthUsageCacheTTL.Seconds()),
		Note:            note,
	}
	return resp, nil
}

func summarizeCLIProxyAuthUsage(raw []byte, payload map[string]any, auth *cliproxyauth.Auth) ([]cliproxyAuthUsageMetric, map[string]json.RawMessage, string, string) {
	usageData := cliproxyAuthUsageData(raw)
	summary := make([]cliproxyAuthUsageMetric, 0, 8)
	root := gjson.ParseBytes(raw)
	if plan := firstGJSONString(root, "plan", "plan_type", "chatgpt_plan_type", "subscription_plan", "account_plan"); plan != "" {
		summary = append(summary, cliproxyAuthUsageMetric{Name: "plan", Value: plan})
	} else if plan := codexPlanTypeFromIDToken(firstGJSONString(root, "id_token", "token.id_token", "tokens.id_token")); plan != "" {
		summary = append(summary, cliproxyAuthUsageMetric{Name: "plan", Value: plan})
	} else if auth != nil && auth.Attributes != nil {
		if plan = firstNonEmptyString(auth.Attributes["plan"], auth.Attributes["plan_type"], auth.Attributes["chatgpt_plan_type"]); plan != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "plan", Value: plan})
		}
	}
	summary = appendFirstUsageMetric(summary, "5h", root,
		"usage.5h", "usage.5_hour", "usage.five_hour", "usage.fiveHour", "usage.five_hour_limit", "usage.five_hour_quota",
		"quota.5h", "quota.5_hour", "quota.five_hour", "quota.fiveHour", "quota.five_hour_limit", "quota.five_hour_quota",
		"limits.5h", "limits.5_hour", "limits.five_hour", "limits.fiveHour", "limits.five_hour_limit", "limits.five_hour_quota",
	)
	summary = appendFirstUsageMetric(summary, "5h_reset", root,
		"usage.5h.reset_at", "usage.5_hour.reset_at", "usage.five_hour.reset_at", "usage.fiveHour.reset_at",
		"usage.5h.reset_after", "usage.5_hour.reset_after", "usage.five_hour.reset_after", "usage.fiveHour.reset_after",
		"quota.5h.reset_at", "quota.5_hour.reset_at", "quota.five_hour.reset_at", "quota.fiveHour.reset_at",
		"limits.5h.reset_at", "limits.5_hour.reset_at", "limits.five_hour.reset_at", "limits.fiveHour.reset_at",
	)
	summary = appendFirstUsageMetric(summary, "weekly", root,
		"usage.weekly", "usage.week", "usage.7d", "usage.seven_day", "usage.sevenDay", "usage.weekly_limit", "usage.weekly_quota",
		"quota.weekly", "quota.week", "quota.7d", "quota.seven_day", "quota.sevenDay", "quota.weekly_limit", "quota.weekly_quota",
		"limits.weekly", "limits.week", "limits.7d", "limits.seven_day", "limits.sevenDay", "limits.weekly_limit", "limits.weekly_quota",
	)
	summary = appendFirstUsageMetric(summary, "weekly_reset", root,
		"usage.weekly.reset_at", "usage.week.reset_at", "usage.7d.reset_at", "usage.seven_day.reset_at", "usage.sevenDay.reset_at",
		"usage.weekly.reset_after", "usage.week.reset_after", "usage.7d.reset_after", "usage.seven_day.reset_after", "usage.sevenDay.reset_after",
		"quota.weekly.reset_at", "quota.week.reset_at", "quota.7d.reset_at", "quota.seven_day.reset_at", "quota.sevenDay.reset_at",
		"limits.weekly.reset_at", "limits.week.reset_at", "limits.7d.reset_at", "limits.seven_day.reset_at", "limits.sevenDay.reset_at",
	)
	if quota := root.Get("quota"); quota.Exists() {
		if quota.Get("exceeded").Bool() {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "quota", Value: "exceeded"})
		} else if reason := strings.TrimSpace(quota.Get("reason").String()); reason != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "quota", Value: reason})
		}
		if recover := firstGJSONString(quota, "next_recover_at", "nextRecoverAt"); recover != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "recover_at", Value: recover})
		}
	}
	for _, key := range []string{"reset_at", "reset_after", "remaining", "credits", "credit_balance", "credits_balance"} {
		if len(summary) >= 8 {
			break
		}
		if result := root.Get(key); result.Exists() && isGJSONScalar(result) {
			summary = append(summary, cliproxyAuthUsageMetric{Name: key, Value: gjsonScalarString(result)})
		}
	}
	if len(summary) > 8 {
		summary = summary[:8]
	}
	disabled, _ := boolFromAny(payload["disabled"])
	unavailable, _ := boolFromAny(payload["unavailable"])
	quotaExceeded := root.Get("quota.exceeded").Bool()
	switch {
	case disabled:
		return summary, usageData, "disabled", "auth file is disabled"
	case unavailable:
		return summary, usageData, "warning", "auth file is marked unavailable"
	case quotaExceeded:
		return summary, usageData, "warning", "quota is marked exceeded"
	case len(summary) > 0 || len(usageData) > 0:
		return summary, usageData, "ok", ""
	default:
		return summary, usageData, "unknown", ""
	}
}

func cloneCLIProxyAuthFileUsageResponse(resp cliproxyAuthFileUsageResponse) cliproxyAuthFileUsageResponse {
	if len(resp.Summary) > 0 {
		resp.Summary = append([]cliproxyAuthUsageMetric(nil), resp.Summary...)
	}
	if len(resp.Data) > 0 {
		data := make(map[string]json.RawMessage, len(resp.Data))
		for key, value := range resp.Data {
			data[key] = append(json.RawMessage(nil), value...)
		}
		resp.Data = data
	}
	return resp
}

func (h *Handler) mergeCLIProxyRuntimeUsage(resp *cliproxyAuthFileUsageResponse) {
	if h == nil || h.selector == nil || h.cfg == nil || resp == nil {
		return
	}
	runtime, ok := h.latestCLIProxyRuntimeUsage(resp.Provider)
	if !ok {
		return
	}
	resp.Summary = mergeUsageSummary(resp.Summary, runtime.summary)
	if len(runtime.data) > 0 {
		if resp.Data == nil {
			resp.Data = map[string]json.RawMessage{}
		}
		for key, value := range runtime.data {
			resp.Data[key] = value
		}
	}
	if resp.Status == "unknown" || resp.Status == "ok" {
		resp.Status = runtime.status
	}
	if resp.Note == "" {
		resp.Note = runtime.note
	}
}

type cliproxyRuntimeUsage struct {
	summary []cliproxyAuthUsageMetric
	data    map[string]json.RawMessage
	status  string
	note    string
	kind    string
}

func (h *Handler) latestCLIProxyRuntimeUsage(authProvider string) (cliproxyRuntimeUsage, bool) {
	authProvider = strings.ToLower(strings.TrimSpace(authProvider))
	if authProvider == "" {
		return cliproxyRuntimeUsage{}, false
	}
	var bestAuth *cliproxyRuntimeUsage
	var bestAuthAt time.Time
	var bestLimit *cliproxyRuntimeUsage
	var bestLimitAt time.Time
	var bestAny *cliproxyRuntimeUsage
	var bestAnyAt time.Time
	for name, prov := range h.cfg.Provider {
		if prov == nil || prov.Backend != config.ProviderBackendCLIProxy {
			continue
		}
		if !cliproxyAuthProviderMatchesBackend(authProvider, prov.BackendProvider) {
			continue
		}
		status := h.selector.ProviderDetail(name)
		if status == nil {
			continue
		}
		for i := range status.SuppressReasons {
			reason := status.SuppressReasons[i]
			runtime, ok := runtimeUsageFromSuppressReason(name, reason)
			if !ok {
				continue
			}
			switch runtime.kind {
			case "auth_error":
				if bestAuth == nil || reason.Time.After(bestAuthAt) {
					bestAuth = &runtime
					bestAuthAt = reason.Time
				}
				continue
			case "usage_limit":
				if bestLimit == nil || reason.Time.After(bestLimitAt) {
					bestLimit = &runtime
					bestLimitAt = reason.Time
				}
				continue
			}
			if bestAny == nil || reason.Time.After(bestAnyAt) {
				bestAny = &runtime
				bestAnyAt = reason.Time
			}
		}
	}
	if bestAuth != nil && (bestLimit == nil || bestAuthAt.After(bestLimitAt)) {
		return *bestAuth, true
	}
	if bestLimit != nil {
		return *bestLimit, true
	}
	if bestAny != nil {
		return *bestAny, true
	}
	return cliproxyRuntimeUsage{}, false
}

func runtimeUsageFromSuppressReason(providerName string, reason sel.SuppressReason) (cliproxyRuntimeUsage, bool) {
	body := extractJSONFromText(reason.Reason)
	if body == "" || !gjson.Valid(body) {
		return cliproxyRuntimeUsage{}, false
	}
	root := gjson.Parse(body)
	errObj := root.Get("error")
	if errObj.Exists() {
		root = errObj
	}
	summary := make([]cliproxyAuthUsageMetric, 0, 6)
	if plan := firstGJSONString(root, "plan_type", "plan", "chatgpt_plan_type"); plan != "" {
		summary = append(summary, cliproxyAuthUsageMetric{Name: "plan", Value: plan})
	}
	errType := firstGJSONString(root, "type", "code")
	switch errType {
	case "usage_limit_reached":
		summary = append(summary, cliproxyAuthUsageMetric{Name: "5h", Value: "limited"})
		if resetAt := runtimeResetAt(root, reason.Time, "resets_at", "resets_in_seconds"); resetAt != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "5h_reset", Value: resetAt})
		}
	case "authentication_error", "auth_unavailable":
		summary = append(summary, cliproxyAuthUsageMetric{Name: "auth", Value: runtimeAuthErrorValue(root)})
	case "model_cooldown":
		summary = append(summary, cliproxyAuthUsageMetric{Name: "quota", Value: "model_cooldown"})
		if model := firstGJSONString(root, "model"); model != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "model", Value: model})
		}
		if cooldown := firstGJSONString(root, "reset_time"); cooldown != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "cooldown", Value: cooldown})
		}
		if resetAt := runtimeResetAt(root, reason.Time, "", "reset_seconds"); resetAt != "" {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "recover_at", Value: resetAt})
		}
	case "server_error", "internal_server_error":
		if strings.Contains(strings.ToLower(firstGJSONString(root, "message")), "auth_unavailable") || strings.Contains(strings.ToLower(firstGJSONString(root, "code")), "auth_unavailable") {
			summary = append(summary, cliproxyAuthUsageMetric{Name: "auth", Value: "unavailable"})
		}
	}
	summary = appendFirstUsageMetric(summary, "weekly", root, "weekly", "week", "7d", "weekly_limit", "weekly_quota")
	summary = appendFirstUsageMetric(summary, "weekly_reset", root, "weekly.reset_at", "week.reset_at", "7d.reset_at", "weekly_reset_at")
	if len(summary) == 0 {
		return cliproxyRuntimeUsage{}, false
	}
	data := map[string]json.RawMessage{}
	runtimePayload := map[string]any{
		"provider":    providerName,
		"observed_at": reason.Time.UTC().Format(time.RFC3339),
		"body":        json.RawMessage(body),
	}
	if encoded, err := json.Marshal(runtimePayload); err == nil {
		data[runtimeUsageDataKey(summary)] = encoded
	}
	status := "warning"
	note := "runtime quota data is from the latest sanitized upstream error body"
	kind := "runtime"
	switch {
	case hasUsageMetric(summary, "auth"):
		status = "error"
		note = "runtime auth status is from the latest sanitized upstream error body"
		kind = "auth_error"
	case hasUsageMetric(summary, "5h"):
		kind = "usage_limit"
	case hasUsageMetric(summary, "quota"):
		kind = "quota"
	}
	return cliproxyRuntimeUsage{
		summary: summary,
		data:    data,
		status:  status,
		note:    note,
		kind:    kind,
	}, true
}

func runtimeAuthErrorValue(root gjson.Result) string {
	msg := strings.ToLower(firstGJSONString(root, "message"))
	if strings.Contains(msg, "invalidated") {
		return "invalidated"
	}
	if strings.Contains(msg, "no auth available") {
		return "unavailable"
	}
	code := firstGJSONString(root, "code")
	if strings.EqualFold(code, "auth_unavailable") {
		return "unavailable"
	}
	return firstNonEmptyString(code, "error")
}

func runtimeUsageDataKey(summary []cliproxyAuthUsageMetric) string {
	if hasUsageMetric(summary, "auth") {
		return "runtime_auth"
	}
	return "runtime_quota"
}

func mergeUsageSummary(base, extra []cliproxyAuthUsageMetric) []cliproxyAuthUsageMetric {
	if len(extra) == 0 {
		return base
	}
	out := append([]cliproxyAuthUsageMetric(nil), base...)
	seen := make(map[string]int, len(out))
	for i, item := range out {
		seen[item.Name] = i
	}
	for _, item := range extra {
		if item.Name == "" || item.Value == "" {
			continue
		}
		if idx, ok := seen[item.Name]; ok {
			if out[idx].Value == "" || item.Name == "plan" {
				out[idx].Value = item.Value
			}
			continue
		}
		out = append(out, item)
		seen[item.Name] = len(out) - 1
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func extractJSONFromText(text string) string {
	text = strings.TrimSpace(text)
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return ""
	}
	candidate := strings.TrimSpace(text[start:])
	for len(candidate) > 0 {
		if gjson.Valid(candidate) {
			return candidate
		}
		candidate = strings.TrimSpace(candidate[:len(candidate)-1])
	}
	return ""
}

func runtimeResetAt(root gjson.Result, observedAt time.Time, unixPath, secondsPath string) string {
	if unixPath != "" {
		if unix := root.Get(unixPath).Int(); unix > 0 {
			return time.Unix(unix, 0).UTC().Format(time.RFC3339)
		}
	}
	if secondsPath != "" {
		if seconds := root.Get(secondsPath).Int(); seconds > 0 {
			return observedAt.Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339)
		}
	}
	return ""
}

func appendFirstUsageMetric(summary []cliproxyAuthUsageMetric, name string, root gjson.Result, paths ...string) []cliproxyAuthUsageMetric {
	if len(summary) >= 8 || hasUsageMetric(summary, name) {
		return summary
	}
	for _, path := range paths {
		result := root.Get(path)
		if !result.Exists() {
			continue
		}
		value := usageMetricValue(result)
		if value == "" {
			continue
		}
		return append(summary, cliproxyAuthUsageMetric{Name: name, Value: value})
	}
	return summary
}

func hasUsageMetric(summary []cliproxyAuthUsageMetric, name string) bool {
	for _, item := range summary {
		if item.Name == name {
			return true
		}
	}
	return false
}

func usageMetricValue(result gjson.Result) string {
	if isGJSONScalar(result) {
		return gjsonScalarString(result)
	}
	used := firstExistingGJSON(result, "used", "current", "consumed")
	limit := firstExistingGJSON(result, "limit", "total", "quota", "maximum")
	if used.Exists() && limit.Exists() && isGJSONScalar(used) && isGJSONScalar(limit) {
		return gjsonScalarString(used) + "/" + gjsonScalarString(limit)
	}
	remaining := firstExistingGJSON(result, "remaining", "available", "left")
	if remaining.Exists() && limit.Exists() && isGJSONScalar(remaining) && isGJSONScalar(limit) {
		return gjsonScalarString(remaining) + "/" + gjsonScalarString(limit) + " remaining"
	}
	if reset := firstExistingGJSON(result, "reset_at", "reset_after", "resets_at", "next_reset_at", "nextResetAt", "next_recover_at", "nextRecoverAt"); reset.Exists() && isGJSONScalar(reset) {
		return gjsonScalarString(reset)
	}
	raw := result.Raw
	if raw == "" {
		marshaled, err := json.Marshal(result.Value())
		if err != nil {
			return ""
		}
		raw = string(marshaled)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(raw)); err == nil {
		raw = compact.String()
	}
	if len(raw) > 96 {
		raw = raw[:93] + "..."
	}
	return raw
}

func firstExistingGJSON(root gjson.Result, paths ...string) gjson.Result {
	for _, path := range paths {
		result := root.Get(path)
		if result.Exists() {
			return result
		}
	}
	return gjson.Result{}
}

func cliproxyAuthUsageData(raw []byte) map[string]json.RawMessage {
	root := gjson.ParseBytes(raw)
	allowed := []string{
		"usage",
		"quota",
		"model_states",
		"limits",
		"remaining",
		"reset_at",
		"reset_after",
		"credits",
		"credit_balance",
		"credits_balance",
		"minimum_credit_amount_for_usage",
		"maximum_credits",
	}
	data := make(map[string]json.RawMessage)
	for _, path := range allowed {
		result := root.Get(path)
		if !result.Exists() {
			continue
		}
		rawValue := result.Raw
		if rawValue == "" {
			marshaled, err := json.Marshal(result.Value())
			if err != nil {
				continue
			}
			rawValue = string(marshaled)
		}
		if rawValue == "" || rawValue == "null" {
			continue
		}
		data[path] = json.RawMessage(rawValue)
	}
	if len(data) == 0 {
		return nil
	}
	return data
}

func firstGJSONString(root gjson.Result, paths ...string) string {
	for _, path := range paths {
		result := root.Get(path)
		if result.Exists() {
			if value := strings.TrimSpace(result.String()); value != "" {
				return value
			}
		}
	}
	return ""
}

func isGJSONScalar(result gjson.Result) bool {
	return result.Type == gjson.String || result.Type == gjson.Number || result.Type == gjson.True || result.Type == gjson.False
}

func gjsonScalarString(result gjson.Result) string {
	if result.Type == gjson.String {
		return strings.TrimSpace(result.String())
	}
	return result.Raw
}

func codexPlanTypeFromIDToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	if plan := strings.TrimSpace(gjson.GetBytes(claims, `https://api\.openai\.com/auth.chatgpt_plan_type`).String()); plan != "" {
		return plan
	}
	var payload map[string]any
	if err := json.Unmarshal(claims, &payload); err != nil {
		return ""
	}
	authInfo, _ := payload["https://api.openai.com/auth"].(map[string]any)
	return firstNonEmptyString(authInfo["chatgpt_plan_type"])
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
