package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/pkg/ssh"
)

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// Validate checks required fields of Tool.
func (t *Tool) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	if t.Description == "" {
		return fmt.Errorf("tool description is required")
	}
	if t.InputSchema == nil {
		return fmt.Errorf("tool input schema is required")
	}
	return nil
}

// Client manages MCP server connections and tool invocations.
type Client struct {
	name   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	tools  []Tool

	mu    sync.Mutex // protects stdin/stdout access
	reqID atomic.Int64
}

// Validate checks required fields of Client.
func (c *Client) Validate() error {
	if c.name == "" {
		return fmt.Errorf("client name is required")
	}
	return nil
}

// NewClient creates a new MCP client.
func NewClient(cfg *config.MCPConfig) (*Client, error) {
	return &Client{
		name: cfg.Name,
	}, nil
}

// Start launches the MCP server subprocess and completes the initialize handshake.
func (c *Client) Start(ctx context.Context, cfg *config.MCPConfig) error {
	var err error
	defer func() { deferlog.DebugError(err, "MCP client start", "name", c.name) }()

	// build command
	var cmd *exec.Cmd
	if cfg.SSHCfg != nil {
		// SSH remote execution: env vars are passed as prefix
		cmd = ssh.CommandWithEnv(cfg.SSHCfg, cfg.Env, cfg.Command, cfg.Args...)
	} else {
		cmd = exec.Command(cfg.Command, cfg.Args...)
		// set environment variables for local execution
		cmdEnv := os.Environ()
		for k, v := range cfg.Env {
			cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = cmdEnv
	}
	cmd.Stderr = os.Stderr

	// get stdin/stdout pipes before Start()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewScanner(stdout)
	// increase buffer for large JSON responses (1MB)
	c.stdout.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	slog.Debug("Starting MCP client", "name", c.name)

	if err = cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	// complete MCP handshake
	if err = c.initialize(ctx); err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("initialize: %w", err)
	}

	// fetch tool list
	c.tools, err = c.ListTools(ctx)
	if err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("list tools: %w", err)
	}

	slog.Debug("MCP client ready", "name", c.name, "tools", len(c.tools))
	return nil
}

// initialize completes the MCP handshake: initialize request → initialized notification.
func (c *Client) initialize(ctx context.Context) error {
	var err error
	defer func() { deferlog.DebugError(err, "MCP client initialize", "name", c.name) }()

	// send initialize request
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "warden",
				"version": "0.1.0",
			},
		},
	}

	initResp, err := c.sendRequest(ctx, initReq)
	if err != nil {
		return fmt.Errorf("send initialize: %w", err)
	}

	slog.Debug("MCP initialize response", "name", c.name, "response", string(initResp))

	// send initialized notification (no id, no response expected)
	notification := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	if err = c.sendNotification(notification); err != nil {
		return fmt.Errorf("send initialized notification: %w", err)
	}

	return nil
}

// CachedTools returns the cached tool list from the initial ListTools call during Start.
func (c *Client) CachedTools() []Tool {
	return c.tools
}

// ListTools fetches the available tool list from the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "tools/list",
	}

	resp, err := c.sendRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("send tools/list: %w", err)
	}

	var toolsResp struct {
		Result struct {
			Tools []Tool `json:"tools"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp, &toolsResp); err != nil {
		return nil, fmt.Errorf("unmarshal tools response: %w", err)
	}

	return toolsResp.Result.Tools, nil
}

// CallTool invokes a specific tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}

	resp, err := c.sendRequest(ctx, req)
	if err != nil {
		return "", fmt.Errorf("send tools/call %s: %w", name, err)
	}

	var callResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError,omitempty"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.Unmarshal(resp, &callResp); err != nil {
		return "", fmt.Errorf("unmarshal call response: %w", err)
	}

	if callResp.Error != nil {
		return "", fmt.Errorf("MCP error %d: %s", callResp.Error.Code, callResp.Error.Message)
	}

	// extract text from content blocks
	var texts []string
	for _, block := range callResp.Result.Content {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}

	result := strings.Join(texts, "\n")
	if callResp.Result.IsError {
		return result, fmt.Errorf("tool error: %s", result)
	}

	return result, nil
}

// sendRequest sends a JSON-RPC request and waits for the response (thread-safe).
func (c *Client) sendRequest(ctx context.Context, req map[string]any) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// write request + newline
	if _, err := c.stdin.Write(append(reqData, '\n')); err != nil {
		return nil, fmt.Errorf("write to stdin: %w", err)
	}

	// read response line
	if !c.stdout.Scan() {
		if err := c.stdout.Err(); err != nil {
			return nil, fmt.Errorf("read from stdout: %w", err)
		}
		return nil, fmt.Errorf("read from stdout: unexpected EOF")
	}

	return []byte(c.stdout.Text()), nil
}

// sendNotification sends a JSON-RPC notification (no response expected, thread-safe).
func (c *Client) sendNotification(req map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	if _, err := c.stdin.Write(append(reqData, '\n')); err != nil {
		return fmt.Errorf("write notification to stdin: %w", err)
	}

	return nil
}

// nextID returns a monotonically increasing request ID.
func (c *Client) nextID() int64 {
	return c.reqID.Add(1)
}

// Close terminates the MCP server subprocess.
func (c *Client) Close() error {
	if c.cmd == nil {
		return nil
	}

	// close stdin to signal EOF to the child process
	if c.stdin != nil {
		c.stdin.Close()
	}

	if err := c.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("kill process: %w", err)
	}

	if err := c.cmd.Wait(); err != nil {
		slog.Warn("MCP server failed to stop cleanly", "name", c.name, "error", err)
	}

	c.cmd = nil
	return nil
}

// IsRunning checks if the MCP server process is still running.
func (c *Client) IsRunning() bool {
	if c.cmd == nil {
		return false
	}

	err := c.cmd.Process.Signal(syscall.Signal(0))
	return err == nil
}
