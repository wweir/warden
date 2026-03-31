package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	qwenOAuthTokenEndpoint = "https://chat.qwen.ai/api/v1/oauth2/token"
	qwenOAuthClientID      = "f0304373b74a44d2b584a3fb70ca9e56"
)

type oauthCreds struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ResourceURL  string `json:"resource_url"`
	ExpiryDate   int64  `json:"expiry_date"` // unix timestamp in milliseconds
}

// oauthManager handles Qwen OAuth token lifecycle for a single config directory.
type oauthManager struct {
	mu        sync.Mutex
	configDir string
	creds     *oauthCreds
}

// qwenProvider implements TokenProvider for Qwen.
type qwenProvider struct {
	mu       sync.Mutex
	managers map[string]*oauthManager
}

func (p *qwenProvider) getManager(configDir string) *oauthManager {
	p.mu.Lock()
	defer p.mu.Unlock()

	if m, ok := p.managers[configDir]; ok {
		return m
	}
	m := &oauthManager{configDir: configDir}
	p.managers[configDir] = m
	return m
}

func (p *qwenProvider) GetAccessToken(ctx context.Context, configDir string) (string, error) {
	return p.getManager(configDir).getAccessToken(ctx)
}

func (p *qwenProvider) InvalidateAuth(configDir string) {
	m := p.getManager(configDir)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creds = nil
}

func (p *qwenProvider) CheckCredsReadable(_ context.Context, configDir string) error {
	_, err := readOAuthCreds(configDir)
	return err
}

// getAccessToken returns a valid access token, refreshing if needed.
func (m *oauthManager) getAccessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.creds == nil {
		creds, err := readOAuthCreds(m.configDir)
		if err != nil {
			return "", err
		}
		m.creds = creds
	}

	if m.isQwenTokenExpired() {
		if err := m.refreshQwenToken(ctx); err != nil {
			return "", fmt.Errorf("refresh oauth token: %w", err)
		}
	}

	return m.creds.AccessToken, nil
}

func (m *oauthManager) isQwenTokenExpired() bool {
	if m.creds.ExpiryDate == 0 {
		return false
	}
	return time.Now().Add(tokenExpiryBuffer).UnixMilli() >= m.creds.ExpiryDate
}

func (m *oauthManager) refreshQwenToken(ctx context.Context) error {
	if m.creds.RefreshToken == "" {
		return fmt.Errorf("no refresh_token available, re-authenticate with qwen CLI")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {m.creds.RefreshToken},
		"client_id":     {qwenOAuthClientID},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, qwenOAuthTokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post token request: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		ResourceURL  string `json:"resource_url"`
		ExpiresIn    int64  `json:"expires_in"`
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

	m.creds.AccessToken = tokenResp.AccessToken
	m.creds.TokenType = tokenResp.TokenType
	m.creds.ResourceURL = tokenResp.ResourceURL
	m.creds.ExpiryDate = time.Now().UnixMilli() + tokenResp.ExpiresIn*1000
	if tokenResp.RefreshToken != "" {
		m.creds.RefreshToken = tokenResp.RefreshToken
	}

	if err := writeOAuthCreds(m.configDir, m.creds); err != nil {
		return fmt.Errorf("persist refreshed creds: %w", err)
	}

	return nil
}

// readOAuthCreds reads the full OAuth credentials from disk.
func readOAuthCreds(configDir string) (*oauthCreds, error) {
	path := filepath.Join(configDir, "oauth_creds.json")

	data, err := os.ReadFile(path)
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
