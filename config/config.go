package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/pkg/provider"
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
	Webhook       map[string]*WebhookConfig  `json:"webhook" usage:"Reusable HTTP webhook configurations (referenced by log http targets)"`
	Provider      map[string]*ProviderConfig `json:"provider" usage:"Upstream LLM provider configurations"`
	Route         map[string]*RouteConfig    `json:"route" usage:"Route prefix configurations"`
	MCP           map[string]*MCPConfig      `json:"mcp" usage:"MCP server configurations"`
	SSH           map[string]*SSHConfig      `json:"ssh" usage:"SSH connection configurations"`
	ToolHooks     []*HookRuleConfig          `json:"tool_hooks" usage:"Global tool call hook rules (supports wildcards in match pattern)"`
}

type LogConfig struct {
	Targets []*LogTarget `json:"targets" usage:"Log output targets (file, http)"`
}

// LogTarget defines a single log output destination.
// Type must be "file" or "http".
type LogTarget struct {
	Type string `json:"type" usage:"Target type: file or http"`

	// file type fields
	Dir string `json:"dir" usage:"Directory for JSON log files (file type only)"`

	// http type fields
	Webhook string `json:"webhook" usage:"Webhook config name to use for HTTP push (http type only)"`

	WebhookCfg *WebhookConfig `json:"-"` // resolved from Webhook during Validate
}

// WebhookConfig defines a complete HTTP webhook call configuration.
type WebhookConfig struct {
	URL          string            `json:"url" usage:"Target URL"`
	Method       string            `json:"method" usage:"HTTP method, default POST"`
	Headers      map[string]string `json:"headers" usage:"Static request headers"`
	BodyTemplate string            `json:"body_template" usage:"Go template for request body; .Record holds the log record, sprig functions available; omit to send record as plain JSON"`
	Timeout      string            `json:"timeout" usage:"Per-request timeout, default 5s"`
	Retry        int               `json:"retry" usage:"Retry count on failure, default 2"`
}

// LogValue implements slog.LogValuer to print a safe summary without secrets or pointers.
func (c *ConfigStruct) LogValue() slog.Value {
	providers := make([]string, 0, len(c.Provider))
	for name := range c.Provider {
		providers = append(providers, name)
	}
	routes := make([]string, 0, len(c.Route))
	for prefix := range c.Route {
		routes = append(routes, prefix)
	}
	attrs := []slog.Attr{
		slog.String("addr", c.Addr),
		slog.Any("providers", providers),
		slog.Any("routes", routes),
	}
	if c.AdminPassword != "" {
		attrs = append(attrs, slog.String("admin_password", "***"))
	}
	return slog.GroupValue(attrs...)
}

type ProviderConfig struct {
	Name         string            `json:"-"` // populated from map key
	URL          string            `json:"url" usage:"Upstream LLM base URL"`
	Protocol     string            `json:"protocol" usage:"API protocol: openai, anthropic, ollama, qwen, copilot"`
	APIKey       deferlog.Secret   `json:"api_key" usage:"API key for authentication"`
	ConfigDir    string            `json:"config_dir" usage:"Local CLI config directory for OAuth credentials (required for qwen/copilot)"`
	Timeout      string            `json:"timeout" usage:"Request timeout (e.g. 60s, 2m)"`
	Proxy        string            `json:"proxy" usage:"HTTP/SOCKS proxy URL (e.g. http://host:port, socks5://host:port)"`
	Headers      map[string]string `json:"headers" usage:"Custom HTTP headers to send with upstream requests (overrides defaults)"`
	Models       []string          `json:"models" usage:"Supported model IDs (skips /models discovery when set)"`
	ModelAliases map[string]string `json:"model_aliases" usage:"Model alias mapping (alias_name = real_name), alias appears in /models and is resolved before upstream request"`
	RequestPatch []RequestPatchOp  `json:"request_patch" usage:"JSON Patch operations (RFC 6902) applied to request body before forwarding"`

	clientCache   map[time.Duration]*http.Client // cached clients by timeout
	clientCacheMu sync.RWMutex
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
	if p := provider.Get(b.Protocol); p != nil {
		token, _ := p.GetAccessToken(b.ConfigDir, nil)
		return token
	}
	return ""
}

// InvalidateAuth clears the cached OAuth credentials for qwen/copilot providers,
// forcing a re-read from disk on the next GetAPIKey call.
// No-op for providers with a static api_key.
func (b *ProviderConfig) InvalidateAuth() {
	if b.APIKey.Value() != "" {
		return
	}
	if p := provider.Get(b.Protocol); p != nil {
		p.InvalidateAuth(b.ConfigDir, nil)
	}
}

