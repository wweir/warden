package admin

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wweir/warden/config"
	sel "github.com/wweir/warden/internal/selector"
)

func TestHandleCLIProxyAuthFileCreateWritesAuthJSON(t *testing.T) {
	authDir := t.TempDir()
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})

	body := `{"type":"codex","label":"team-a","email":"user@example.com"}`
	payload, _ := json.Marshal(map[string]string{"content": body})
	req := httptest.NewRequest(http.MethodPost, "/_admin/api/cliproxy/auth-files", strings.NewReader(string(payload)))
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileCreate(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp cliproxyAuthFileCreateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.File.Provider != "codex" {
		t.Fatalf("provider = %q, want codex", resp.File.Provider)
	}
	if resp.File.ValidationStatus != cliproxyAuthValidationValid {
		t.Fatalf("validation_status = %q, want valid; message=%q", resp.File.ValidationStatus, resp.File.ValidationMessage)
	}
	if !strings.HasPrefix(resp.File.Filename, "codex-team-a-") || !strings.HasSuffix(resp.File.Filename, ".json") {
		t.Fatalf("filename = %q, want codex-team-a-*.json", resp.File.Filename)
	}
	data, err := os.ReadFile(filepath.Join(authDir, resp.File.Filename))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("decode written file: %v", err)
	}
	if written["type"] != "codex" || written["label"] != "team-a" || written["email"] != "user@example.com" {
		t.Fatalf("written auth = %#v, want codex team-a user@example.com", written)
	}
	info, err := os.Stat(filepath.Join(authDir, resp.File.Filename))
	if err != nil {
		t.Fatalf("stat written file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}

func TestHandleCLIProxyAuthFileCreateAcceptsProviderField(t *testing.T) {
	authDir := t.TempDir()
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})

	body := `{"provider":"codex","label":"team-a","email":"user@example.com"}`
	payload, _ := json.Marshal(map[string]string{"content": body, "filename": "codex-provider.json"})
	req := httptest.NewRequest(http.MethodPost, "/_admin/api/cliproxy/auth-files", strings.NewReader(string(payload)))
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileCreate(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(authDir, "codex-provider.json"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("decode written file: %v", err)
	}
	if written["type"] != "codex" {
		t.Fatalf("type = %q, want codex", written["type"])
	}
}

func TestHandleCLIProxyAuthFileCreateFlattensAuthWrapperMetadata(t *testing.T) {
	authDir := t.TempDir()
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})

	body := `{"provider":"codex","label":"outer","metadata":{"email":"user@example.com","refresh_token":"secret"}}`
	payload, _ := json.Marshal(map[string]string{"content": body, "filename": "codex-wrapper.json"})
	req := httptest.NewRequest(http.MethodPost, "/_admin/api/cliproxy/auth-files", strings.NewReader(string(payload)))
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileCreate(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(authDir, "codex-wrapper.json"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("decode written file: %v", err)
	}
	if written["type"] != "codex" || written["email"] != "user@example.com" || written["refresh_token"] != "secret" {
		t.Fatalf("written auth = %#v, want flattened codex metadata", written)
	}
	if _, exists := written["metadata"]; exists {
		t.Fatalf("metadata wrapper was not flattened: %#v", written)
	}
}

func TestHandleCLIProxyAuthFileCreateAcceptsCodexCLIAuthJSON(t *testing.T) {
	authDir := t.TempDir()
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})

	body := `{"auth_mode":"apikey","OPENAI_API_KEY":"sk-test"}`
	payload, _ := json.Marshal(map[string]string{"content": body, "filename": "codex-cli.json"})
	req := httptest.NewRequest(http.MethodPost, "/_admin/api/cliproxy/auth-files", strings.NewReader(string(payload)))
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileCreate(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(authDir, "codex-cli.json"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("decode written file: %v", err)
	}
	if written["type"] != "codex" || written["access_token"] != "sk-test" {
		t.Fatalf("written auth = %#v, want codex auth with access_token", written)
	}
}

