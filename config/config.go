package config

import (
	_ "embed"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/wweir/warden/pkg/provider"
)

//go:embed warden.example.yaml
var ExampleConfig string

type ConfigStruct struct {
	Addr          string                     `json:"addr" usage:"Gateway listening address"`
	AdminPassword SecretString               `json:"admin_password" usage:"Admin panel password (empty to disable)"`
	APIKeys       map[string]SecretString    `json:"api_keys" usage:"API keys for programmatic access (name -> key)"`
	Log           *LogConfig                 `json:"log" usage:"Request/response logging configuration"`
	Webhook       map[string]*WebhookConfig  `json:"webhook" usage:"Reusable HTTP webhook configurations (referenced by log http targets)"`
	Provider      map[string]*ProviderConfig `json:"provider" usage:"Upstream LLM provider configurations"`
	Route         map[string]*RouteConfig    `json:"route" usage:"Route prefix configurations"`
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
	BodyTemplate string            `json:"body_template" usage:"Go template for request body; for log target use .Record, for tool hook http use .CallContext/.Args; omit to send caller default JSON"`
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
	Name              string            `json:"-"` // populated from map key
	URL               string            `json:"url" usage:"Upstream LLM base URL"`
	Family            string            `json:"family" usage:"Required provider adapter family: openai, anthropic, ollama, qwen, copilot"`
	Protocol          string            `json:"protocol" usage:"Deprecated alias of family; retained for backward compatibility"`
	APIKey            SecretString      `json:"api_key" usage:"API key for authentication"`
	ConfigDir         string            `json:"config_dir" usage:"Local CLI config directory for OAuth credentials (required for qwen/copilot)"`
	Timeout           string            `json:"timeout" usage:"First-token timeout for non-streaming requests (e.g. 30s, 2m); streaming uses fixed 30s; body reading has no time limit"`
	Proxy             string            `json:"proxy" usage:"HTTP/SOCKS proxy URL (e.g. http://host:port, socks5://host:port)"`
	Headers           map[string]string `json:"headers" usage:"Custom HTTP headers to send with upstream requests (overrides defaults)"`
	Models            []string          `json:"models" usage:"Extra model IDs always included; /models discovery results are merged when available"`
	EnabledProtocols  []string          `json:"enabled_protocols" usage:"Optional allowlist of externally exposed route protocols for this provider family"`
	DisabledProtocols []string          `json:"disabled_protocols" usage:"Optional denylist of externally exposed route protocols for this provider family"`
	ResponsesToChat   bool              `json:"responses_to_chat" usage:"Route responses to upstream /chat/completions for openai protocol"`

	clientCache   map[time.Duration]*http.Client // cached clients by timeout
	clientCacheMu sync.RWMutex
}

// GetAPIKey returns the effective API key for authentication.
// For qwen protocol, reads from local OAuth credentials file if api_key is not set.
func (b *ProviderConfig) GetAPIKey() string {
	if b.APIKey.Value() != "" {
		return b.APIKey.Value()
	}
	if p := provider.Get(b.Protocol); p != nil {
		token, _ := p.GetAccessToken(b.ConfigDir)
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
		p.InvalidateAuth(b.ConfigDir)
	}
}

