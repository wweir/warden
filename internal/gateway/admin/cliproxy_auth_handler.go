package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/wweir/warden/config"
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
