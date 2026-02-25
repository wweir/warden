package qwen

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wweir/warden/pkg/ssh"
)

const (
	oauthTokenEndpoint = "https://chat.qwen.ai/api/v1/oauth2/token"
	oauthClientID      = "f0304373b74a44d2b584a3fb70ca9e56"
	// refresh token 30s before expiry
	tokenExpiryBuffer = 30 * time.Second
)

type oauthCreds struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ResourceURL  string `json:"resource_url"`
	ExpiryDate   int64  `json:"expiry_date"` // unix timestamp in milliseconds
}

// oauthManager handles OAuth token lifecycle for a single provider.
type oauthManager struct {
	mu        sync.Mutex
	configDir string
	sshCfg    *ssh.Config
	creds     *oauthCreds
}

var (
	managersMu sync.Mutex
	managers   = make(map[string]*oauthManager)
)

// getOAuthManager returns a singleton oauthManager per configDir+sshHost.
func getOAuthManager(configDir string, sshCfg *ssh.Config) *oauthManager {
	managersMu.Lock()
	defer managersMu.Unlock()

	key := configDir
	if sshCfg != nil {
		key = configDir + "@" + sshCfg.Host
	}

	if m, ok := managers[key]; ok {
		return m
	}
	m := &oauthManager{configDir: configDir, sshCfg: sshCfg}
	managers[key] = m
	return m
}

// getAccessToken returns a valid access token, refreshing if needed.
func (m *oauthManager) getAccessToken() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// load credentials from file on first call
	if m.creds == nil {
		creds, err := readOAuthCreds(m.configDir, m.sshCfg)
		if err != nil {
			return "", err
		}
		m.creds = creds
	}

	// refresh if token is expired or about to expire
	if m.isExpired() {
		if err := m.refresh(); err != nil {
			return "", fmt.Errorf("refresh oauth token: %w", err)
		}
	}

	return m.creds.AccessToken, nil
}

func (m *oauthManager) isExpired() bool {
	if m.creds.ExpiryDate == 0 {
		return false
	}
	return time.Now().Add(tokenExpiryBuffer).UnixMilli() >= m.creds.ExpiryDate
}

func (m *oauthManager) refresh() error {
	if m.creds.RefreshToken == "" {
		return fmt.Errorf("no refresh_token available, re-authenticate with qwen CLI")
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {m.creds.RefreshToken},
		"client_id":     {oauthClientID},
	}

	resp, err := http.Post(oauthTokenEndpoint, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("post token request: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		ResourceURL  string `json:"resource_url"`
		ExpiresIn    int64  `json:"expires_in"` // seconds
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("decode token response: %w", err)
	}
	if tokenResp.Error != "" {
		return fmt.Errorf("token endpoint error: %s", tokenResp.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, tokenResp.Error)
	}

	// update credentials
	m.creds.AccessToken = tokenResp.AccessToken
	m.creds.TokenType = tokenResp.TokenType
	m.creds.ResourceURL = tokenResp.ResourceURL
	m.creds.ExpiryDate = time.Now().UnixMilli() + tokenResp.ExpiresIn*1000
	if tokenResp.RefreshToken != "" {
		m.creds.RefreshToken = tokenResp.RefreshToken
	}

	// persist updated credentials (skip in SSH mode)
	if m.sshCfg != nil {
		slog.Warn("SSH mode: skipping oauth creds persistence", "config_dir", m.configDir, "host", m.sshCfg.Host)
	} else if err := writeOAuthCreds(m.configDir, m.creds); err != nil {
		return fmt.Errorf("persist refreshed creds: %w", err)
	}

	return nil
}

// GetAccessToken returns a valid access token for the given config directory,
// refreshing automatically if the token is expired or about to expire.
// If sshCfg is non-nil, credentials are read from the remote host via SSH.
func GetAccessToken(configDir string, sshCfg *ssh.Config) (string, error) {
	return getOAuthManager(configDir, sshCfg).getAccessToken()
}

// readOAuthCreds reads the full OAuth credentials from disk or remote host.
func readOAuthCreds(configDir string, sshCfg *ssh.Config) (*oauthCreds, error) {
	path := filepath.Join(configDir, "oauth_creds.json")

	var data []byte
	var err error
	if sshCfg != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		data, err = ssh.ReadFile(ctx, sshCfg, path)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read oauth creds %s: %w", path, err)
	}

	var creds oauthCreds
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse oauth creds %s: %w", path, err)
	}

	if creds.AccessToken == "" {
		return nil, fmt.Errorf("oauth creds %s: access_token is empty", path)
	}

	return &creds, nil
}

// writeOAuthCreds persists OAuth credentials to disk.
func writeOAuthCreds(configDir string, creds *oauthCreds) error {
	path := filepath.Join(configDir, "oauth_creds.json")
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal oauth creds: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}
