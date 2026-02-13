package main

import (
	"log/slog"
	"os"

	"github.com/lmittmann/tint"

	"warden/config"
	"warden/internal/app"
)

// Version 应用程序版本信息
var Version = "development"

// BuildTime 构建时间
var BuildTime = "unknown"

// GitHash git 提交哈希
var GitHash = "unknown"

// GitBranch git 分支
var GitBranch = "unknown"

func main() {
	// 配置日志输出
	logger := slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level: slog.LevelDebug,
	}))

	slog.SetDefault(logger)

	logger.Info("Starting Warden AI Gateway",
		"version", Version,
		"build_time", BuildTime,
		"git_hash", GitHash,
		"git_branch", GitBranch)

	// 加载配置
	cfg, err := config.Load("warden.toml")
	if err != nil {
		logger.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	logger.Info("Config loaded successfully", "addr", cfg.Addr)

	// 启动应用程序
	application := app.NewApp(cfg)
	if err := application.Run(); err != nil {
		logger.Error("Application error", "error", err)
		os.Exit(1)
	}
}
