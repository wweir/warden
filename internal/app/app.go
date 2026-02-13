package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"warden/config"
	"warden/internal/gateway"
	"warden/internal/mcp"
)

type App struct {
	cfg        *config.ConfigStruct
	gateway    *gateway.Gateway
	mcpServers map[string]*mcp.Server
}

func NewApp(cfg *config.ConfigStruct) *App {
	return &App{
		cfg:        cfg,
		gateway:    gateway.NewGateway(cfg),
		mcpServers: make(map[string]*mcp.Server),
	}
}

func (a *App) Run() error {
	// 启动 MCP 服务器
	var wg sync.WaitGroup
	for name, mcpCfg := range a.cfg.MCP {
		wg.Add(1)
		go func(name string, cfg *config.MCPConfig) {
			defer wg.Done()
			s := mcp.NewServer(cfg)
			a.mcpServers[name] = s
			if err := s.Start(); err != nil {
				slog.Error("Failed to start MCP server", "name", name, "error", err)
				os.Exit(1)
			}
		}(name, mcpCfg)
	}

	wg.Wait() // 等待所有 MCP 服务器启动完成

	// 启动 HTTP 服务器
	server := &http.Server{
		Addr:    a.cfg.Addr,
		Handler: a.gateway,
	}

	// 启动服务器 goroutine
	go func() {
		slog.Info("Gateway is listening", "addr", a.cfg.Addr)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	}()

	// 优雅关闭
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

	// 停止 MCP 服务器
	for name, s := range a.mcpServers {
		slog.Info("Stopping MCP server", "name", name)
		if err := s.Stop(); err != nil {
			slog.Warn("Failed to stop MCP server", "name", name, "error", err)
		}
	}

	slog.Info("Warden shutdown complete")
	return nil
}
