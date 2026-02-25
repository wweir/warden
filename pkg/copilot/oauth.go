package copilot

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
	tokenEndpoint = "https://api.github.com/copilot_internal/v2/token"
	// refresh token 30s before expiry
	tokenExpiryBuffer = 30 * time.Second
)

// copilotToken holds a short-lived Copilot API token.
type copilotToken struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"` // unix timestamp in seconds
}

// hostsFile represents the structure of ~/.config/github-copilot/hosts.json.
type hostsFile map[string]struct {
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

var (
	managersMu sync.Mutex
	managers   = make(map[string]*tokenManager)
)

// getTokenManager returns a singleton tokenManager per configDir+sshHost.
func getTokenManager(configDir string, sshCfg *ssh.Config) *tokenManager {
	managersMu.Lock()
	defer managersMu.Unlock()

	key := configDir
	if sshCfg != nil {
		key = configDir + "@" + sshCfg.Host
	}

	if m, ok := managers[key]; ok {
		return m
	}
	m := &tokenManager{configDir: configDir, sshCfg: sshCfg}
	managers[key] = m
	return m
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
	if m.isExpired() {
		if err := m.refresh(); err != nil {
			return "", fmt.Errorf("refresh copilot token: %w", err)
		}
	}

	return m.token.Token, nil
}

func (m *tokenManager) isExpired() bool {
	if m.token == nil {
		return true
	}
	return time.Now().Add(tokenExpiryBuffer).Unix() >= m.token.ExpiresAt
}

func (m *tokenManager) refresh() error {
	req, err := http.NewRequest(http.MethodGet, tokenEndpoint, nil)
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

// GetAccessToken returns a valid Copilot API token for the given config directory,
// refreshing automatically if the token is expired or about to expire.
// If sshCfg is non-nil, the GitHub OAuth token is read from the remote host via SSH.
func GetAccessToken(configDir string, sshCfg *ssh.Config) (string, error) {
	return getTokenManager(configDir, sshCfg).getAccessToken()
}

// readGitHubToken reads the GitHub OAuth token from hosts.json or apps.json.
// If sshCfg is non-nil, files are read from the remote host via SSH.
func readGitHubToken(configDir string, sshCfg *ssh.Config) (string, error) {
	// try hosts.json first, then apps.json
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

		var hosts hostsFile
		if err := json.Unmarshal(data, &hosts); err != nil {
			continue
		}

		// prefer github.com entry
		if entry, ok := hosts["github.com"]; ok && entry.OAuthToken != "" {
			return entry.OAuthToken, nil
		}
		// fall back to first entry with a token
		for _, entry := range hosts {
			if entry.OAuthToken != "" {
				return entry.OAuthToken, nil
			}
		}
	}

	return "", fmt.Errorf("no GitHub OAuth token found in %s (hosts.json or apps.json)", configDir)
}
