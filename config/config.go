package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/pkg/copilot"
	"github.com/wweir/warden/pkg/qwen"
	"github.com/wweir/warden/pkg/ssh"
)

//go:embed warden.example.yaml
var ExampleConfig string

// SSHConfig holds SSH connection parameters for remote access.
type SSHConfig = ssh.Config

type ConfigStruct struct {
	Addr          string                     `json:"addr" usage:"Gateway listening address"`
	AdminPassword string                     `json:"admin_password" usage:"Admin panel password (empty to disable)"`
	Log           *LogConfig                 `json:"log" usage:"Request/response logging configuration"`
	Provider      map[string]*ProviderConfig `json:"provider" usage:"Upstream LLM provider configurations"`
	Route         map[string]*RouteConfig    `json:"route" usage:"Route prefix configurations"`
	MCP           map[string]*MCPConfig      `json:"mcp" usage:"MCP server configurations"`
	SSH           map[string]*SSHConfig      `json:"ssh" usage:"SSH connection configurations"`
}

type LogConfig struct {
	FileDir string `json:"file_dir" usage:"Directory for request/response JSON log files"`
}

type ProviderConfig struct {
	Name         string            `json:"-"` // populated from map key
	URL          string            `json:"url" usage:"Upstream LLM base URL"`
	Protocol     string            `json:"protocol" usage:"API protocol: openai, anthropic, ollama, qwen, copilot"`
	APIKey       deferlog.Secret   `json:"api_key" usage:"API key for authentication"`
	ConfigDir    string            `json:"config_dir" usage:"Local CLI config directory for OAuth credentials (required for qwen/copilot)"`
	SSH          string            `json:"ssh" usage:"SSH config name for remote credential access"`
	Timeout      string            `json:"timeout" usage:"Request timeout (e.g. 60s, 2m)"`
	Proxy        string            `json:"proxy" usage:"HTTP/SOCKS proxy URL (e.g. http://host:port, socks5://host:port)"`
	Headers      map[string]string `json:"headers" usage:"Custom HTTP headers to send with upstream requests (overrides defaults)"`
	Models       []string          `json:"models" usage:"Supported model IDs (skips /models discovery when set)"`
	ModelAliases map[string]string `json:"model_aliases" usage:"Model alias mapping (alias_name = real_name), alias appears in /models and is resolved before upstream request"`
	RequestPatch []RequestPatchOp  `json:"request_patch" usage:"JSON Patch operations (RFC 6902) applied to request body before forwarding"`

	TimeoutDuration time.Duration // parsed from Timeout
	ProxyURL        *url.URL      // parsed from Proxy
	SSHCfg          *SSHConfig    // resolved from SSH during Validate
}

// RequestPatchOp defines a single JSON Patch operation (RFC 6902).
// Supported ops: "add", "remove", "replace", "move", "copy", "test".
type RequestPatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	From  string `json:"from,omitempty"`
	Value any    `json:"value,omitempty"`
}

// ApplyRequestPatch applies the configured JSON Patch operations to rawBody.
// Returns rawBody unchanged if RequestPatch is empty or body is not valid JSON.
// Each operation is applied independently; failures on individual ops are skipped.
// Remove operations targeting missing paths are silently skipped.
func (b *ProviderConfig) ApplyRequestPatch(rawBody []byte) []byte {
	if len(b.RequestPatch) == 0 {
		return rawBody
	}

	doc := rawBody
	for _, op := range b.RequestPatch {
		opJSON, err := json.Marshal([]RequestPatchOp{op})
		if err != nil {
			continue
		}
		patch, err := jsonpatch.DecodePatch(opJSON)
		if err != nil {
			continue
		}
		result, err := patch.ApplyWithOptions(doc, &jsonpatch.ApplyOptions{
			AllowMissingPathOnRemove: true,
		})
		if err != nil {
			continue
		}
		doc = result
	}
	return doc
}

// GetAPIKey returns the effective API key for authentication.
// For qwen protocol, reads from local OAuth credentials file if api_key is not set.
func (b *ProviderConfig) GetAPIKey() string {
	if b.APIKey.Value() != "" {
		return b.APIKey.Value()
	}
	switch b.Protocol {
	case "qwen":
		token, _ := qwen.GetAccessToken(b.ConfigDir, b.SSHCfg)
		return token
	case "copilot":
		token, _ := copilot.GetAccessToken(b.ConfigDir, b.SSHCfg)
		return token
	}
	return ""
}

