package config

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/wweir/warden/pkg/provider"
)

var supportedProxySchemes = map[string]bool{
	"http":    true,
	"https":   true,
	"socks5":  true,
	"socks5h": true,
}

const providerCredCheckTimeout = 5 * time.Second

// Validate checks configuration validity.
func (c *ConfigStruct) Validate() error {
	if len(c.APIKeys) > 0 {
		return NewValidationError("top-level api_keys is deprecated; move client API keys into route.<prefix>.api_keys")
	}
	if err := c.validateLogConfig(); err != nil {
		return err
	}
	if err := c.validateWebhookConfig(); err != nil {
		return err
	}
	if err := c.validateRoutePrefixes(); err != nil {
		return err
	}
	if err := c.validateProviderConfig(); err != nil {
		return err
	}
	if err := c.validateCLIProxyConfig(); err != nil {
		return err
	}
	if err := c.validateRouteConfig(); err != nil {
		return err
	}
	return nil
}

func (c *ConfigStruct) validateLogConfig() error {
	if c.Log == nil {
		return nil
	}

	for i, target := range c.Log.Targets {
		if target == nil {
			return NewValidationError("log.targets[%d]: target is required", i)
		}
		switch target.Type {
		case "file":
			if target.Dir == "" {
				return NewValidationError("log.targets[%d]: dir is required for file type", i)
			}
		case "http":
			if err := resolveWebhookReference(fmt.Sprintf("log.targets[%d]", i), target.Webhook, c.Webhook, func(cfg *WebhookConfig) {
				target.WebhookCfg = cfg
			}); err != nil {
				return err
			}
		default:
			return NewValidationError("log.targets[%d]: invalid type %q (must be file or http)", i, target.Type)
		}
	}

	return nil
}

func (c *ConfigStruct) validateWebhookConfig() error {
	for name, webhook := range c.Webhook {
		if webhook == nil {
			return NewValidationError("webhook %s: config is required", name)
		}
		if webhook.URL == "" {
			return NewValidationError("webhook %s: url is required", name)
		}
		if err := validateHTTPURL("webhook "+name, webhook.URL); err != nil {
			return err
		}
		if err := validateWebhookRuntimeConfig("webhook "+name, webhook); err != nil {
			return err
		}
	}

	return nil
}

func validateWebhookRuntimeConfig(ctx string, webhook *WebhookConfig) error {
	if webhook.BodyTemplate != "" {
		if _, err := template.New("body").Funcs(sprig.FuncMap()).Parse(webhook.BodyTemplate); err != nil {
			return NewValidationError("%s: invalid body_template: %v", ctx, err)
		}
	}
	if webhook.Timeout == "" {
		return nil
	}
	dur, err := time.ParseDuration(webhook.Timeout)
	if err != nil {
		return NewValidationError("%s: invalid timeout %s: %v", ctx, webhook.Timeout, err)
	}
	if dur <= 0 {
		return NewValidationError("%s: timeout must be greater than 0", ctx)
	}
	return nil
}

