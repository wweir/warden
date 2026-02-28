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

	"github.com/wweir/warden/pkg/ssh"
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
	sshCfg    *ssh.Config
	ghToken   string        // GitHub OAuth token from hosts.json
	token     *copilotToken // cached Copilot API token
}

// copilotProvider implements TokenProvider for GitHub Copilot.
type copilotProvider struct {
	mu       sync.Mutex
	managers map[string]*tokenManager
}

func (p *copilotProvider) getManager(configDir string, sshCfg *ssh.Config) *tokenManager {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := configDir
	if sshCfg != nil {
		key = configDir + "@" + sshCfg.Host
	}

	if m, ok := p.managers[key]; ok {
		return m
	}
	m := &tokenManager{configDir: configDir, sshCfg: sshCfg}
	p.managers[key] = m
	return m
}

func (p *copilotProvider) GetAccessToken(configDir string, sshCfg *ssh.Config) (string, error) {
	return p.getManager(configDir, sshCfg).getAccessToken()
}

func (p *copilotProvider) InvalidateAuth(configDir string, sshCfg *ssh.Config) {
	m := p.getManager(configDir, sshCfg)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ghToken = ""
	m.token = nil
}

func (p *copilotProvider) CheckCredsReadable(configDir string, sshCfg *ssh.Config) error {
	_, err := readGitHubToken(configDir, sshCfg)
	return err
}

// getAccessToken returns a valid Copilot API token, refreshing if needed.
func (m *tokenManager) getAccessToken() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// load GitHub OAuth token from hosts.json on first call
	if m.ghToken == "" {
		token, err := readGitHubToken(m.configDir, m.sshCfg)
		if err != nil {
			return "", err
		}
		m.ghToken = token
	}

	// refresh if Copilot token is expired or not yet obtained
	if m.isCopilotTokenExpired() {
		if err := m.refreshCopilotToken(); err != nil {
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

func (m *tokenManager) refreshCopilotToken() error {
	req, err := http.NewRequest(http.MethodGet, copilotTokenEndpoint, nil)
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
func readGitHubToken(configDir string, sshCfg *ssh.Config) (string, error) {
	for _, filename := range []string{"hosts.json", "apps.json"} {
		path := filepath.Join(configDir, filename)

		var data []byte
		var err error
		if sshCfg != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			data, err = ssh.ReadFile(ctx, sshCfg, path)
			cancel()
		} else {
			data, err = os.ReadFile(path)
		}
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