// HTTPClient returns an *http.Client configured with proxy (if set) and the given timeout.
func (b *ProviderConfig) HTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if b.ProxyURL != nil {
		transport.Proxy = http.ProxyURL(b.ProxyURL)
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

// ResolveModel returns the real model name if the given model is an alias,
// otherwise returns the model unchanged.
func (b *ProviderConfig) ResolveModel(model string) string {
	if real, ok := b.ModelAliases[model]; ok {
		return real
	}
	return model
}

type RouteConfig struct {
	Prefix        string            `json:"-"` // populated from map key
	Providers     []string          `json:"providers" usage:"Provider names to use (order matters for fallback)"`
	Tools         []string          `json:"tools" usage:"MCP tool names to inject"`
	SystemPrompts map[string]string `json:"system_prompts" usage:"Per-model system prompt injection (model_name = prompt_text)"`

	mu           sync.RWMutex
	EnabledTools map[string]bool // runtime state for dynamic toggle
}

// IsToolEnabled checks if a tool is enabled (thread-safe).
func (r *RouteConfig) IsToolEnabled(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.EnabledTools[name]
}

// SetToolEnabled sets a tool's enabled state (thread-safe).
func (r *RouteConfig) SetToolEnabled(name string, enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.EnabledTools[name] = enabled
}

// ToolConfig holds per-tool configuration within an MCPConfig.
type ToolConfig struct {
	Disabled bool         `json:"disabled" usage:"Disable this tool (default: false = enabled)"`
	Hooks    []HookConfig `json:"hooks" usage:"Hooks to run before/after this tool call"`
}

// HookConfig defines a single hook execution target.
type HookConfig struct {
	Type    string   `json:"type" usage:"Hook type: exec or ai"`
	When    string   `json:"when" usage:"Hook timing: pre (can block) or post (audit only)"`
	Timeout string   `json:"timeout" usage:"Hook execution timeout (default: 5s)"`
	Command string   `json:"command" usage:"Command to execute (exec type only)"`
	Args    []string `json:"args" usage:"Command arguments (exec type only)"`
	Route   string   `json:"route" usage:"Gateway route prefix for AI hook (e.g. /openai) (ai type only)"`
	Model   string   `json:"model" usage:"Model name for AI hook (ai type only)"`
	Prompt  string   `json:"prompt" usage:"Prompt template for AI hook; supports {{.ToolName}}, {{.Arguments}}, {{.Result}}, {{.CallID}} (ai type only)"`

	TimeoutDuration time.Duration // parsed from Timeout during Validate
}

type MCPConfig struct {
	Name    string                 `json:"-"` // populated from map key
	Command string                 `json:"command" usage:"MCP server command"`
	Args    []string               `json:"args" usage:"MCP server arguments"`
	Env     map[string]string      `json:"env" usage:"Environment variables"`
	SSH     string                 `json:"ssh" usage:"SSH config name for remote MCP execution"`
	Tools   map[string]*ToolConfig `json:"tools" usage:"Per-tool configuration (disabled flag and hooks)"`

	SSHCfg *SSHConfig // resolved from SSH during Validate
}

// Validate checks configuration validity.
func (c *ConfigStruct) Validate() error {
	// validate SSH configs
	for name, sshCfg := range c.SSH {
		sshCfg.Name = name
		if sshCfg.Host == "" {
			return NewValidationError("ssh %s: host is required", name)
		}
		// expand ~ in identity_file
		if strings.HasPrefix(sshCfg.IdentityFile, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return NewValidationError("ssh %s: cannot determine home directory: %v", name, err)
			}
			sshCfg.IdentityFile = filepath.Join(home, sshCfg.IdentityFile[2:])
		}
	}

	// validate route prefix format
	for prefix, route := range c.Route {
		if prefix == "" || prefix[0] != '/' {
			return NewValidationError("route prefix must start with /: %s", prefix)
		}
		route.Prefix = prefix
	}

	// validate provider configs
	for name, prov := range c.Provider {
		prov.Name = name

		// resolve SSH reference
		if prov.SSH != "" {
			sshCfg, ok := c.SSH[prov.SSH]
			if !ok {
				return NewValidationError("provider %s: unknown ssh config %q", name, prov.SSH)
			}
			prov.SSHCfg = sshCfg
		}

		// apply protocol defaults
		switch prov.Protocol {
		case "qwen":
			if prov.URL == "" {
				if prov.APIKey.Value() != "" {
					prov.URL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
				} else {
					prov.URL = "https://portal.qwen.ai/v1"
				}
			}
			if prov.ConfigDir == "" {
				prov.ConfigDir = "~/.qwen"
			}
		case "copilot":
			if prov.URL == "" {
				prov.URL = "https://api.githubcopilot.com"
			}
			if prov.ConfigDir == "" {
				prov.ConfigDir = "~/.config/github-copilot"
			}
		}

		// expand ~ in config_dir (only for local access, SSH paths are remote)
		if prov.SSHCfg == nil && strings.HasPrefix(prov.ConfigDir, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return NewValidationError("provider %s: cannot determine home directory: %v", name, err)
			}
			prov.ConfigDir = filepath.Join(home, prov.ConfigDir[2:])
		}

		if prov.URL == "" {
			return NewValidationError("provider %s: url is required", name)
		}

		// validate protocol
		switch prov.Protocol {
		case "openai", "anthropic", "ollama":
		case "qwen":
			// verify oauth creds file is readable at startup
			if prov.APIKey.Value() == "" {
				if _, err := qwen.GetAccessToken(prov.ConfigDir, prov.SSHCfg); err != nil {
					return NewValidationError("provider %s: %v", name, err)
				}
			}
		case "copilot":
			// verify GitHub OAuth token is readable at startup
			if prov.APIKey.Value() == "" {
				if _, err := copilot.GetAccessToken(prov.ConfigDir, prov.SSHCfg); err != nil {
					return NewValidationError("provider %s: %v", name, err)
				}
			}
		default:
			return NewValidationError("provider %s: invalid protocol %s (must be openai/anthropic/ollama/qwen/copilot)", name, prov.Protocol)
		}

		// validate request_patch ops
		for i, op := range prov.RequestPatch {
			switch op.Op {
			case "add", "replace":
				if op.Value == nil {
					return NewValidationError("provider %s: request_patch[%d]: op %q requires value", name, i, op.Op)
				}
			case "move", "copy":
				if op.From == "" {
					return NewValidationError("provider %s: request_patch[%d]: op %q requires from", name, i, op.Op)
				}
			case "remove", "test":
			default:
				return NewValidationError("provider %s: request_patch[%d]: unknown op %q", name, i, op.Op)
			}
			if op.Path == "" {
				return NewValidationError("provider %s: request_patch[%d]: path is required", name, i)
			}
		}

		// parse timeout
		if prov.Timeout != "" {
			dur, err := time.ParseDuration(prov.Timeout)
			if err != nil {
				return NewValidationError("provider %s: invalid timeout %s: %v", name, prov.Timeout, err)
			}
			prov.TimeoutDuration = dur
		} else {
			prov.TimeoutDuration = 60 * time.Second
		}

		// parse proxy
		if prov.Proxy != "" {
			proxyURL, err := url.Parse(prov.Proxy)
			if err != nil {
				return NewValidationError("provider %s: invalid proxy URL %s: %v", name, prov.Proxy, err)
			}
			prov.ProxyURL = proxyURL
		}
	}

	// validate route references to providers and tools
	for prefix, route := range c.Route {
		for _, provName := range route.Providers {
			if _, exists := c.Provider[provName]; !exists {
				return NewValidationError("route %s: unknown provider %s", prefix, provName)
			}
		}

		for _, toolName := range route.Tools {
			if _, exists := c.MCP[toolName]; !exists {
				return NewValidationError("route %s: unknown MCP tool %s", prefix, toolName)
			}
		}

		// initialize enabled tools
		route.EnabledTools = make(map[string]bool)
		for _, toolName := range route.Tools {
			route.EnabledTools[toolName] = true
		}
	}

	// validate MCP configs
	for name, mcp := range c.MCP {
		mcp.Name = name
		if mcp.Command == "" {
			return NewValidationError("mcp %s: command is required", name)
		}
		// resolve SSH reference
		if mcp.SSH != "" {
			sshCfg, ok := c.SSH[mcp.SSH]
			if !ok {
				return NewValidationError("mcp %s: unknown ssh config %q", name, mcp.SSH)
			}
			mcp.SSHCfg = sshCfg
		}

		// validate per-tool hook configs
		for toolName, toolCfg := range mcp.Tools {
			for i := range toolCfg.Hooks {
				hook := &toolCfg.Hooks[i]
				switch hook.Type {
				case "exec":
					if hook.Command == "" {
						return NewValidationError("mcp %s tool %s hook[%d]: command is required for exec type", name, toolName, i)
					}
				case "ai":
					if hook.Route == "" {
						return NewValidationError("mcp %s tool %s hook[%d]: route is required for ai type", name, toolName, i)
					}
					if hook.Model == "" {
						return NewValidationError("mcp %s tool %s hook[%d]: model is required for ai type", name, toolName, i)
					}
					if hook.Prompt == "" {
						return NewValidationError("mcp %s tool %s hook[%d]: prompt is required for ai type", name, toolName, i)
					}
				default:
					return NewValidationError("mcp %s tool %s hook[%d]: invalid type %q (must be exec or ai)", name, toolName, i, hook.Type)
				}
				switch hook.When {
				case "pre", "post":
				default:
					return NewValidationError("mcp %s tool %s hook[%d]: invalid when %q (must be pre or post)", name, toolName, i, hook.When)
				}
				timeout := hook.Timeout
				if timeout == "" {
					timeout = "5s"
				}
				dur, err := time.ParseDuration(timeout)
				if err != nil {
					return NewValidationError("mcp %s tool %s hook[%d]: invalid timeout %q: %v", name, toolName, i, timeout, err)
				}
				hook.TimeoutDuration = dur
			}
		}
	}

	return nil
}

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

func NewValidationError(format string, args ...any) error {
	return &ValidationError{
		Message: fmt.Sprintf(format, args...),
	}
}