func TestHandleCLIProxyAuthFileCreateAcceptsCodexCLITokenJSON(t *testing.T) {
	authDir := t.TempDir()
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})

	body := `{"auth_mode":"chatgpt","tokens":{"access_token":"access","refresh_token":"refresh","id_token":"id"}}`
	payload, _ := json.Marshal(map[string]string{"content": body, "filename": "codex-cli-tokens.json"})
	req := httptest.NewRequest(http.MethodPost, "/_admin/api/cliproxy/auth-files", strings.NewReader(string(payload)))
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileCreate(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(authDir, "codex-cli-tokens.json"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("decode written file: %v", err)
	}
	if written["type"] != "codex" || written["access_token"] != "access" || written["refresh_token"] != "refresh" || written["id_token"] != "id" {
		t.Fatalf("written auth = %#v, want flattened codex tokens", written)
	}
}

func TestHandleCLIProxyAuthFilesList(t *testing.T) {
	authDir := t.TempDir()
	content := `{"type":"claude","label":"team-b"}`
	if err := os.WriteFile(filepath.Join(authDir, "claude-team-b-aabbcc.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/_admin/api/cliproxy/auth-files", nil)
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFilesList(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp cliproxyAuthFileListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AuthDir != authDir {
		t.Fatalf("auth_dir = %q, want %q", resp.AuthDir, authDir)
	}
	if len(resp.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(resp.Files))
	}
	if resp.Files[0].Provider != "claude" {
		t.Fatalf("provider = %q, want claude", resp.Files[0].Provider)
	}
	if resp.Files[0].ValidationStatus != cliproxyAuthValidationWarning {
		t.Fatalf("validation_status = %q, want warning", resp.Files[0].ValidationStatus)
	}
	if !strings.HasPrefix(resp.Files[0].Filename, "claude-team-b-") || !strings.HasSuffix(resp.Files[0].Filename, ".json") {
		t.Fatalf("filename = %q, want claude-team-b-*.json", resp.Files[0].Filename)
	}
}

func TestHandleCLIProxyAuthFileCreateRejectsUnsafeFilename(t *testing.T) {
	authDir := t.TempDir()
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/_admin/api/cliproxy/auth-files", strings.NewReader(`{"content":"{\"type\":\"codex\"}","filename":"../escape.json"}`))
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileCreate(rec, req, nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleCLIProxyAuthFileDeleteRemovesAuthJSON(t *testing.T) {
	authDir := t.TempDir()
	filePath := filepath.Join(authDir, "codex-team.json")
	if err := os.WriteFile(filePath, []byte(`{"type":"codex","email":"user@example.com"}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})
	req := httptest.NewRequest(http.MethodDelete, "/_admin/api/cliproxy/auth-files?filename=codex-team.json", nil)
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileDelete(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("deleted file stat err = %v, want not exist", err)
	}
}

