package cliproxybridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	sdkcliproxy "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	"github.com/wweir/warden/config"
	"gopkg.in/yaml.v3"
)

const (
	startTimeout        = 10 * time.Second
	healthProbeInterval = 100 * time.Millisecond
	healthProbeTimeout  = 300 * time.Millisecond

	defaultCodexUserAgent             = "codex-tui/0.118.0 (Mac OS 26.3.1; arm64) iTerm.app/3.6.9 (codex-tui; 0.118.0)"
	defaultCodexBetaFeatures          = "multi_agent"
	defaultClaudeUserAgent            = "claude-cli/2.1.63 (external, cli)"
	defaultClaudePackageVersion       = "0.74.0"
	defaultClaudeRuntimeVersion       = "v24.3.0"
	defaultClaudeOS                   = "MacOS"
	defaultClaudeArch                 = "arm64"
	defaultClaudeTimeout              = "600"
	defaultClaudeStabilizeDeviceState = true
)

type Bridge struct {
	service    *sdkcliproxy.Service
	baseURL    string
	configPath string
	cancel     context.CancelFunc
	errCh      chan error
}

func New(cfg *config.ConfigStruct) (*Bridge, error) {
	if !ShouldStart(cfg) {
		return nil, nil
	}

	host, port, baseURL, err := embeddedEndpoint(cfg)
	if err != nil {
		return nil, err
	}

	authDir := strings.TrimSpace(cfg.CLIProxy.AuthDir)
	if authDir == "" {
		authDir = config.DefaultCLIProxyAuthDir
	}
	if strings.HasPrefix(authDir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve cliproxy auth_dir: %w", err)
		}
		authDir = filepath.Join(home, authDir[2:])
	}

	clipCfg := buildRuntimeConfig(cfg, host, port, authDir)

	configPath, err := writeRuntimeConfig(clipCfg)
	if err != nil {
		return nil, err
	}

	service, err := sdkcliproxy.NewBuilder().
		WithConfig(clipCfg).
		WithConfigPath(configPath).
		Build()
	if err != nil {
		_ = os.Remove(configPath)
		return nil, fmt.Errorf("build cliproxy service: %w", err)
	}

	return &Bridge{
		service:    service,
		baseURL:    baseURL,
		configPath: configPath,
		errCh:      make(chan error, 1),
	}, nil
}

func buildRuntimeConfig(cfg *config.ConfigStruct, host string, port int, authDir string) *sdkconfig.Config {
	clipCfg := &sdkconfig.Config{
		Host:                   host,
		Port:                   port,
		AuthDir:                authDir,
		LoggingToFile:          false,
		UsageStatisticsEnabled: false,
		RequestRetry:           cfg.CLIProxy.RequestRetry,
		MaxRetryCredentials:    cfg.CLIProxy.MaxRetryCredentials,
	}
	clipCfg.Home.Enabled = false
	clipCfg.SDKConfig.ProxyURL = effectiveCLIProxyProxy(cfg)
	clipCfg.SDKConfig.APIKeys = cliproxyAPIKeys(cfg)
	clipCfg.Pprof.Enable = false
	clipCfg.RemoteManagement.AllowRemote = false
	clipCfg.RemoteManagement.DisableControlPanel = true
	applyFeatureHidingDefaults(clipCfg)
	return clipCfg
}

func effectiveCLIProxyProxy(cfg *config.ConfigStruct) string {
	if cfg == nil || cfg.CLIProxy == nil {
		return ""
	}
	if proxy := strings.TrimSpace(cfg.CLIProxy.Proxy); proxy != "" {
		return proxy
	}
	names := make([]string, 0, len(cfg.Provider))
	for name := range cfg.Provider {
		names = append(names, name)
	}
	slices.Sort(names)
	var firstProxy string
	for _, name := range names {
		prov := cfg.Provider[name]
		if prov == nil || prov.Backend != config.ProviderBackendCLIProxy {
			continue
		}
		if proxy := strings.TrimSpace(prov.Proxy); proxy != "" {
			if firstProxy == "" {
				firstProxy = proxy
				continue
			}
			if proxy != firstProxy {
				slog.Warn("Multiple embedded cliproxy providers with different proxies, using first one",
					"first", firstProxy, "current", proxy)
			}
		}
	}
	return firstProxy
}

