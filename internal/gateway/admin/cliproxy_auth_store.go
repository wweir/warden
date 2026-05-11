package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

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
	slices.SortFunc(files, func(a, b cliproxyAuthFileMeta) int {
		return strings.Compare(a.Filename, b.Filename)
	})
	return files, nil
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
	return cliproxyAuthFileUsageResponse{
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
	}, nil
}

func cloneCLIProxyAuthFileUsageResponse(resp cliproxyAuthFileUsageResponse) cliproxyAuthFileUsageResponse {
	resp.Summary = cloneSlice(resp.Summary)
	if len(resp.Data) > 0 {
		data := make(map[string]json.RawMessage, len(resp.Data))
		for key, value := range resp.Data {
			data[key] = cloneSlice(value)
		}
		resp.Data = data
	}
	return resp
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

func cloneSlice[T ~[]E, E any](items T) T {
	if len(items) == 0 {
		return nil
	}
	return append(T(nil), items...)
}
