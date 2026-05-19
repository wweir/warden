package providerauth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/pkg/protocol/anthropic"
)

const ClientAuthFailureBody = "provider authentication failed"

// SetHeaders injects provider authentication headers based on the resolved target URL.
// targetURL is used to select the matching endpoint so multi-endpoint providers do not
// mix authentication schemes or endpoint-specific headers.
func SetHeaders(ctx context.Context, h http.Header, provCfg *config.ProviderConfig, targetURL string) error {
	h.Set("Content-Type", "application/json")
	apiKey, err := provCfg.ResolveAPIKey(ctx)
	if err != nil {
		return fmt.Errorf("resolve provider api key for %s: %w", provCfg.Name, err)
	}

	format := config.ProviderFormatForURL(provCfg, targetURL)
	if format == "" {
		format = provCfg.Format
	}
	headers := config.ProviderHeadersForURL(provCfg, targetURL)

	if apiKey != "" {
		if format == config.ProviderFormatAnthropic {
			anthropic.SetAuthHeaders(h, apiKey)
		} else {
			h.Set("Authorization", "Bearer "+apiKey)
		}
	}

	// Merge headers selected for the resolved target endpoint.
	for k, v := range headers {
		h.Set(k, v)
	}
	return nil
}