func TestHandleCLIProxyAuthFileDeleteRejectsUnsafeFilename(t *testing.T) {
	authDir := t.TempDir()
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})
	req := httptest.NewRequest(http.MethodDelete, "/_admin/api/cliproxy/auth-files?filename=../escape.json", nil)
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileDelete(rec, req, nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleCLIProxyAuthFileUsageReadsSanitizedState(t *testing.T) {
	authDir := t.TempDir()
	content := `{
		"type":"codex",
		"label":"team-a",
		"email":"user@example.com",
		"access_token":"secret-token",
		"plan_type":"plus",
		"status":"active",
		"reset_at":"2026-05-10T13:00:00Z",
		"usage":{
			"five_hour":{"used":12,"limit":300,"reset_at":"2026-05-10T12:00:00Z"},
			"weekly":{"used":120,"limit":900,"reset_at":"2026-05-12T00:00:00Z"}
		},
		"quota":{"exceeded":true,"reason":"quota","next_recover_at":"2026-05-10T12:00:00Z","backoff_level":2},
		"model_states":{
			"gpt-5.5":{"status":"error","status_message":"quota exhausted","unavailable":true,"quota":{"exceeded":true,"reason":"quota"}}
		}
	}`
	if err := os.WriteFile(filepath.Join(authDir, "codex-team.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/_admin/api/cliproxy/auth-files/usage?filename=codex-team.json", nil)
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileUsage(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret-token") {
		t.Fatalf("usage response leaked access token: %s", body)
	}
	var resp cliproxyAuthFileUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Provider != "codex" || resp.AccountInfo != "user@example.com" {
		t.Fatalf("usage summary = %#v, want provider/plan/account", resp)
	}
	if resp.Status != "warning" {
		t.Fatalf("status = %q, want warning", resp.Status)
	}
	summary := usageSummaryMap(resp.Summary)
	if summary["plan"] != "plus" || summary["5h"] != "12/300" || summary["5h_reset"] != "2026-05-10T12:00:00Z" || summary["weekly"] != "120/900" || summary["weekly_reset"] != "2026-05-12T00:00:00Z" || summary["quota"] != "exceeded" {
		t.Fatalf("summary = %#v, want plan and quota metrics", resp.Summary)
	}
	if string(resp.Data["quota"]) == "" || string(resp.Data["model_states"]) == "" || string(resp.Data["reset_at"]) != `"2026-05-10T13:00:00Z"` {
		t.Fatalf("data = %#v, want quota and model_states passthrough", resp.Data)
	}
}

func TestHandleCLIProxyAuthFileUsageCacheInvalidatesWhenFileChanges(t *testing.T) {
	authDir := t.TempDir()
	filePath := filepath.Join(authDir, "codex-team.json")
	firstContent := []byte(`{"type":"codex","email":"user@example.com","plan_type":"first","usage":{"remaining":1},"quota":{"exceeded":true,"reason":"first"}}`)
	if err := os.WriteFile(filePath, firstContent, 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})

	req1 := httptest.NewRequest(http.MethodGet, "/_admin/api/cliproxy/auth-files/usage?filename=codex-team.json", nil)
	rec1 := httptest.NewRecorder()
	handler.HandleCLIProxyAuthFileUsage(rec1, req1, nil)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d; body=%s", rec1.Code, http.StatusOK, rec1.Body.String())
	}

	reqCached := httptest.NewRequest(http.MethodGet, "/_admin/api/cliproxy/auth-files/usage?filename=codex-team.json", nil)
	recCached := httptest.NewRecorder()
	handler.HandleCLIProxyAuthFileUsage(recCached, reqCached, nil)
	if recCached.Code != http.StatusOK {
		t.Fatalf("cached status = %d, want %d; body=%s", recCached.Code, http.StatusOK, recCached.Body.String())
	}
	var cachedResp cliproxyAuthFileUsageResponse
	if err := json.Unmarshal(recCached.Body.Bytes(), &cachedResp); err != nil {
		t.Fatalf("decode cached response: %v", err)
	}
	if !cachedResp.Cached {
		t.Fatalf("cached = false, want true before file changes")
	}

	secondContent := []byte(`{"type":"codex","email":"user@example.com","plan_type":"second","usage":{"remaining":2},"quota":{"exceeded":true,"reason":"second"}}`)
	if err := os.WriteFile(filePath, secondContent, 0o600); err != nil {
		t.Fatalf("rewrite auth file: %v", err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(filePath, future, future); err != nil {
		t.Fatalf("touch auth file: %v", err)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/_admin/api/cliproxy/auth-files/usage?filename=codex-team.json", nil)
	rec2 := httptest.NewRecorder()
	handler.HandleCLIProxyAuthFileUsage(rec2, req2, nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d; body=%s", rec2.Code, http.StatusOK, rec2.Body.String())
	}
	var resp cliproxyAuthFileUsageResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Cached {
		t.Fatalf("cached = true, want false after file changes")
	}
	summary := usageSummaryMap(resp.Summary)
	if summary["plan"] != "second" {
		t.Fatalf("summary = %#v, want refreshed plan=second", resp.Summary)
	}
}

func TestHandleCLIProxyAuthFileUsageRedactsAPIKeyAccountInfo(t *testing.T) {
	authDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(authDir, "openai-key.json"), []byte(`{"type":"openai","api_key":"sk-secret","usage":{"remaining":10}}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/_admin/api/cliproxy/auth-files/usage?filename=openai-key.json", nil)
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileUsage(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "sk-secret") {
		t.Fatalf("usage response leaked api key: %s", body)
	}
	var resp cliproxyAuthFileUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AccountKind != "api_key" || resp.AccountInfo != "configured" {
		t.Fatalf("account = %q/%q, want api_key/configured", resp.AccountKind, resp.AccountInfo)
	}
}

func TestHandleCLIProxyAuthFileUsageExtractsCodexPlanFromIDToken(t *testing.T) {
	authDir := t.TempDir()
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_plan_type":"plus"}}`))
	token := "header." + claims + ".signature"
	if err := os.WriteFile(filepath.Join(authDir, "codex-oauth.json"), []byte(`{"type":"codex","email":"user@example.com","id_token":"`+token+`"}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/_admin/api/cliproxy/auth-files/usage?filename=codex-oauth.json", nil)
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileUsage(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, token) {
		t.Fatalf("usage response leaked id token: %s", body)
	}
	var resp cliproxyAuthFileUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q, want ok; note=%q", resp.Status, resp.Note)
	}
	if len(resp.Summary) != 1 || resp.Summary[0].Name != "plan" || resp.Summary[0].Value != "plus" {
		t.Fatalf("summary = %#v, want plan plus", resp.Summary)
	}
}

func TestHandleCLIProxyAuthFileUsageWithoutMetadataIsQuietUnknown(t *testing.T) {
	authDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(authDir, "codex-basic.json"), []byte(`{"type":"codex","email":"user@example.com","access_token":"secret"}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/_admin/api/cliproxy/auth-files/usage?filename=codex-basic.json", nil)
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileUsage(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp cliproxyAuthFileUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "unknown" || resp.Note != "" {
		t.Fatalf("status/note = %q/%q, want quiet unknown", resp.Status, resp.Note)
	}
}

func TestHandleCLIProxyAuthFileUsageMergesRuntimeQuotaFromSelector(t *testing.T) {
	authDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(authDir, "codex-team.json"), []byte(`{"type":"codex","email":"user@example.com","access_token":"secret","plan_type":"plus"}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	cfg := &config.ConfigStruct{
		CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		Provider: map[string]*config.ProviderConfig{
			"codex-pro-lite": {
				Name:            "codex-pro-lite",
				Family:          config.ProviderProtocolOpenAI,
				Backend:         config.ProviderBackendCLIProxy,
				BackendProvider: "codex",
				URL:             "http://127.0.0.1:18741/v1",
			},
		},
	}
	selector := sel.NewSelector(cfg)
	resetAt := time.Date(2026, 5, 10, 6, 30, 0, 0, time.UTC).Unix()
	selector.RecordOutcomeWithSource("codex-pro-lite", &sel.UpstreamError{
		Code: http.StatusTooManyRequests,
		Body: `{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","plan_type":"plus","resets_at":` + strconv.FormatInt(resetAt, 10) + `,"resets_in_seconds":1200}}`,
	}, 10*time.Millisecond, "pre_stream")
	selector.RecordOutcomeWithSource("codex-pro-lite", &sel.UpstreamError{
		Code: http.StatusTooManyRequests,
		Body: `{"error":{"code":"model_cooldown","message":"All credentials for model gpt-5.5 are cooling down via provider codex","model":"gpt-5.5","provider":"codex","reset_seconds":1200,"reset_time":"20m0s"}}`,
	}, 10*time.Millisecond, "pre_stream")
	handler := NewHandler(Deps{
		Cfg:      cfg,
		Selector: selector,
	})
	req := httptest.NewRequest(http.MethodGet, "/_admin/api/cliproxy/auth-files/usage?filename=codex-team.json", nil)
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileUsage(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret") {
		t.Fatalf("usage response leaked credential: %s", body)
	}
	var resp cliproxyAuthFileUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	summary := usageSummaryMap(resp.Summary)
	if summary["plan"] != "plus" || summary["5h"] != "limited" || summary["5h_reset"] != "2026-05-10T06:30:00Z" {
		t.Fatalf("summary = %#v, want runtime 5h quota data", resp.Summary)
	}
	if resp.Status != "warning" {
		t.Fatalf("status = %q, want warning", resp.Status)
	}
	if string(resp.Data["runtime_quota"]) == "" {
		t.Fatalf("data = %#v, want runtime_quota", resp.Data)
	}
}

func TestHandleCLIProxyAuthFileUsagePrefersNewerRuntimeAuthError(t *testing.T) {
	authDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(authDir, "codex-team.json"), []byte(`{"type":"codex","email":"user@example.com","access_token":"secret","plan_type":"plus"}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	cfg := &config.ConfigStruct{
		CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		Provider: map[string]*config.ProviderConfig{
			"codex-pro-lite": {
				Name:            "codex-pro-lite",
				Family:          config.ProviderProtocolOpenAI,
				Backend:         config.ProviderBackendCLIProxy,
				BackendProvider: "codex",
				URL:             "http://127.0.0.1:18741/v1",
			},
		},
	}
	selector := sel.NewSelector(cfg)
	selector.RecordOutcomeWithSource("codex-pro-lite", &sel.UpstreamError{
		Code: http.StatusTooManyRequests,
		Body: `{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","plan_type":"plus","resets_in_seconds":1200}}`,
	}, 10*time.Millisecond, "pre_stream")
	time.Sleep(time.Millisecond)
	selector.RecordOutcomeWithSource("codex-pro-lite", &sel.UpstreamError{
		Code: http.StatusUnauthorized,
		Body: `{"error":{"message":"Your authentication token has been invalidated. Please try signing in again.","type":"authentication_error","code":"auth_unavailable"}}`,
	}, 10*time.Millisecond, "pre_stream")
	handler := NewHandler(Deps{
		Cfg:      cfg,
		Selector: selector,
	})
	req := httptest.NewRequest(http.MethodGet, "/_admin/api/cliproxy/auth-files/usage?filename=codex-team.json", nil)
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileUsage(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp cliproxyAuthFileUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	summary := usageSummaryMap(resp.Summary)
	if summary["auth"] != "invalidated" {
		t.Fatalf("summary = %#v, want auth invalidated", resp.Summary)
	}
	if _, ok := summary["5h"]; ok {
		t.Fatalf("summary = %#v, should not show stale 5h limit after newer auth error", resp.Summary)
	}
	if resp.Status != "error" {
		t.Fatalf("status = %q, want error", resp.Status)
	}
	if string(resp.Data["runtime_auth"]) == "" {
		t.Fatalf("data = %#v, want runtime_auth", resp.Data)
	}
}

func usageSummaryMap(summary []cliproxyAuthUsageMetric) map[string]string {
	values := make(map[string]string, len(summary))
	for _, item := range summary {
		values[item.Name] = item.Value
	}
	return values
}

func TestHandleCLIProxyAuthFileCreateRejectsMissingType(t *testing.T) {
	authDir := t.TempDir()
	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/_admin/api/cliproxy/auth-files", strings.NewReader(`{"content":"{\"email\":\"user@example.com\"}"}`))
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileCreate(rec, req, nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleCLIProxyAuthFileVerifySendsBackendProbe(t *testing.T) {
	authDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(authDir, "codex-team.json"), []byte(`{"type":"codex","email":"user@example.com"}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	var gotPath string
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method == http.MethodGet && r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-test"}]}`))
			return
		}
		data, _ := io.ReadAll(r.Body)
		gotBody = string(data)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_verify","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer upstream.Close()

	handler := NewHandler(Deps{
		Cfg: &config.ConfigStruct{
			CLIProxy: &config.CLIProxyConfig{AuthDir: authDir},
			Provider: map[string]*config.ProviderConfig{
				"cliproxy-codex": {
					URL:             upstream.URL,
					Protocol:        config.ProviderProtocolOpenAI,
					Backend:         config.ProviderBackendCLIProxy,
					BackendProvider: "codex",
				},
			},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/_admin/api/cliproxy/auth-files/verify", strings.NewReader(`{"provider":"cliproxy-codex","filename":"codex-team.json"}`))
	rec := httptest.NewRecorder()

	handler.HandleCLIProxyAuthFileVerify(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotPath != "/responses" {
		t.Fatalf("path = %q, want /responses", gotPath)
	}
	if !strings.Contains(gotBody, `"model":"gpt-test"`) {
		t.Fatalf("request body = %s, want model gpt-test", gotBody)
	}
	if !strings.Contains(gotBody, `"input":"ping"`) {
		t.Fatalf("request body = %s, want input ping", gotBody)
	}
	if !strings.Contains(gotBody, `"store":false`) {
		t.Fatalf("request body = %s, want store false", gotBody)
	}
	if strings.Contains(gotBody, `"messages"`) || strings.Contains(gotBody, `"max_tokens"`) || strings.Contains(gotBody, `"max_completion_tokens"`) || strings.Contains(gotBody, `"max_output_tokens"`) {
		t.Fatalf("request body = %s, want no token limit parameter", gotBody)
	}
	var resp cliproxyAuthFileVerifyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q, want ok; error=%q", resp.Status, resp.Error)
	}
	if resp.Protocol != config.RouteProtocolResponsesStateless {
		t.Fatalf("protocol = %q, want %q", resp.Protocol, config.RouteProtocolResponsesStateless)
	}
}
