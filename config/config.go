package config

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/wweir/warden/pkg/provider"
)

const (
	DefaultConfigDir       = "/etc/warden"
	DefaultConfigPath      = DefaultConfigDir + "/warden.toml"
	DefaultCLIProxyAuthDir = DefaultConfigDir
)

//go:embed warden.example.toml
var ExampleConfig string

type ConfigStruct struct {
	Addr          string                     `json:"addr" usage:"Gateway listening address"`
	AdminPassword SecretString               `json:"admin_password" usage:"Admin panel password (empty to disable)"`
	APIKeys       map[string]SecretString    `json:"api_keys" usage:"Deprecated: move client API keys into route.<prefix>.api_keys"`
	Log           *LogConfig                 `json:"log" usage:"Request/response logging configuration"`
	Webhook       map[string]*WebhookConfig  `json:"webhook" usage:"Reusable HTTP webhook configurations (referenced by log http targets)"`
	CLIProxy      *CLIProxyConfig            `json:"cliproxy" usage:"Embedded CLIProxyAPI backend configuration"`
	Provider      map[string]*ProviderConfig `json:"provider" usage:"Upstream LLM provider configurations"`
	Route         map[string]*RouteConfig    `json:"route" usage:"Route prefix configurations"`
}

type CLIProxyConfig struct {
	Enabled             bool   `json:"enabled" usage:"Start an embedded CLIProxyAPI/cliproxy service for cliproxy-backed providers"`
	AuthDir             string `json:"auth_dir" usage:"Authentication token directory used by the embedded cliproxy service"`
	Proxy               string `json:"proxy" usage:"Optional outbound proxy URL used by embedded cliproxy providers"`
	RequestRetry        int    `json:"request_retry" usage:"Retry count used by embedded cliproxy"`
	MaxRetryCredentials int    `json:"max_retry_credentials" usage:"Maximum number of cliproxy credentials to try for a failed request; 0 means upstream default"`
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
	Name                 string            `json:"-"` // populated from map key
	Disabled             bool              `json:"disabled,omitempty" usage:"Disable this provider from receiving traffic (manual suppress)"`
	URL                  string            `json:"url,omitempty" usage:"Upstream LLM base URL (shorthand for single-endpoint providers)"`
	Format               string            `json:"format,omitempty" usage:"Upstream native protocol format: openai, anthropic, copilot; empty defaults to openai (shorthand for single-endpoint providers)"`
	Protocols            []string          `json:"protocols,omitempty" usage:"Supported service protocols: chat, responses, anthropic, embeddings; empty uses format defaults (shorthand for single-endpoint providers)"`
	Backend              string            `json:"backend" usage:"Optional upstream backend marker, currently cliproxy"`
	BackendProvider      string            `json:"backend_provider" usage:"Provider name inside the upstream backend, for example codex"`
	APIKey               SecretString      `json:"api_key" usage:"API key for authentication"`
	APIKeyCommand        string            `json:"api_key_command" usage:"Shell command that prints an API key to stdout"`
	APIKeyCommandTimeout string            `json:"api_key_command_timeout" usage:"Timeout for api_key_command, default 5s"`
	APIKeyCommandTTL     string            `json:"api_key_command_ttl" usage:"Cache TTL for api_key_command output, default 5m; 0s disables cache"`
	ConfigDir            string            `json:"config_dir" usage:"Local CLI config directory for OAuth credentials (required for copilot)"`
	Timeout              string            `json:"timeout" usage:"First-token timeout from upstream request start to first response body byte (e.g. 30s, 2m); body reading after the first token has no time limit"`
	Proxy                string            `json:"proxy" usage:"HTTP/SOCKS proxy URL (e.g. http://host:port, socks5://host:port)"`
	Headers              map[string]string `json:"headers" usage:"Custom HTTP headers to send with upstream requests (overrides defaults)"`
	Models               []string          `json:"models" usage:"Extra model IDs always included; /models discovery results are merged when available"`
	ResponsesToChat      bool              `json:"responses_to_chat,omitempty" usage:"Route responses to upstream /chat/completions for openai format (shorthand for single-endpoint providers)"`
	AnthropicToChat      bool              `json:"anthropic_to_chat,omitempty" usage:"Route anthropic /messages to upstream /chat/completions for openai format (shorthand for single-endpoint providers)"`
	AnthropicToResponses bool              `json:"anthropic_to_responses,omitempty" usage:"Route responses to upstream /messages for anthropic format (stateless only) (shorthand for single-endpoint providers)"`
	Endpoints            map[string]*ProviderEndpointConfig `json:"endpoint,omitempty" usage:"Explicit protocol endpoints for multi-endpoint providers"`

	clientCache   map[time.Duration]*http.Client // cached clients by timeout
	clientCacheMu sync.RWMutex

	apiKeyCommandMu    sync.Mutex
	apiKeyCommandCache providerAPIKeyCommandCache
}

