package providerauth

import (
	"context"
	"net/http"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/pkg/protocol/anthropic"
)

// SetHeaders injects provider authentication headers based on protocol type.
func SetHeaders(ctx context.Context, h http.Header, provCfg *config.ProviderConfig) {
	h.Set("Content-Type", "application/json")
	apiKey := provCfg.GetAPIKey(ctx)
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
}
