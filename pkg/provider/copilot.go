package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	copilotTokenEndpoint = "https://api.github.com/copilot_internal/v2/token"
)

// copilotToken holds a short-lived Copilot API token.
type copilotToken struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"` // unix timestamp in seconds
}

// copilotHostsFile represents the structure of ~/.config/github-copilot/hosts.json.
type copilotHostsFile map[string]struct {
	OAuthToken string `json:"oauth_token"`
}

// tokenManager handles Copilot token lifecycle for a single config directory.
type tokenManager struct {
	mu        sync.Mutex
	configDir string
	ghToken   string        // GitHub OAuth token from hosts.json
	token     *copilotToken // cached Copilot API token
}

// copilotProvider implements TokenProvider for GitHub Copilot.
type copilotProvider struct {
	mu       sync.Mutex
	managers map[string]*tokenManager
}

func (p *copilotProvider) getManager(configDir string) *tokenManager {
	p.mu.Lock()
	defer p.mu.Unlock()

	if m, ok := p.managers[configDir]; ok {
		return m
	}
	m := &tokenManager{configDir: configDir}
	p.managers[configDir] = m
	return m
}

func (p *copilotProvider) GetAccessToken(ctx context.Context, configDir string) (string, error) {
	return p.getManager(configDir).getAccessToken(ctx)
}

func (p *copilotProvider) InvalidateAuth(configDir string) {
	m := p.getManager(configDir)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ghToken = ""
	m.token = nil
}

func (p *copilotProvider) CheckCredsReadable(_ context.Context, configDir string) error {
	_, err := readGitHubToken(configDir)
	return err
}

// getAccessToken returns a valid Copilot API token, refreshing if needed.
func (m *tokenManager) getAccessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// load GitHub OAuth token from hosts.json on first call
	if m.ghToken == "" {
		token, err := readGitHubToken(m.configDir)
		if err != nil {
			return "", err
		}
		m.ghToken = token
	}

	// refresh if Copilot token is expired or not yet obtained
	if m.isCopilotTokenExpired() {
		if err := m.refreshCopilotToken(ctx); err != nil {
			return "", fmt.Errorf("refresh copilot token: %w", err)
		}
	}

	return m.token.Token, nil
}

func (m *tokenManager) isCopilotTokenExpired() bool {
	if m.token == nil {
		return true
	}
	return time.Now().Add(tokenExpiryBuffer).Unix() >= m.token.ExpiresAt
}

func (m *tokenManager) refreshCopilotToken(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotTokenEndpoint, nil)
	if err != nil {
		return fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Authorization", "token "+m.ghToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch copilot token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
	}

	var token copilotToken
	if err := json.Unmarshal(body, &token); err != nil {
		return fmt.Errorf("decode token response: %w", err)
	}
	if token.Token == "" {
		return fmt.Errorf("token endpoint returned empty token")
	}

	m.token = &token
	return nil
}

// readGitHubToken reads the GitHub OAuth token from hosts.json or apps.json.
func readGitHubToken(configDir string) (string, error) {
	for _, filename := range []string{"hosts.json", "apps.json"} {
		path := filepath.Join(configDir, filename)

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var hosts copilotHostsFile
		if err := json.Unmarshal(data, &hosts); err != nil {
			continue
		}

		if entry, ok := hosts["github.com"]; ok && entry.OAuthToken != "" {
			return entry.OAuthToken, nil
		}
		for _, entry := range hosts {
			if entry.OAuthToken != "" {
				return entry.OAuthToken, nil
			}
		}
	}

	return "", fmt.Errorf("no GitHub OAuth token found in %s (hosts.json or apps.json)", configDir)
}
