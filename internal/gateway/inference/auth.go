package inference

import (
	"errors"
	"log/slog"

	"github.com/wweir/warden/config"
	sel "github.com/wweir/warden/internal/selector"
)

func TryAuthRetry(err error, provCfg *config.ProviderConfig, retried map[string]bool) bool {
	var upErr *sel.UpstreamError
	if !errors.As(err, &upErr) || !upErr.IsAuthError() {
		return false
	}
	if retried[provCfg.Name] {
		return false
	}
	provCfg.InvalidateAuth()
	retried[provCfg.Name] = true
	slog.Info("Auth error, reloading credentials", "provider", provCfg.Name)
	return true
}
