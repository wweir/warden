package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sower-proxy/deferlog/v2"
	"github.com/sower-proxy/feconf"
	_ "github.com/sower-proxy/feconf/decoder/yaml"
	_ "github.com/sower-proxy/feconf/reader/file"
	_ "github.com/sower-proxy/feconf/reader/http"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/gateway"
)

type App struct {
	cfg        *config.ConfigStruct
	configPath string

	mu      sync.RWMutex
	gateway *gateway.Gateway
}

func NewApp(cfg *config.ConfigStruct, configPath string) *App {
	configHash := ""
	if configPath != "" {
		if data, err := os.ReadFile(configPath); err == nil {
			configHash = fmt.Sprintf("%x", sha256.Sum256(data))
		}
	}

	gw := gateway.NewGateway(cfg, configPath, configHash)

	return &App{
		cfg:        cfg,
		configPath: configPath,
		gateway:    gw,
	}
}

// ServeHTTP delegates to the current gateway (hot-reload safe).
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	gw := a.gateway
	a.mu.RUnlock()
	gw.ServeHTTP(w, r)
}

// Reload re-reads the config file and swaps the gateway atomically.
// Returns error if the new config is invalid or addr changed (requires restart).
func (a *App) Reload() (err error) {
	defer func() { deferlog.DebugError(err, "reload gateway") }()

	if a.configPath == "" {
		return fmt.Errorf("no config file path configured")
	}

	cfg, err := feconf.New[config.ConfigStruct]("c", a.configPath).Parse()
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	if cfg.Addr != a.cfg.Addr {
		return fmt.Errorf("addr changed from %s to %s, restart required", a.cfg.Addr, cfg.Addr)
	}

	data, err := os.ReadFile(a.configPath)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}
	configHash := fmt.Sprintf("%x", sha256.Sum256(data))

	newGW := gateway.NewGateway(cfg, a.configPath, configHash)
	newGW.SetReloadFn(a.Reload)

	a.mu.Lock()
	oldGW := a.gateway
	a.gateway = newGW
	a.cfg = cfg
	a.mu.Unlock()

	go oldGW.Close()

	slog.Info("Gateway reloaded successfully")
	return nil
}

func (a *App) Run() (err error) {
	defer func() { deferlog.DebugError(err, "app run") }()

	a.gateway.SetReloadFn(a.Reload)

	server := &http.Server{
		Addr:    a.cfg.Addr,
		Handler: a, // App itself is the handler
	}

	go func() {
		slog.Info("Gateway is listening", "addr", a.cfg.Addr)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	}()

	// graceful shutdown
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	<-stopChan
	slog.Info("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server shutdown failed", "error", err)
		os.Exit(1)
	}

	// close MCP clients managed by Gateway
	a.mu.RLock()
	gw := a.gateway
	a.mu.RUnlock()
	gw.Close()

	slog.Info("Warden shutdown complete")
	return nil
}