func (c *ConfigStruct) validateCLIProxyConfig() error {
	if c.CLIProxy == nil {
		return nil
	}

	c.CLIProxy.AuthDir = strings.TrimSpace(c.CLIProxy.AuthDir)
	c.CLIProxy.Proxy = strings.TrimSpace(c.CLIProxy.Proxy)
	if c.CLIProxy.AuthDir == "" {
		c.CLIProxy.AuthDir = "~/.cli-proxy-api"
	}
	authDir, err := expandHomeDir(c.CLIProxy.AuthDir)
	if err != nil {
		return NewValidationError("cliproxy.auth_dir: %v", err)
	}
	c.CLIProxy.AuthDir = authDir

	if c.CLIProxy.Proxy != "" {
		if err := validateProxyURL("cliproxy.proxy", c.CLIProxy.Proxy); err != nil {
			return err
		}
	}
	if c.CLIProxy.RequestRetry < 0 {
		return NewValidationError("cliproxy.request_retry must be >= 0")
	}
	if c.CLIProxy.MaxRetryCredentials < 0 {
		return NewValidationError("cliproxy.max_retry_credentials must be >= 0")
	}
	if !c.CLIProxy.Enabled {
		return nil
	}

	endpoint := ""
	count := 0
	for name, prov := range c.Provider {
		if prov == nil || prov.Backend != ProviderBackendCLIProxy {
			continue
		}
		count++
		parsed, err := url.Parse(prov.URL)
		if err != nil {
			return NewValidationError("provider %s: invalid cliproxy backend url %s: %v", name, prov.URL, err)
		}
		if parsed.Scheme != "http" {
			return NewValidationError("provider %s: embedded cliproxy backend url must use http, got %q", name, parsed.Scheme)
		}
		if !isLoopbackHost(parsed.Hostname()) {
			return NewValidationError("provider %s: embedded cliproxy backend url must use a loopback host, got %q", name, parsed.Hostname())
		}
		if parsed.Port() == "" {
			return NewValidationError("provider %s: embedded cliproxy backend url must include an explicit port", name)
		}
		if parsed.EscapedPath() != "/v1" {
			return NewValidationError("provider %s: embedded cliproxy backend url path must be /v1", name)
		}
		current := parsed.Scheme + "://" + parsed.Host
		if endpoint == "" {
			endpoint = current
			continue
		}
		if endpoint != current {
			return NewValidationError("provider %s: all embedded cliproxy backend providers must share the same endpoint %s, got %s", name, endpoint, current)
		}
	}
	if count == 0 {
		return NewValidationError("cliproxy.enabled requires at least one provider with backend 'cliproxy'")
	}

	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() != nil && ip.IsLoopback()
}

func (c *ConfigStruct) validateRoutePrefixes() error {
	for prefix, route := range c.Route {
		if prefix == "" || prefix[0] != '/' {
			return NewValidationError("route prefix must start with /: %s", prefix)
		}

		route.Prefix = prefix
	}

	return nil
}

func (c *ConfigStruct) validateProviderConfig() error {
	for name, prov := range c.Provider {
		prov.Name = name
		rawFamily := normalizeProviderProtocol(prov.Family)
		rawProtocol := normalizeProviderProtocol(prov.Protocol)
		if rawFamily == "ollama" || rawProtocol == "ollama" {
			return NewValidationError("provider %s: 'ollama' family/protocol has been removed; use family 'openai' and set service_protocols to ['chat'] for Ollama-compatible endpoints", name)
		}
		prov.Family = normalizeProviderAdapterProtocol(rawFamily)
		prov.Protocol = normalizeProviderAdapterProtocol(rawProtocol)
		if prov.Family != "" && prov.Protocol != "" && prov.Family != prov.Protocol {
			return NewValidationError("provider %s: family %q conflicts with protocol %q", name, rawFamily, rawProtocol)
		}
		if prov.Protocol == "" {
			prov.Protocol = prov.Family
		}
		if prov.Protocol == "" {
			return NewValidationError("provider %s: family is required", name)
		}
		prov.Family = prov.Protocol
		if prov.Protocol == "qwen" {
			return NewValidationError("provider %s: family %q is no longer supported; use family %q with a Qwen-compatible endpoint instead", name, prov.Protocol, ProviderProtocolOpenAI)
		}
		prov.Backend = normalizeProviderBackend(prov.Backend)
		prov.BackendProvider = normalizeProviderBackend(prov.BackendProvider)
		if prov.Backend == "" && prov.BackendProvider != "" {
			return NewValidationError("provider %s: backend_provider requires backend", name)
		}
		switch prov.Backend {
		case "":
		case ProviderBackendCLIProxy:
			if prov.Protocol != ProviderProtocolOpenAI {
				return NewValidationError("provider %s: backend %q requires family 'openai', got %q", name, prov.Backend, prov.Protocol)
			}
			if prov.BackendProvider == "" {
				return NewValidationError("provider %s: backend_provider is required when backend is %q", name, prov.Backend)
			}
			if len(prov.ServiceProtocols) == 0 {
				return NewValidationError("provider %s: backend %q requires explicit service_protocols", name, prov.Backend)
			}
		default:
			return NewValidationError("provider %s: invalid backend %q (must be cliproxy)", name, prov.Backend)
		}
		if len(prov.ServiceProtocols) > 0 {
			if !validConfiguredServiceProtocols(prov.ServiceProtocols) {
				return NewValidationError("provider %s: invalid service_protocols %v (must be chat/responses_stateless/responses_stateful/anthropic/embeddings)", name, prov.ServiceProtocols)
			}
			prov.ServiceProtocols = normalizeConfiguredServiceProtocols(prov.ServiceProtocols)
			allowedProtocols := DefaultServiceProtocols(prov)
			for _, protocol := range prov.ServiceProtocols {
				if !slices.Contains(allowedProtocols, protocol) {
					return NewValidationError("provider %s: service_protocols %v is incompatible with adapter capabilities %v", name, prov.ServiceProtocols, allowedProtocols)
				}
			}
		}
		applyProviderDefaults(prov)

		configDir, err := expandHomeDir(prov.ConfigDir)
		if err != nil {
			return NewValidationError("provider %s: %v", name, err)
		}
		prov.ConfigDir = configDir

		if prov.URL == "" {
			return NewValidationError("provider %s: url is required", name)
		}
		if err := validateHTTPURL("provider "+name, prov.URL); err != nil {
			return err
		}

		switch prov.Protocol {
		case ProviderProtocolOpenAI, ProviderProtocolAnthropic:
		case ProviderProtocolCopilot:
			if prov.APIKey.Value() == "" {
				if p := provider.Get(prov.Protocol); p != nil {
					ctx, cancel := context.WithTimeout(context.Background(), providerCredCheckTimeout)
					err := p.CheckCredsReadable(ctx, prov.ConfigDir)
					cancel()
					if err != nil {
						return NewValidationError("provider %s: %v", name, err)
					}
				}
			}
		default:
			return NewValidationError("provider %s: invalid protocol %s (must be openai/anthropic/copilot)", name, prov.Protocol)
		}

		if prov.Timeout != "" {
			if _, err := time.ParseDuration(prov.Timeout); err != nil {
				return NewValidationError("provider %s: invalid timeout %s: %v", name, prov.Timeout, err)
			}
		}

		if prov.Proxy != "" {
			if err := validateProxyURL("provider "+name, prov.Proxy); err != nil {
				return err
			}
		}

		if prov.ResponsesToChat && prov.Protocol != "openai" {
			return NewValidationError("provider %s: responses_to_chat requires protocol 'openai', got %q", name, prov.Protocol)
		}
		if prov.AnthropicToChat && prov.Protocol != "openai" {
			return NewValidationError("provider %s: anthropic_to_chat requires protocol 'openai', got %q", name, prov.Protocol)
		}
	}

	return nil
}

