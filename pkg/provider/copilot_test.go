package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadGitHubToken(t *testing.T) {
	dir := t.TempDir()

	t.Run("hosts.json with github.com entry", func(t *testing.T) {
		hosts := map[string]any{
			"github.com": map[string]string{
				"oauth_token": "gho_test123",
			},
		}
		data, _ := json.Marshal(hosts)
		os.WriteFile(filepath.Join(dir, "hosts.json"), data, 0644)

		token, err := readGitHubToken(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "gho_test123" {
			t.Errorf("got %q, want %q", token, "gho_test123")
		}
	})

	t.Run("apps.json fallback", func(t *testing.T) {
		appsDir := filepath.Join(dir, "apps-only")
		os.MkdirAll(appsDir, 0755)

		apps := map[string]any{
			"github.com": map[string]string{
				"oauth_token": "gho_apps456",
			},
		}
		data, _ := json.Marshal(apps)
		os.WriteFile(filepath.Join(appsDir, "apps.json"), data, 0644)

		token, err := readGitHubToken(appsDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "gho_apps456" {
			t.Errorf("got %q, want %q", token, "gho_apps456")
		}
	})

	t.Run("missing files", func(t *testing.T) {
		emptyDir := filepath.Join(dir, "empty")
		os.MkdirAll(emptyDir, 0755)

		_, err := readGitHubToken(emptyDir)
		if err == nil {
			t.Fatal("expected error for missing files")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		badDir := filepath.Join(dir, "bad")
		os.MkdirAll(badDir, 0755)
		os.WriteFile(filepath.Join(badDir, "hosts.json"), []byte("not json"), 0644)

		_, err := readGitHubToken(badDir)
		if err == nil {
			t.Fatal("expected error for invalid json")
		}
	})

	t.Run("empty oauth_token", func(t *testing.T) {
		emptyTokenDir := filepath.Join(dir, "empty-token")
		os.MkdirAll(emptyTokenDir, 0755)

		hosts := map[string]any{
			"github.com": map[string]string{
				"oauth_token": "",
			},
		}
		data, _ := json.Marshal(hosts)
		os.WriteFile(filepath.Join(emptyTokenDir, "hosts.json"), data, 0644)

		_, err := readGitHubToken(emptyTokenDir)
		if err == nil {
			t.Fatal("expected error for empty oauth_token")
		}
	})
}

func TestCopilotTokenExpiry(t *testing.T) {
	t.Run("nil token is expired", func(t *testing.T) {
		m := &tokenManager{}
		if !m.isCopilotTokenExpired() {
			t.Fatal("expected nil token to be expired")
		}
	})

	t.Run("future token is not expired", func(t *testing.T) {
		m := &tokenManager{
			token: &copilotToken{
				Token:     "valid",
				ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
			},
		}
		if m.isCopilotTokenExpired() {
			t.Fatal("expected future token to not be expired")
		}
	})

	t.Run("past token is expired", func(t *testing.T) {
		m := &tokenManager{
			token: &copilotToken{
				Token:     "expired",
				ExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
			},
		}
		if !m.isCopilotTokenExpired() {
			t.Fatal("expected past token to be expired")
		}
	})

	t.Run("token within buffer is expired", func(t *testing.T) {
		m := &tokenManager{
			token: &copilotToken{
				Token:     "almost-expired",
				ExpiresAt: time.Now().Add(10 * time.Second).Unix(),
			},
		}
		if !m.isCopilotTokenExpired() {
			t.Fatal("expected token within buffer to be expired")
		}
	})
}

func TestCopilotGetAccessToken(t *testing.T) {
	p := &copilotProvider{managers: make(map[string]*tokenManager)}

	t.Run("pre-loaded token returned", func(t *testing.T) {
		dir := t.TempDir()
		hosts := map[string]any{
			"github.com": map[string]string{
				"oauth_token": "gho_test_exchange",
			},
		}
		data, _ := json.Marshal(hosts)
		os.WriteFile(filepath.Join(dir, "hosts.json"), data, 0644)

		mgr := p.getManager(dir)
		mgr.mu.Lock()
		mgr.ghToken = "gho_test_exchange"
		mgr.token = &copilotToken{
			Token:     "tid=copilot-token-abc",
			ExpiresAt: time.Now().Add(30 * time.Minute).Unix(),
		}
		mgr.mu.Unlock()

		token, err := p.GetAccessToken(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "tid=copilot-token-abc" {
			t.Errorf("got %q, want %q", token, "tid=copilot-token-abc")
		}
	})
}
