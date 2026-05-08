package providerauth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/pkg/protocol/anthropic"
)

const ClientAuthFailureBody = "provider authentication failed"

// SetHeaders injects provider authentication headers based on protocol type.
func SetHeaders(ctx context.Context, h http.Header, provCfg *config.ProviderConfig) error {
	h.Set("Content-Type", "application/json")
	apiKey, err := provCfg.ResolveAPIKey(ctx)
	if err != nil {
		return fmt.Errorf("resolve provider api key for %s: %w", provCfg.Name, err)
	}
	if apiKey != "" {
		switch provCfg.Protocol {
		case "anthropic":
			anthropic.SetAuthHeaders(h, apiKey)
		default:
			h.Set("Authorization", "Bearer "+apiKey)
		}
	}
	for k, v := range provCfg.Headers {
		h.Set(k, v)
	}
	return nil
}