func (c *ConfigStruct) validateRouteConfig() error {
	for prefix, route := range c.Route {
		route.exactModels = make(map[string]*CompiledRouteModel)
		route.wildcards = nil
		explicitServiceProtocols := len(route.ServiceProtocols) > 0

		if err := validateConfiguredProtocolName(prefix, "", route.Protocol); err != nil {
			return err
		}
		route.Protocol = normalizeRouteProtocol(route.Protocol)
		if len(route.ServiceProtocols) > 0 && !validConfiguredServiceProtocols(route.ServiceProtocols) {
			return NewValidationError("route %s: invalid service_protocols %v (must be chat/responses_stateless/responses_stateful/anthropic/embeddings)", prefix, route.ServiceProtocols)
		}
		route.serviceProtocols = configuredRouteServiceProtocols(route)
		if len(route.serviceProtocols) == 0 {
			return NewValidationError("route %s: protocol is required", prefix)
		}
		if !slices.Contains(route.serviceProtocols, route.Protocol) {
			return NewValidationError("route %s: service_protocols must include route protocol %s", prefix, route.Protocol)
		}

		if len(route.ExactModels) == 0 && len(route.WildcardModels) == 0 {
			return NewValidationError("route %s: exact_models or wildcard_models is required", prefix)
		}
		if err := validateRouteAPIKeys(prefix, route.CloneAPIKeys()); err != nil {
			return err
		}

		for i, rule := range route.Hooks {
			if rule.Match == "" {
				return NewValidationError("route %s hooks[%d]: match is required", prefix, i)
			}
			if err := validateHookConfig(fmt.Sprintf("route %s hooks[%d]", prefix, i), &rule.Hook, c.Webhook, c.Route); err != nil {
				return err
			}
		}

		for modelName, modelCfg := range route.ExactModels {
			if err := c.validateExactRouteModel(prefix, route, modelName, modelCfg); err != nil {
				return err
			}
		}

		for pattern, modelCfg := range route.WildcardModels {
			if err := c.validateWildcardRouteModel(prefix, route, pattern, modelCfg); err != nil {
				return err
			}
		}

		slices.SortFunc(route.wildcards, func(a, b *CompiledRouteModel) int {
			if cmp := comparePatternSpecificity(b.Specificity, a.Specificity); cmp != 0 {
				return cmp
			}
			return strings.Compare(a.Pattern, b.Pattern)
		})
		for i := 0; i < len(route.wildcards); i++ {
			for j := i + 1; j < len(route.wildcards); j++ {
				a := route.wildcards[i]
				b := route.wildcards[j]
				if comparePatternSpecificity(a.Specificity, b.Specificity) != 0 {
					break
				}
				if wildcardPatternsConflict(a.Pattern, b.Pattern) {
					return NewValidationError("route %s wildcard models %q and %q have ambiguous precedence", prefix, a.Pattern, b.Pattern)
				}
			}
		}

		if explicitServiceProtocols {
			if err := validateExplicitRouteServiceProtocolSupport(prefix, route, c.Provider); err != nil {
				return err
			}
		} else {
			PruneUnsupportedRouteServiceProtocols(route, c.Provider)
		}
	}

	return nil
}