// HTTPClient returns an *http.Client configured with the provider's proxy and timeout.
// If override is non-zero it is used as the timeout; otherwise falls back to Timeout
// (default 120s). The timeout applies to waiting for response headers (first-token latency),
// not the entire request duration - this allows streaming responses and slow non-streaming
// responses to complete without being terminated by a fixed deadline.
// Clients are cached by timeout value to reuse connections.
func (b *ProviderConfig) HTTPClient(override time.Duration) *http.Client {
	timeout := override
	if timeout == 0 {
		timeout = 120 * time.Second // default first-token timeout
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
	transport.ResponseHeaderTimeout = timeout // first-token timeout
	if b.Proxy != "" {
		if proxyURL, err := url.Parse(b.Proxy); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	if b.clientCache == nil {
		b.clientCache = make(map[time.Duration]*http.Client)
	}

	// No http.Client.Timeout - streaming responses should not have a total time limit
	client := &http.Client{Transport: transport}
	b.clientCache[timeout] = client
	return client
}

type RouteConfig struct {
	Prefix         string                               `json:"-"` // populated from map key
	Protocol       string                               `json:"protocol" usage:"The single external protocol exposed by this route: chat, responses_stateless, responses_stateful, or anthropic"`
	ExactModels    map[string]*ExactRouteModelConfig    `json:"exact_models" usage:"Exact public model mappings for this route protocol; each entry defines explicit upstream provider/model targets"`
	WildcardModels map[string]*WildcardRouteModelConfig `json:"wildcard_models" usage:"Wildcard public model mappings for this route protocol; each pattern defines ordered upstream providers and forwards the requested model unchanged"`
	Hooks          []*HookRuleConfig                    `json:"hooks" usage:"Tool hook rules scoped to this route"`

	exactModels      map[string]*CompiledRouteModel
	wildcards        []*CompiledRouteModel
	serviceProtocols []string
}

type ExactRouteModelConfig struct {
	PromptEnabled *bool                  `json:"prompt_enabled,omitempty" usage:"Whether extra system prompt injection is enabled for this exact route model; nil infers from system_prompt presence"`
	SystemPrompt  string                 `json:"system_prompt" usage:"System prompt injected for this exact route model when prompt_enabled is true"`
	Upstreams     []*RouteUpstreamConfig `json:"upstreams" usage:"Ordered upstream provider/model mappings for this exact public model"`
}

type WildcardRouteModelConfig struct {
	PromptEnabled *bool    `json:"prompt_enabled,omitempty" usage:"Whether extra system prompt injection is enabled for this wildcard route model; nil keeps legacy behavior based on system_prompt presence"`
	SystemPrompt  string   `json:"system_prompt" usage:"System prompt injected for this wildcard route model when prompt_enabled is true"`
	Providers     []string `json:"providers" usage:"Ordered upstream providers for this wildcard route model; requested model is forwarded unchanged"`
}

type RouteUpstreamConfig struct {
	Provider string `json:"provider" usage:"Upstream provider name"`
	Model    string `json:"model" usage:"Upstream model name"`
}

// HookRuleConfig defines a hook rule that matches tool calls by pattern.
// Match uses glob-style wildcards and targets the full tool name seen in model output. Examples: "web_search", "filesystem__write_file", or a bare "*".
type HookRuleConfig struct {
	Match string     `json:"match" usage:"Full tool name pattern to match (supports * wildcard)"`
	Hook  HookConfig `json:"hook" usage:"Hook to run when the pattern matches"`
}

// HookConfig defines a single hook execution target.
type HookConfig struct {
	Type    string   `json:"type" usage:"Hook type: exec, ai or http"`
	When    string   `json:"when" usage:"Hook timing: pre (can block) or post (audit only)"`
	Timeout string   `json:"timeout" usage:"Hook execution timeout (default: 5s)"`
	Command string   `json:"command" usage:"Command to execute (exec type only)"`
	Args    []string `json:"args" usage:"Command arguments (exec type only)"`
	Route   string   `json:"route" usage:"Gateway route prefix for AI hook (e.g. /openai) (ai type only)"`
	Model   string   `json:"model" usage:"Model name for AI hook (ai type only)"`
	Prompt  string   `json:"prompt" usage:"Prompt template for AI hook; supports {{.ToolName}}, {{.FullName}}, {{.MCPName}}, {{.Arguments}}, {{.Result}}, {{.CallID}} (ai type only)"`
	Webhook string   `json:"webhook" usage:"Webhook config name to call (http type only)"`

	TimeoutDuration time.Duration  `json:"-"` // parsed from Timeout during Validate
	WebhookCfg      *WebhookConfig `json:"-"`
}
