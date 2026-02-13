package mcp

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"warden/config"
)

// Server 管理 MCP 服务器进程
type Server struct {
	cfg *config.MCPConfig
	cmd *exec.Cmd
}

// NewServer 创建新的 MCP 服务器实例
func NewServer(cfg *config.MCPConfig) *Server {
	return &Server{
		cfg: cfg,
	}
}

// Start 启动 MCP 服务器
func (s *Server) Start() error {
	if s.cmd != nil {
		return fmt.Errorf("already started")
	}

	slog.Info("Starting MCP server", "name", s.cfg.Name)

	// 构建命令
	s.cmd = exec.Command(s.cfg.Command, s.cfg.Args...)

	// 设置环境变量
	cmdEnv := os.Environ()
	for k, v := range s.cfg.Env {
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
	}
	s.cmd.Env = cmdEnv

	// 设置输出
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	slog.Debug("MCP server started", "name", s.cfg.Name, "pid", s.cmd.Process.Pid)

	// 监听命令结束
	go func() {
		if err := s.cmd.Wait(); err != nil {
			slog.Error("MCP server failed", "name", s.cfg.Name, "error", err)
		} else {
			slog.Info("MCP server exited", "name", s.cfg.Name)
		}
		s.cmd = nil
	}()

	return nil
}

// Stop 停止 MCP 服务器
func (s *Server) Stop() error {
	if s.cmd == nil {
		return fmt.Errorf("not running")
	}

	slog.Info("Stopping MCP server", "name", s.cfg.Name)

	if err := s.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("kill process: %w", err)
	}

	if err := s.cmd.Wait(); err != nil {
		slog.Warn("MCP server failed to stop cleanly", "name", s.cfg.Name, "error", err)
	}

	s.cmd = nil
	return nil
}

// IsRunning 检查服务器是否正在运行
func (s *Server) IsRunning() bool {
	if s.cmd == nil {
		return false
	}

	err := s.cmd.Process.Signal(nil) // 发送 0 信号检查是否存活
	return err == nil
}