func validateExplicitRouteServiceProtocolSupport(prefix string, route *RouteConfig, providers map[string]*ProviderConfig) error {
	for _, serviceProtocol := range route.serviceProtocols {
		if RouteHasServiceProtocolSupport(route, providers, serviceProtocol) {
			continue
		}
		return NewValidationError("route %s: service_protocols includes %s but no route upstream/provider supports it", prefix, serviceProtocol)
	}
	return nil
}

func validateRouteAPIKeys(prefix string, keys map[string]SecretString) error {
	seenValues := make(map[string]string, len(keys))
	for name, key := range keys {
		if strings.TrimSpace(name) == "" {
			return NewValidationError("route %s api_keys: key name is required", prefix)
		}
		value := key.Value()
		if strings.TrimSpace(value) == "" {
			return NewValidationError("route %s api_keys[%q]: key value is required", prefix, name)
		}
		if previous, exists := seenValues[value]; exists {
			return NewValidationError("route %s api_keys[%q]: duplicate key value already used by %q", prefix, name, previous)
		}
		seenValues[value] = name
	}
	return nil
}

func (c *ConfigStruct) validateExactRouteModel(prefix string, route *RouteConfig, modelName string, modelCfg *ExactRouteModelConfig) error {
	if modelCfg == nil {
		return NewValidationError("route %s exact model %q: config is required", prefix, modelName)
	}
	if hasWildcardPattern(modelName) {
		return NewValidationError("route %s exact model %q: exact model name cannot contain *", prefix, modelName)
	}
	if route.SupportsServiceProtocol(RouteProtocolResponsesStateful) && len(modelCfg.Upstreams) != 1 {
		return NewValidationError("route %s exact model %q: exactly one upstream is required for protocol %s", prefix, modelName, route.Protocol)
	}
	if len(modelCfg.Upstreams) == 0 {
		return NewValidationError("route %s exact model %q: upstreams is required", prefix, modelName)
	}
	promptEnabled := routeModelPromptEnabled(modelCfg.PromptEnabled, modelCfg.SystemPrompt)
	compiled := &CompiledRouteModel{
		Key:           modelName,
		Pattern:       modelName,
		PublicModel:   modelName,
		PromptEnabled: promptEnabled,
		Wildcard:      false,
		Specificity:   buildPatternSpecificity(modelName),
	}
	if promptEnabled {
		compiled.SystemPrompt = modelCfg.SystemPrompt
	}
	for idx, upstream := range modelCfg.Upstreams {
		if upstream == nil {
			return NewValidationError("route %s exact model %q upstreams[%d]: config is required", prefix, modelName, idx)
		}
		if upstream.Provider == "" {
			return NewValidationError("route %s exact model %q upstreams[%d]: provider is required", prefix, modelName, idx)
		}
		if upstream.Model == "" {
			return NewValidationError("route %s exact model %q upstreams[%d]: model is required", prefix, modelName, idx)
		}
		prov, exists := c.Provider[upstream.Provider]
		if !exists {
			return NewValidationError("route %s exact model %q upstreams[%d]: unknown provider %s", prefix, modelName, idx, upstream.Provider)
		}
		if !ProviderSupportsAnyServiceProtocol(prov, route.serviceProtocols) {
			return NewValidationError("route %s exact model %q upstreams[%d]: provider %s does not support any route service protocol %v", prefix, modelName, idx, upstream.Provider, route.serviceProtocols)
		}
		if route.SupportsServiceProtocol(RouteProtocolResponsesStateful) && prov.ResponsesToChat && ProviderSupportsServiceProtocol(prov, RouteProtocolResponsesStateless) {
			return NewValidationError("route %s exact model %q upstreams[%d]: provider %s enables responses_to_chat and cannot back route protocol %s", prefix, modelName, idx, upstream.Provider, route.Protocol)
		}
		compiled.Upstreams = append(compiled.Upstreams, CompiledRouteUpstream{
			Provider:      upstream.Provider,
			UpstreamModel: upstream.Model,
			RenameModel:   upstream.Model != modelName,
		})
	}
	if !compiledRouteModelSupportsServiceProtocol(compiled, c.Provider, route.Protocol) {
		return NewValidationError("route %s exact model %q: at least one upstream must support route protocol %s", prefix, modelName, route.Protocol)
	}
	route.exactModels[modelName] = compiled
	return nil
}

