package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wweir/warden/config"
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
