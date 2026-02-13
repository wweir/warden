package config

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// Load 加载并验证配置
func Load(configPath string) (*ConfigStruct, error) {
	cfg := &ConfigStruct{
		Addr:    ":8080",
		BaseURL: make(map[string]*BaseURLConfig),
		Route:   make(map[string]*RouteConfig),
		MCP:     make(map[string]*MCPConfig),
	}

	// 读取配置文件
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	// 解析 TOML 配置
	if _, err := toml.Decode(string(content), cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// 验证配置
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

type ConfigStruct struct {
	Addr    string                    `json:"addr" usage:"Gateway listening address"`
	BaseURL map[string]*BaseURLConfig `json:"baseurl" usage:"Upstream LLM base URL configurations"`
	Route   map[string]*RouteConfig   `json:"route" usage:"Route prefix configurations"`
	MCP     map[string]*MCPConfig     `json:"mcp" usage:"MCP server configurations"`
}

type BaseURLConfig struct {
	Name         string `json:"-"` // populated from map key
	URL          string `json:"url" usage:"Upstream LLM base URL"`
	Protocol     string `json:"protocol" usage:"API protocol: openai, anthropic, ollama"`
	APIKey       string `json:"api_key" usage:"API key for authentication"`
	Timeout      string `json:"timeout" usage:"Request timeout (e.g. 60s, 2m)"`
	DefaultModel string `json:"default_model" usage:"Default model override"`

	TimeoutDuration time.Duration // parsed from Timeout
}

type RouteConfig struct {
	Prefix   string   `json:"-"` // populated from map key
	BaseURLs []string `json:"baseurls" usage:"BaseURL names to use (order matters for fallback)"`
	Tools    []string `json:"tools" usage:"MCP tool names to inject"`

	EnabledTools map[string]bool // runtime state, 允许动态开关
}

type MCPConfig struct {
	Name    string            `json:"-"` // populated from map key
	Command string            `json:"command" usage:"MCP server command"`
	Args    []string          `json:"args" usage:"MCP server arguments"`
	Env     map[string]string `json:"env" usage:"Environment variables"`
}

// Validate 验证配置的合法性
func (c *ConfigStruct) Validate() error {
	// 验证路由前缀格式
	for prefix, route := range c.Route {
		if prefix == "" || prefix[0] != '/' {
			return NewValidationError("route prefix must start with /: %s", prefix)
		}
		route.Prefix = prefix
	}

	// 验证 baseurl 配置
	for name, buc := range c.BaseURL {
		buc.Name = name
		if buc.URL == "" {
			return NewValidationError("baseurl %s: url is required", name)
		}

		// 验证协议类型
		switch buc.Protocol {
		case "openai", "anthropic", "ollama":
		default:
			return NewValidationError("baseurl %s: invalid protocol %s (must be openai/anthropic/ollama)", name, buc.Protocol)
		}

		// 解析超时
		if buc.Timeout != "" {
			dur, err := time.ParseDuration(buc.Timeout)
			if err != nil {
				return NewValidationError("baseurl %s: invalid timeout %s: %v", name, buc.Timeout, err)
			}
			buc.TimeoutDuration = dur
		} else {
			buc.TimeoutDuration = 60 * time.Second // 默认 60 秒
		}
	}

	// 验证 route 中引用的 baseurl 和工具是否存在
	for prefix, route := range c.Route {
		// 验证 baseurl 引用
		for _, buName := range route.BaseURLs {
			if _, exists := c.BaseURL[buName]; !exists {
				return NewValidationError("route %s: unknown baseurl %s", prefix, buName)
			}
		}

		// 验证 tool 引用
		for _, toolName := range route.Tools {
			if _, exists := c.MCP[toolName]; !exists {
				return NewValidationError("route %s: unknown MCP tool %s", prefix, toolName)
			}
		}

		// 初始化 enabled tools
		route.EnabledTools = make(map[string]bool)
		for _, toolName := range route.Tools {
			route.EnabledTools[toolName] = true
		}
	}

	// 验证 mcp 配置
	for name, mcp := range c.MCP {
		mcp.Name = name
		if mcp.Command == "" {
			return NewValidationError("mcp %s: command is required", name)
		}
	}

	return nil
}

// ValidationError 配置验证错误
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

func NewValidationError(format string, args ...interface{}) error {
	return &ValidationError{
		Message: sprintf(format, args...),
	}
}

func sprintf(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}