func (c *ConfigStruct) validateWildcardRouteModel(prefix string, route *RouteConfig, pattern string, modelCfg *WildcardRouteModelConfig) error {
	if modelCfg == nil {
		return NewValidationError("route %s wildcard model %q: config is required", prefix, pattern)
	}
	if !hasWildcardPattern(pattern) {
		return NewValidationError("route %s wildcard model %q: pattern must contain *", prefix, pattern)
	}
	if route.SupportsServiceProtocol(RouteProtocolResponsesStateful) && len(modelCfg.Providers) != 1 {
		return NewValidationError("route %s wildcard model %q: exactly one provider is required for protocol %s", prefix, pattern, route.Protocol)
	}
	if len(modelCfg.Providers) == 0 {
		return NewValidationError("route %s wildcard model %q: providers is required", prefix, pattern)
	}

	promptEnabled := routeModelPromptEnabled(modelCfg.PromptEnabled, modelCfg.SystemPrompt)
	compiled := &CompiledRouteModel{
		Key:           pattern,
		Pattern:       pattern,
		PublicModel:   pattern,
		PromptEnabled: promptEnabled,
		Wildcard:      true,
		Specificity:   buildPatternSpecificity(pattern),
	}
	if promptEnabled {
		compiled.SystemPrompt = modelCfg.SystemPrompt
	}
	for idx, provName := range modelCfg.Providers {
		prov, exists := c.Provider[provName]
		if !exists {
			return NewValidationError("route %s wildcard model %q providers[%d]: unknown provider %s", prefix, pattern, idx, provName)
		}
		if !ProviderSupportsAnyServiceProtocol(prov, route.serviceProtocols) {
			return NewValidationError("route %s wildcard model %q providers[%d]: provider %s does not support any route service protocol %v", prefix, pattern, idx, provName, route.serviceProtocols)
		}
		if route.SupportsServiceProtocol(RouteProtocolResponsesStateful) && prov.ResponsesToChat && ProviderSupportsServiceProtocol(prov, RouteProtocolResponsesStateless) {
			return NewValidationError("route %s wildcard model %q providers[%d]: provider %s enables responses_to_chat and cannot back route protocol %s", prefix, pattern, idx, provName, route.Protocol)
		}
		compiled.Upstreams = append(compiled.Upstreams, CompiledRouteUpstream{
			Provider:      provName,
			UpstreamModel: "",
		})
	}
	if !compiledRouteModelSupportsServiceProtocol(compiled, c.Provider, route.Protocol) {
		return NewValidationError("route %s wildcard model %q: at least one provider must support route protocol %s", prefix, pattern, route.Protocol)
	}
	route.wildcards = append(route.wildcards, compiled)
	return nil
}

func compiledRouteModelSupportsServiceProtocol(compiled *CompiledRouteModel, providers map[string]*ProviderConfig, serviceProtocol string) bool {
	if compiled == nil || serviceProtocol == "" {
		return false
	}
	for _, upstream := range compiled.Upstreams {
		if ProviderSupportsServiceProtocol(providers[upstream.Provider], serviceProtocol) {
			return true
		}
	}
	return false
}