const defaultProviderFirstTokenTimeout = 120 * time.Second

type providerAPIKeyCommandCache struct {
	Command   string
	Token     string
	ExpiresAt time.Time
}

// ProviderEndpointConfig defines a single protocol endpoint for a provider.
// Each endpoint is a complete protocol access definition, not an override.
type ProviderEndpointConfig struct {
	URL                  string            `json:"url" usage:"Endpoint base URL"`
	Format               string            `json:"format,omitempty" usage:"Endpoint native protocol format: openai, anthropic, copilot; empty defaults to openai"`
	Protocols            []string          `json:"protocols,omitempty" usage:"Endpoint supported service protocols; empty uses format defaults"`
	Headers              map[string]string `json:"headers,omitempty" usage:"Endpoint-specific headers merged with provider-level headers"`
	Models               []string          `json:"models,omitempty" usage:"Endpoint-specific model list overriding provider-level models"`
	ResponsesToChat      bool              `json:"responses_to_chat,omitempty" usage:"Route responses to this openai-format endpoint"`
	AnthropicToChat      bool              `json:"anthropic_to_chat,omitempty" usage:"Route anthropic /messages to this openai-format endpoint"`
	AnthropicToResponses bool              `json:"anthropic_to_responses,omitempty" usage:"Route responses to this anthropic-format endpoint (stateless only)"`
}

// GetAPIKey returns the effective API key for authentication.
func (b *ProviderConfig) GetAPIKey(ctx context.Context) string {
	token, _ := b.ResolveAPIKey(ctx)
	return token
}

// InvalidateAuth clears cached dynamic provider credentials.
func (b *ProviderConfig) InvalidateAuth() {
	b.clearAPIKeyCommandCache()
	if b == nil || b.APIKey.Value() != "" {
		return
	}
	if p := provider.Get(b.Format); p != nil {
		p.InvalidateAuth(b.ConfigDir)
	}
}

// FirstTokenTimeout returns the provider timeout for waiting until the first response body byte.
func (b *ProviderConfig) FirstTokenTimeout(override time.Duration) time.Duration {
	if override > 0 {
		return override
	}
	if b != nil && b.Timeout != "" {
		if d, err := time.ParseDuration(b.Timeout); err == nil && d > 0 {
			return d
		}
	}
	return defaultProviderFirstTokenTimeout
}

// HTTPClient returns an *http.Client configured with the provider's proxy.
func (b *ProviderConfig) HTTPClient(override time.Duration) *http.Client {
	timeout := b.FirstTokenTimeout(override)

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
	transport.ResponseHeaderTimeout = timeout
	transport.IdleConnTimeout = 30 * time.Second
	if b.Backend == ProviderBackendCLIProxy {
		transport.Proxy = nil
	} else if b.Proxy != "" {
		if proxyURL, err := url.Parse(b.Proxy); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	client := &http.Client{Transport: transport}
	if b.clientCache == nil {
		b.clientCache = make(map[time.Duration]*http.Client)
	}
	b.clientCache[timeout] = client
	return client
}

type RouteConfig struct {
	Prefix           string                               `json:"-"` // populated from map key
	Protocol         string                               `json:"protocol" usage:"Primary route protocol: chat, responses, or anthropic"`
	ServiceProtocols []string                             `json:"service_protocols" usage:"External service protocols exposed by this route: chat, responses, anthropic, embeddings; empty derives from protocol"`
	APIKeys          map[string]SecretString              `json:"api_keys" usage:"Client API keys allowed to access this route (name -> key); empty means no client auth"`
	ExactModels      map[string]*ExactRouteModelConfig    `json:"exact_models" usage:"Exact public model mappings for this route protocol; each entry defines explicit upstream provider/model targets"`
	WildcardModels   map[string]*WildcardRouteModelConfig `json:"wildcard_models" usage:"Wildcard public model mappings for this route protocol; each pattern defines ordered upstream providers and forwards the requested model unchanged"`
	Hooks            []*HookRuleConfig                    `json:"hooks" usage:"Tool hook rules scoped to this route"`

	exactModels      map[string]*CompiledRouteModel
	wildcards        []*CompiledRouteModel
	serviceProtocols []string
	apiKeysMu        sync.RWMutex
}

func (r *RouteConfig) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	return json.Marshal(struct {
		Protocol         string                               `json:"protocol"`
		ServiceProtocols []string                             `json:"service_protocols,omitempty"`
		APIKeys          map[string]SecretString              `json:"api_keys"`
		ExactModels      map[string]*ExactRouteModelConfig    `json:"exact_models"`
		WildcardModels   map[string]*WildcardRouteModelConfig `json:"wildcard_models"`
		Hooks            []*HookRuleConfig                    `json:"hooks"`
	}{
		Protocol:         r.Protocol,
		ServiceProtocols: r.ServiceProtocols,
		APIKeys:          r.CloneAPIKeys(),
		ExactModels:      r.ExactModels,
		WildcardModels:   r.WildcardModels,
		Hooks:            r.Hooks,
	})
}

