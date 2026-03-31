package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQwenGetAccessToken(t *testing.T) {
	p := &qwenProvider{managers: make(map[string]*oauthManager)}
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

		token, err := p.GetAccessToken(context.Background(), subDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "test-token-123" {
			t.Errorf("got %q, want %q", token, "test-token-123")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		p2 := &qwenProvider{managers: make(map[string]*oauthManager)}
		nonexistent := filepath.Join(dir, "nonexistent")
		_, err := p2.GetAccessToken(context.Background(), nonexistent)
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		p2 := &qwenProvider{managers: make(map[string]*oauthManager)}
		badDir := filepath.Join(dir, "bad")
		os.MkdirAll(badDir, 0755)
		os.WriteFile(filepath.Join(badDir, "oauth_creds.json"), []byte(`not json`), 0644)

		_, err := p2.GetAccessToken(context.Background(), badDir)
		if err == nil {
			t.Fatal("expected error for invalid json")
		}
	})

	t.Run("empty access_token", func(t *testing.T) {
		p2 := &qwenProvider{managers: make(map[string]*oauthManager)}
		emptyDir := filepath.Join(dir, "empty")
		os.MkdirAll(emptyDir, 0755)
		os.WriteFile(filepath.Join(emptyDir, "oauth_creds.json"), []byte(`{"access_token":""}`), 0644)

		_, err := p2.GetAccessToken(context.Background(), emptyDir)
		if err == nil {
			t.Fatal("expected error for empty access_token")
		}
	})
}

func TestQwenTokenExpiry(t *testing.T) {
	t.Run("expired token detected", func(t *testing.T) {
		m := &oauthManager{
			creds: &oauthCreds{
				AccessToken:  "expired-token",
				RefreshToken: "old-refresh",
				ExpiryDate:   time.Now().Add(-1 * time.Hour).UnixMilli(),
			},
		}
		if !m.isQwenTokenExpired() {
			t.Fatal("expected token to be expired")
		}
	})

	t.Run("token within buffer is expired", func(t *testing.T) {
		m := &oauthManager{
			creds: &oauthCreds{
				AccessToken: "about-to-expire",
				ExpiryDate:  time.Now().Add(10 * time.Second).UnixMilli(),
			},
		}
		if !m.isQwenTokenExpired() {
			t.Fatal("expected token within buffer to be considered expired")
		}
	})

	t.Run("zero expiry never expires", func(t *testing.T) {
		m := &oauthManager{
			creds: &oauthCreds{
				AccessToken: "no-expiry-token",
				ExpiryDate:  0,
			},
		}
		if m.isQwenTokenExpired() {
			t.Fatal("expected zero expiry to not be considered expired")
		}
	})

	t.Run("refresh without refresh_token fails", func(t *testing.T) {
		m := &oauthManager{
			creds: &oauthCreds{
				AccessToken:  "token",
				RefreshToken: "",
				ExpiryDate:   time.Now().Add(-1 * time.Hour).UnixMilli(),
			},
		}
		err := m.refreshQwenToken(context.Background())
		if err == nil {
			t.Fatal("expected error when refresh_token is empty")
		}
	})
}

func TestQwenWriteReadCreds(t *testing.T) {
	writeDir := filepath.Join(t.TempDir(), "write")
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

	read, err := readOAuthCreds(writeDir)
	if err != nil {
		t.Fatalf("read creds: %v", err)
	}
	if read.AccessToken != creds.AccessToken {
		t.Errorf("access_token: got %q, want %q", read.AccessToken, creds.AccessToken)
	}
	if read.RefreshToken != creds.RefreshToken {
		t.Errorf("refresh_token: got %q, want %q", read.RefreshToken, creds.RefreshToken)
	}

	info, _ := os.Stat(filepath.Join(writeDir, "oauth_creds.json"))
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file permissions: got %o, want 0600", perm)
	}
}