func validateConfiguredProtocolName(prefix, modelName, protocol string) error {
	protocol = normalizeRouteProtocol(protocol)
	switch protocol {
	case RouteProtocolChat, RouteProtocolResponsesStateless, RouteProtocolResponsesStateful, RouteProtocolAnthropic:
		return nil
	default:
		if modelName == "" {
			return NewValidationError("route %s: invalid protocol %q (must be chat/responses_stateless/responses_stateful/anthropic)", prefix, protocol)
		}
		return NewValidationError("route %s model %q: invalid protocol %q (must be chat/responses_stateless/responses_stateful/anthropic)", prefix, modelName, protocol)
	}
}

func routeModelPromptEnabled(explicit *bool, prompt string) bool {
	if explicit != nil {
		return *explicit
	}
	return strings.TrimSpace(prompt) != ""
}

// validateHookConfig validates a single HookConfig and parses its timeout.
func validateHookConfig(ctx string, hook *HookConfig, webhooks map[string]*WebhookConfig, routes map[string]*RouteConfig) error {
	switch hook.Type {
	case "exec":
		if hook.Command == "" {
			return NewValidationError("%s: command is required for exec type", ctx)
		}
	case "ai":
		if hook.Route == "" {
			return NewValidationError("%s: route is required for ai type", ctx)
		}
		routeCfg, ok := routes[hook.Route]
		if !ok {
			return NewValidationError("%s: route %q does not exist", ctx, hook.Route)
		}
		if routeCfg.ConfiguredProtocol() != RouteProtocolChat {
			return NewValidationError("%s: route %q must use protocol %q for ai type", ctx, hook.Route, RouteProtocolChat)
		}
		if len(routeCfg.Hooks) > 0 {
			return NewValidationError("%s: route %q cannot define hooks for ai type", ctx, hook.Route)
		}
		if hook.Model == "" {
			return NewValidationError("%s: model is required for ai type", ctx)
		}
		if hook.Prompt == "" {
			return NewValidationError("%s: prompt is required for ai type", ctx)
		}
	case "http":
		if err := resolveWebhookReference(ctx, hook.Webhook, webhooks, func(cfg *WebhookConfig) {
			hook.WebhookCfg = cfg
		}); err != nil {
			return err
		}
	default:
		return NewValidationError("%s: invalid type %q (must be exec, ai or http)", ctx, hook.Type)
	}

	switch hook.When {
	case "block", "pre":
		hook.When = "block"
	case "async", "post":
		hook.When = "async"
	default:
		return NewValidationError("%s: invalid when %q (must be block or async)", ctx, hook.When)
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

func resolveWebhookReference(ctx, name string, webhooks map[string]*WebhookConfig, set func(*WebhookConfig)) error {
	if name == "" {
		return NewValidationError("%s: webhook is required for http type", ctx)
	}

	webhookCfg, ok := webhooks[name]
	if !ok {
		return NewValidationError("%s: unknown webhook %q", ctx, name)
	}

	set(webhookCfg)
	return nil
}

func applyProviderDefaults(prov *ProviderConfig) {
	switch prov.Protocol {
	case ProviderProtocolCopilot:
		if prov.URL == "" {
			prov.URL = "https://api.githubcopilot.com"
		}
		if prov.ConfigDir == "" {
			prov.ConfigDir = "~/.config/github-copilot"
		}
	}
}

func expandHomeDir(pathValue string) (string, error) {
	if !strings.HasPrefix(pathValue, "~/") {
		return pathValue, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}

	return filepath.Join(home, pathValue[2:]), nil
}

func validateHTTPURL(ctx, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return NewValidationError("%s: invalid url %s: %v", ctx, rawURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return NewValidationError("%s: url must be an absolute http/https URL, got %q", ctx, rawURL)
	}
	if parsed.Host == "" {
		return NewValidationError("%s: url must include host, got %q", ctx, rawURL)
	}
	return nil
}

func validateProxyURL(ctx, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return NewValidationError("%s: invalid proxy URL %s: %v", ctx, rawURL, err)
	}
	if !supportedProxySchemes[parsed.Scheme] {
		return NewValidationError("%s: proxy URL scheme must be one of http/https/socks5/socks5h, got %q", ctx, parsed.Scheme)
	}
	if parsed.Host == "" {
		return NewValidationError("%s: proxy URL must include host, got %q", ctx, rawURL)
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
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}
