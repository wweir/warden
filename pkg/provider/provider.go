package provider

import (
	"context"
	"time"
)

const (
	// tokenExpiryBuffer is the duration before expiry to trigger a refresh.
	tokenExpiryBuffer = 30 * time.Second
)

// TokenProvider manages OAuth token lifecycle for a specific protocol.
type TokenProvider interface {
	GetAccessToken(ctx context.Context, configDir string) (string, error)
	InvalidateAuth(configDir string)
	CheckCredsReadable(ctx context.Context, configDir string) error
}

var providers = map[string]TokenProvider{
	"copilot": &copilotProvider{managers: make(map[string]*tokenManager)},
}

// Get returns the TokenProvider for the given protocol name.
// Returns nil if the protocol has no token provider.
func Get(protocol string) TokenProvider {
	return providers[protocol]
}
