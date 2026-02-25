package qwen

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGetAccessToken(t *testing.T) {
	// clear global managers to avoid cross-test pollution
	managersMu.Lock()
	managers = make(map[string]*oauthManager)
	managersMu.Unlock()

	dir := t.TempDir()

	t.Run("valid non-expired token", func(t *testing.T) {
		subDir := filepath.Join(dir, "valid")
		os.MkdirAll(subDir, 0755)
		creds := oauthCreds{
			AccessToken:  "test-token-123",
			RefreshToken: "ref",
			ExpiryDate:   time.Now().Add(1 * time.Hour).UnixMilli(),
		}
		data, _ := json.Marshal(creds)
		os.WriteFile(filepath.Join(subDir, "oauth_creds.json"), data, 0644)

		// reset manager
		managersMu.Lock()
		delete(managers, subDir)
		managersMu.Unlock()

		token, err := GetAccessToken(subDir, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "test-token-123" {
			t.Errorf("got %q, want %q", token, "test-token-123")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		nonexistent := filepath.Join(dir, "nonexistent")
		managersMu.Lock()
		delete(managers, nonexistent)
		managersMu.Unlock()

		_, err := GetAccessToken(nonexistent, nil)
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		badDir := filepath.Join(dir, "bad")
		os.MkdirAll(badDir, 0755)
		os.WriteFile(filepath.Join(badDir, "oauth_creds.json"), []byte(`not json`), 0644)

		managersMu.Lock()
		delete(managers, badDir)
		managersMu.Unlock()

		_, err := GetAccessToken(badDir, nil)
		if err == nil {
			t.Fatal("expected error for invalid json")
		}
	})

	t.Run("empty access_token", func(t *testing.T) {
		emptyDir := filepath.Join(dir, "empty")
		os.MkdirAll(emptyDir, 0755)
		os.WriteFile(filepath.Join(emptyDir, "oauth_creds.json"), []byte(`{"access_token":""}`), 0644)

		managersMu.Lock()
		delete(managers, emptyDir)
		managersMu.Unlock()

		_, err := GetAccessToken(emptyDir, nil)
		if err == nil {
			t.Fatal("expected error for empty access_token")
		}
	})
}

func TestTokenRefresh(t *testing.T) {
	// mock OAuth token endpoint
	_ = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.FormValue("grant_type") != "refresh_token" {
			t.Errorf("unexpected grant_type: %s", r.FormValue("grant_type"))
		}
		if r.FormValue("client_id") != oauthClientID {
			t.Errorf("unexpected client_id: %s", r.FormValue("client_id"))
		}
		if r.FormValue("refresh_token") == "" {
			t.Error("missing refresh_token")
		}

		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "refreshed-token-456",
			"token_type":    "Bearer",
			"refresh_token": "new-refresh-token",
			"resource_url":  "portal.qwen.ai",
			"expires_in":    3600,
		})
	}))

	dir := t.TempDir()

	t.Run("expired token detected", func(t *testing.T) {
		m := &oauthManager{
			configDir: dir,
			creds: &oauthCreds{
				AccessToken:  "expired-token",
				RefreshToken: "old-refresh",
				ExpiryDate:   time.Now().Add(-1 * time.Hour).UnixMilli(),
			},
		}
		m.mu.Lock()
		expired := m.isExpired()
		m.mu.Unlock()

		if !expired {
			t.Fatal("expected token to be expired")
		}
	})

	t.Run("not expired token skips refresh", func(t *testing.T) {
		futureDir := filepath.Join(dir, "future")
		os.MkdirAll(futureDir, 0755)
		creds := oauthCreds{
			AccessToken:  "valid-token",
			RefreshToken: "ref",
			ExpiryDate:   time.Now().Add(1 * time.Hour).UnixMilli(),
		}
		data, _ := json.Marshal(creds)
		os.WriteFile(filepath.Join(futureDir, "oauth_creds.json"), data, 0644)

		managersMu.Lock()
		delete(managers, futureDir)
		managersMu.Unlock()

		token, err := GetAccessToken(futureDir, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "valid-token" {
			t.Errorf("got %q, want %q", token, "valid-token")
		}
	})

	t.Run("token near expiry triggers refresh", func(t *testing.T) {
		m := &oauthManager{
			configDir: dir,
			creds: &oauthCreds{
				AccessToken:  "about-to-expire",
				RefreshToken: "ref",
				ExpiryDate:   time.Now().Add(10 * time.Second).UnixMilli(), // within 30s buffer
			},
		}
		m.mu.Lock()
		expired := m.isExpired()
		m.mu.Unlock()

		if !expired {
			t.Fatal("expected token within buffer to be considered expired")
		}
	})

	t.Run("zero expiry never expires", func(t *testing.T) {
		m := &oauthManager{
			configDir: dir,
			creds: &oauthCreds{
				AccessToken: "no-expiry-token",
				ExpiryDate:  0,
			},
		}
		m.mu.Lock()
		expired := m.isExpired()
		m.mu.Unlock()

		if expired {
			t.Fatal("expected zero expiry to not be considered expired")
		}
	})

	t.Run("refresh without refresh_token fails", func(t *testing.T) {
		m := &oauthManager{
			configDir: dir,
			creds: &oauthCreds{
				AccessToken:  "token",
				RefreshToken: "",
				ExpiryDate:   time.Now().Add(-1 * time.Hour).UnixMilli(),
			},
		}

		err := m.refresh()
		if err == nil {
			t.Fatal("expected error when refresh_token is empty")
		}
	})

	t.Run("write and read back creds", func(t *testing.T) {
		writeDir := filepath.Join(dir, "write")
		os.MkdirAll(writeDir, 0755)

		creds := &oauthCreds{
			AccessToken:  "written-token",
			TokenType:    "Bearer",
			RefreshToken: "written-refresh",
			ResourceURL:  "portal.qwen.ai",
			ExpiryDate:   time.Now().Add(1 * time.Hour).UnixMilli(),
		}
		if err := writeOAuthCreds(writeDir, creds); err != nil {
			t.Fatalf("write creds: %v", err)
		}

		read, err := readOAuthCreds(writeDir, nil)
		if err != nil {
			t.Fatalf("read creds: %v", err)
		}
		if read.AccessToken != creds.AccessToken {
			t.Errorf("access_token: got %q, want %q", read.AccessToken, creds.AccessToken)
		}
		if read.RefreshToken != creds.RefreshToken {
			t.Errorf("refresh_token: got %q, want %q", read.RefreshToken, creds.RefreshToken)
		}
		if read.ExpiryDate != creds.ExpiryDate {
			t.Errorf("expiry_date: got %d, want %d", read.ExpiryDate, creds.ExpiryDate)
		}

		// check file permissions
		info, _ := os.Stat(filepath.Join(writeDir, "oauth_creds.json"))
		perm := info.Mode().Perm()
		if perm != 0600 {
			t.Errorf("file permissions: got %o, want 0600", perm)
		}
	})
}