// HTTPClient returns an *http.Client configured with the provider's proxy and timeout.
// If override is non-zero it is used as the timeout; otherwise falls back to Timeout
// (default 60s). Format errors are caught during Validate, so errors here are ignored.
// Clients are cached by timeout value to reuse connections.
func (b *ProviderConfig) HTTPClient(override time.Duration) *http.Client {
	timeout := override
	if timeout == 0 {
		timeout = 60 * time.Second
		if b.Timeout != "" {
			if d, err := time.ParseDuration(b.Timeout); err == nil {
				timeout = d
			}
		}
	}

	b.clientCacheMu.RLock()
	if client, ok := b.clientCache[timeout]; ok {
		b.clientCacheMu.RUnlock()
		return client
	}
	b.clientCacheMu.RUnlock()

	b.clientCacheMu.Lock()
	defer b.clientCacheMu.Unlock()

	// double-check after acquiring write lock
	if client, ok := b.clientCache[timeout]; ok {
		return client
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if b.Proxy != "" {
		if proxyURL, err := url.Parse(b.Proxy); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	if b.clientCache == nil {
		b.clientCache = make(map[time.Duration]*http.Client)
	}

	client := &http.Client{Timeout: timeout, Transport: transport}
	b.clientCache[timeout] = client
	return client
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
	EnabledTools map[string]bool `json:"-"` // runtime state for dynamic toggle
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
	Disabled bool `json:"disabled" usage:"Disable this tool (default: false = enabled)"`
}

// HookRuleConfig defines a hook rule that matches tool calls by pattern.
// Match uses glob-style wildcards: "*" matches any sequence within a segment,
// "**" matches across segments. Pattern format: "<mcp_name>__<tool_name>",
// e.g. "my_mcp__write_*" matches all write tools in my_mcp.
// A bare "*" matches all tool calls.
type HookRuleConfig struct {
	Match string       `json:"match" usage:"Tool name pattern to match (format: mcp_name__tool_name, supports * wildcard)"`
	Hook  HookConfig   `json:"hook" usage:"Hook to run when the pattern matches"`
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

	TimeoutDuration time.Duration `json:"-"` // parsed from Timeout during Validate
}

type MCPConfig struct {
	Name    string                 `json:"-"` // populated from map key
	Command string                 `json:"command" usage:"MCP server command"`
	Args    []string               `json:"args" usage:"MCP server arguments"`
	Env     map[string]string      `json:"env" usage:"Environment variables"`
	SSH     string                 `json:"ssh" usage:"SSH config name for remote MCP execution"`
	Tools   map[string]*ToolConfig `json:"tools" usage:"Per-tool configuration (disabled flag and hooks)"`

	SSHCfg *SSHConfig `json:"-"` // resolved from SSH during Validate
}

// Validate checks configuration validity.
func (c *ConfigStruct) Validate() error {
	// validate log targets
	if c.Log != nil {
		for i, t := range c.Log.Targets {
			switch t.Type {
			case "file":
				if t.Dir == "" {
					return NewValidationError("log.targets[%d]: dir is required for file type", i)
				}
			case "http":
				if t.Webhook == "" {
					return NewValidationError("log.targets[%d]: webhook is required for http type", i)
				}
				webhookCfg, ok := c.Webhook[t.Webhook]
				if !ok {
					return NewValidationError("log.targets[%d]: unknown webhook %q", i, t.Webhook)
				}
				t.WebhookCfg = webhookCfg
			default:
				return NewValidationError("log.targets[%d]: invalid type %q (must be file or http)", i, t.Type)
			}
		}
	}

	// validate webhook configs
	for name, wh := range c.Webhook {
		if wh.URL == "" {
			return NewValidationError("webhook %s: url is required", name)
		}
		if _, err := url.Parse(wh.URL); err != nil {
			return NewValidationError("webhook %s: invalid url %s: %v", name, wh.URL, err)
		}
		if wh.Timeout != "" {
			if _, err := time.ParseDuration(wh.Timeout); err != nil {
				return NewValidationError("webhook %s: invalid timeout %s: %v", name, wh.Timeout, err)
			}
		}
	}

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

		// expand ~ in config_dir
		if strings.HasPrefix(prov.ConfigDir, "~/") {
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
		case "qwen", "copilot":
			// verify OAuth credentials file is readable at startup (no network IO)
			if prov.APIKey.Value() == "" {
				if p := provider.Get(prov.Protocol); p != nil {
					if err := p.CheckCredsReadable(prov.ConfigDir, nil); err != nil {
						return NewValidationError("provider %s: %v", name, err)
					}
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

		// validate timeout format
		if prov.Timeout != "" {
			if _, err := time.ParseDuration(prov.Timeout); err != nil {
				return NewValidationError("provider %s: invalid timeout %s: %v", name, prov.Timeout, err)
			}
		}

		// validate proxy URL format
		if prov.Proxy != "" {
			if _, err := url.Parse(prov.Proxy); err != nil {
				return NewValidationError("provider %s: invalid proxy URL %s: %v", name, prov.Proxy, err)
			}
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

	}

	// validate global tool hook rules
	for i, rule := range c.ToolHooks {
		if rule.Match == "" {
			return NewValidationError("tool_hooks[%d]: match is required", i)
		}
		if err := validateHookConfig(fmt.Sprintf("tool_hooks[%d]", i), &rule.Hook); err != nil {
			return err
		}
	}

	return nil
}

// validateHookConfig validates a single HookConfig and parses its timeout.
func validateHookConfig(ctx string, hook *HookConfig) error {
	switch hook.Type {
	case "exec":
		if hook.Command == "" {
			return NewValidationError("%s: command is required for exec type", ctx)
		}
	case "ai":
		if hook.Route == "" {
			return NewValidationError("%s: route is required for ai type", ctx)
		}
		if hook.Model == "" {
			return NewValidationError("%s: model is required for ai type", ctx)
		}
		if hook.Prompt == "" {
			return NewValidationError("%s: prompt is required for ai type", ctx)
		}
	default:
		return NewValidationError("%s: invalid type %q (must be exec or ai)", ctx, hook.Type)
	}
	switch hook.When {
	case "pre", "post":
	default:
		return NewValidationError("%s: invalid when %q (must be pre or post)", ctx, hook.When)
	}
	timeout := hook.Timeout
	if timeout == "" {
		timeout = "5s"
	}
	dur, err := time.ParseDuration(timeout)
	if err != nil {
		return NewValidationError("%s: invalid timeout %q: %v", ctx, timeout, err)
	}
	hook.TimeoutDuration = dur
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
