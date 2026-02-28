package provider

import (
	"time"

	"github.com/wweir/warden/pkg/ssh"
)

const (
	// tokenExpiryBuffer is the duration before expiry to trigger a refresh.
	tokenExpiryBuffer = 30 * time.Second
)

// TokenProvider manages OAuth token lifecycle for a specific protocol.
type TokenProvider interface {
	GetAccessToken(configDir string, sshCfg *ssh.Config) (string, error)
	InvalidateAuth(configDir string, sshCfg *ssh.Config)
	CheckCredsReadable(configDir string, sshCfg *ssh.Config) error
}

var providers = map[string]TokenProvider{
	"qwen":    &qwenProvider{managers: make(map[string]*oauthManager)},
	"copilot": &copilotProvider{managers: make(map[string]*tokenManager)},
}

// Get returns the TokenProvider for the given protocol name.
// Returns nil if the protocol has no token provider.
func Get(protocol string) TokenProvider {
	return providers[protocol]
}