func (r *RouteConfig) APIKeyCount() int {
	if r == nil {
		return 0
	}
	r.apiKeysMu.RLock()
	defer r.apiKeysMu.RUnlock()
	return len(r.APIKeys)
}

func (r *RouteConfig) SetAPIKey(name string, value SecretString) {
	if r == nil {
		return
	}
	r.apiKeysMu.Lock()
	defer r.apiKeysMu.Unlock()
	if r.APIKeys == nil {
		r.APIKeys = make(map[string]SecretString)
	}
	r.APIKeys[name] = value
}

func (r *RouteConfig) AddAPIKey(name string, value SecretString) bool {
	if r == nil {
		return false
	}
	r.apiKeysMu.Lock()
	defer r.apiKeysMu.Unlock()
	if r.APIKeys == nil {
		r.APIKeys = make(map[string]SecretString)
	}
	if _, exists := r.APIKeys[name]; exists {
		return false
	}
	r.APIKeys[name] = value
	return true
}

func (r *RouteConfig) DeleteAPIKey(name string) (SecretString, bool) {
	if r == nil {
		return "", false
	}
	r.apiKeysMu.Lock()
	defer r.apiKeysMu.Unlock()
	previous, exists := r.APIKeys[name]
	if !exists {
		return "", false
	}
	delete(r.APIKeys, name)
	return previous, true
}

func (r *RouteConfig) CloneAPIKeys() map[string]SecretString {
	if r == nil {
		return nil
	}
	r.apiKeysMu.RLock()
	defer r.apiKeysMu.RUnlock()
	if len(r.APIKeys) == 0 {
		return nil
	}
	cloned := make(map[string]SecretString, len(r.APIKeys))
	for name, value := range r.APIKeys {
		cloned[name] = value
	}
	return cloned
}

func (r *RouteConfig) MatchAPIKey(token string) (string, bool) {
	if r == nil || token == "" {
		return "", false
	}
	r.apiKeysMu.RLock()
	defer r.apiKeysMu.RUnlock()
	var matched string
	for name, key := range r.APIKeys {
		if subtle.ConstantTimeCompare([]byte(token), []byte(key.Value())) == 1 {
			matched = name
		}
	}
	if matched == "" {
		return "", false
	}
	return matched, true
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
	When    string   `json:"when" usage:"Hook timing: block (reject unsafe tool calls in non-stream responses) or async (audit only, log results)"` // "pre"/"post" accepted as backward-compatible aliases
	Timeout string   `json:"timeout" usage:"Hook execution timeout (default: 5s)"`
	Command string   `json:"command" usage:"Command to execute (exec type only)"`
	Args    []string `json:"args" usage:"Command arguments (exec type only)"`
	Route   string   `json:"route" usage:"Gateway route prefix for AI hook (e.g. /openai) (ai type only)"`
	Model   string   `json:"model" usage:"Model name for AI hook (ai type only)"`
	Prompt  string   `json:"prompt" usage:"Prompt template for AI hook; supports {{.ToolName}}, {{.FullName}}, {{.MCPName}}, {{.Arguments}}, {{.Result}}, {{.CallID}} (ai type only)"`
	Webhook string   `json:"webhook" usage:"Webhook config name to call (http type only)"`

	TimeoutDuration time.Duration  `json:"-"`
	WebhookCfg      *WebhookConfig `json:"-"`
}