func applyFeatureHidingDefaults(cfg *sdkconfig.Config) {
	if cfg == nil {
		return
	}
	cfg.CodexHeaderDefaults.UserAgent = defaultCodexUserAgent
	cfg.CodexHeaderDefaults.BetaFeatures = defaultCodexBetaFeatures
	cfg.SDKConfig.PassthroughHeaders = false
	cfg.ClaudeHeaderDefaults.UserAgent = defaultClaudeUserAgent
	cfg.ClaudeHeaderDefaults.PackageVersion = defaultClaudePackageVersion
	cfg.ClaudeHeaderDefaults.RuntimeVersion = defaultClaudeRuntimeVersion
	cfg.ClaudeHeaderDefaults.OS = defaultClaudeOS
	cfg.ClaudeHeaderDefaults.Arch = defaultClaudeArch
	cfg.ClaudeHeaderDefaults.Timeout = defaultClaudeTimeout
	stabilize := defaultClaudeStabilizeDeviceState
	cfg.ClaudeHeaderDefaults.StabilizeDeviceProfile = &stabilize
}

func ShouldStart(cfg *config.ConfigStruct) bool {
	if cfg == nil || cfg.CLIProxy == nil || !cfg.CLIProxy.Enabled {
		return false
	}
	for _, prov := range cfg.Provider {
		if prov != nil && prov.Backend == config.ProviderBackendCLIProxy {
			return true
		}
	}
	return false
}

func (b *Bridge) Start(ctx context.Context) error {
	if b == nil {
		return nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel

	go func() {
		err := b.service.Run(runCtx)
		b.errCh <- err
	}()

	waitCtx, waitCancel := context.WithTimeout(ctx, startTimeout)
	defer waitCancel()
	if err := b.waitHealthy(waitCtx); err != nil {
		cancel()
		return err
	}

	slog.Info("Embedded cliproxy service is listening", "url", b.baseURL)
	return nil
}

func (b *Bridge) Close(ctx context.Context) error {
	if b == nil {
		return nil
	}
	if b.cancel != nil {
		b.cancel()
	}
	var shutdownErr error
	if err := b.service.Shutdown(ctx); err != nil && !errors.Is(err, context.Canceled) {
		shutdownErr = err
	}
	if b.configPath != "" {
		if err := os.Remove(b.configPath); err != nil && !errors.Is(err, os.ErrNotExist) && shutdownErr == nil {
			shutdownErr = fmt.Errorf("remove cliproxy runtime config: %w", err)
		}
	}
	return shutdownErr
}

func (b *Bridge) Err() <-chan error {
	if b == nil {
		return nil
	}
	return b.errCh
}

func (b *Bridge) waitHealthy(ctx context.Context) error {
	ticker := time.NewTicker(healthProbeInterval)
	defer ticker.Stop()

	for {
		if err := b.probeHealth(ctx); err == nil {
			return nil
		}

		select {
		case err := <-b.errCh:
			if err == nil {
				return fmt.Errorf("cliproxy service stopped before becoming healthy")
			}
			return fmt.Errorf("cliproxy service failed before becoming healthy: %w", err)
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("wait for cliproxy health: %w", ctx.Err())
		}
	}
}

func (b *Bridge) probeHealth(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, b.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}

func embeddedEndpoint(cfg *config.ConfigStruct) (string, int, string, error) {
	for name, prov := range cfg.Provider {
		if prov == nil || prov.Backend != config.ProviderBackendCLIProxy {
			continue
		}
		parsed, err := url.Parse(prov.URL)
		if err != nil {
			return "", 0, "", fmt.Errorf("provider %s: parse cliproxy url: %w", name, err)
		}
		port, err := strconv.Atoi(parsed.Port())
		if err != nil || port <= 0 {
			return "", 0, "", fmt.Errorf("provider %s: invalid cliproxy port %q", name, parsed.Port())
		}
		return parsed.Hostname(), port, parsed.Scheme + "://" + parsed.Host, nil
	}
	return "", 0, "", fmt.Errorf("cliproxy is enabled but no cliproxy-backed provider exists")
}

func cliproxyAPIKeys(cfg *config.ConfigStruct) []string {
	seen := map[string]bool{}
	var keys []string
	for _, prov := range cfg.Provider {
		if prov == nil || prov.Backend != config.ProviderBackendCLIProxy {
			continue
		}
		key := strings.TrimSpace(prov.APIKey.Value())
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func writeRuntimeConfig(cfg *sdkconfig.Config) (string, error) {
	file, err := os.CreateTemp("", "warden-cliproxy-*.yaml")
	if err != nil {
		return "", fmt.Errorf("create cliproxy runtime config: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close cliproxy runtime config: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("marshal cliproxy runtime config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("write cliproxy runtime config: %w", err)
	}
	return path, nil
}
