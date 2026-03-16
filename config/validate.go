package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/wweir/warden/pkg/provider"
)

var supportedProxySchemes = map[string]bool{
	"http":    true,
	"https":   true,
	"socks5":  true,
	"socks5h": true,
}

// Validate checks configuration validity.
func (c *ConfigStruct) Validate() error {
	if err := c.validateLogConfig(); err != nil {
		return err
	}
	if err := c.validateWebhookConfig(); err != nil {
		return err
	}
	if err := c.validateSSHConfig(); err != nil {
		return err
	}
	if err := c.validateRoutePrefixes(); err != nil {
		return err
	}
	if err := c.validateProviderConfig(); err != nil {
		return err
	}
	if err := c.validateRouteConfig(); err != nil {
		return err
	}
	if err := c.validateMCPConfig(); err != nil {
		return err
	}
	return nil
}

func (c *ConfigStruct) validateLogConfig() error {
	if c.Log == nil {
		return nil
	}

	for i, target := range c.Log.Targets {
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
		if webhook.URL == "" {
			return NewValidationError("webhook %s: url is required", name)
		}
		if err := validateHTTPURL("webhook "+name, webhook.URL); err != nil {
			return err
		}
		if webhook.Timeout == "" {
			continue
		}
		if _, err := time.ParseDuration(webhook.Timeout); err != nil {
			return NewValidationError("webhook %s: invalid timeout %s: %v", name, webhook.Timeout, err)
		}
	}

	return nil
}

func (c *ConfigStruct) validateSSHConfig() error {
	for name, sshCfg := range c.SSH {
		sshCfg.Name = name
		if sshCfg.Host == "" {
			return NewValidationError("ssh %s: host is required", name)
		}

		identityFile, err := expandHomeDir(sshCfg.IdentityFile)
		if err != nil {
			return NewValidationError("ssh %s: %v", name, err)
		}
		sshCfg.IdentityFile = identityFile
	}

	return nil
}

func (c *ConfigStruct) validateRoutePrefixes() error {
	for prefix, route := range c.Route {
		if prefix == "" || prefix[0] != '/' {
			return NewValidationError("route prefix must start with /: %s", prefix)
		}

		route.Prefix = prefix
		switch route.Protocol {
		case "", RouteProtocolChat, RouteProtocolResponses, RouteProtocolAnthropic:
		default:
			return NewValidationError("route %s: invalid protocol %q (must be chat/responses/anthropic)", prefix, route.Protocol)
		}
	}

	return nil
}

func (c *ConfigStruct) validateProviderConfig() error {
	for name, prov := range c.Provider {
		prov.Name = name
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
		case "openai", "anthropic", "ollama":
		case "qwen", "copilot":
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

		if prov.ChatToResponses && prov.Protocol != "openai" {
			return NewValidationError("provider %s: chat_to_responses requires protocol 'openai', got %q", name, prov.Protocol)
		}
		if prov.ResponsesToChat && prov.Protocol != "openai" {
			return NewValidationError("provider %s: responses_to_chat requires protocol 'openai', got %q", name, prov.Protocol)
		}
	}

	return nil
}

func (c *ConfigStruct) validateRouteConfig() error {
	for prefix, route := range c.Route {
		if len(route.Models) == 0 && len(route.Providers) > 0 {
			route.Models = compileLegacyRouteModels(route)
		}
		route.exactModels = make(map[string]*CompiledRouteModel)
		route.wildcards = nil

		if len(route.Models) == 0 {
			return NewValidationError("route %s: models is required", prefix)
		}

		for _, toolName := range route.Tools {
			if _, exists := c.MCP[toolName]; !exists {
				return NewValidationError("route %s: unknown MCP tool %s", prefix, toolName)
			}
		}

		route.EnabledTools = make(map[string]bool, len(route.Tools))
		for _, toolName := range route.Tools {
			route.EnabledTools[toolName] = true
		}

		for i, rule := range route.Hooks {
			if rule.Match == "" {
				return NewValidationError("route %s hooks[%d]: match is required", prefix, i)
			}
			if err := validateHookConfig(fmt.Sprintf("route %s hooks[%d]", prefix, i), &rule.Hook, c.Webhook); err != nil {
				return err
			}
		}

		for modelName, modelCfg := range route.Models {
			if err := c.validateRouteModel(prefix, route, modelName, modelCfg); err != nil {
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
					return NewValidationError("route %s: wildcard models %q and %q have ambiguous precedence", prefix, a.Pattern, b.Pattern)
				}
			}
		}
	}

	return nil
}

func (c *ConfigStruct) validateRouteModel(prefix string, route *RouteConfig, modelName string, modelCfg *RouteModelConfig) error {
	if modelCfg == nil {
		return NewValidationError("route %s model %q: config is required", prefix, modelName)
	}

	isWildcard := hasWildcardPattern(modelName)
	switch {
	case isWildcard && len(modelCfg.Upstreams) > 0:
		return NewValidationError("route %s model %q: wildcard model cannot define upstreams", prefix, modelName)
	case !isWildcard && len(modelCfg.Providers) > 0 && len(modelCfg.Upstreams) > 0:
		return NewValidationError("route %s model %q: exact model cannot define both providers and upstreams", prefix, modelName)
	}

	compiled := &CompiledRouteModel{
		Key:          modelName,
		Pattern:      modelName,
		PublicModel:  modelName,
		SystemPrompt: modelCfg.SystemPrompt,
		Wildcard:     isWildcard,
		Specificity:  buildPatternSpecificity(modelName),
	}

	if isWildcard {
		return c.validateWildcardRouteModel(prefix, route, modelName, modelCfg, compiled)
	}

	return c.validateExactRouteModel(prefix, route, modelName, modelCfg, compiled)
}

func (c *ConfigStruct) validateWildcardRouteModel(prefix string, route *RouteConfig, modelName string, modelCfg *RouteModelConfig, compiled *CompiledRouteModel) error {
	if len(modelCfg.Providers) == 0 {
		return NewValidationError("route %s model %q: wildcard model requires providers", prefix, modelName)
	}

	for _, provName := range modelCfg.Providers {
		prov, exists := c.Provider[provName]
		if !exists {
			return NewValidationError("route %s model %q: unknown provider %s", prefix, modelName, provName)
		}
		if route.Protocol != "" && !ProviderSupportsRouteProtocol(prov.Protocol, route.Protocol) {
			return NewValidationError("route %s model %q: provider %s does not support route protocol %s", prefix, modelName, provName, route.Protocol)
		}
		compiled.Upstreams = append(compiled.Upstreams, CompiledRouteUpstream{
			Provider:      provName,
			UpstreamModel: "",
		})
	}

	route.wildcards = append(route.wildcards, compiled)
	return nil
}

func (c *ConfigStruct) validateExactRouteModel(prefix string, route *RouteConfig, modelName string, modelCfg *RouteModelConfig, compiled *CompiledRouteModel) error {
	upstreams := modelCfg.Upstreams
	if len(upstreams) == 0 {
		if len(modelCfg.Providers) == 0 {
			return NewValidationError("route %s model %q: exact model requires upstreams", prefix, modelName)
		}
		for _, provName := range modelCfg.Providers {
			upstreams = append(upstreams, &RouteUpstreamConfig{Provider: provName, Model: modelName})
		}
	}

	for idx, upstream := range upstreams {
		if upstream == nil {
			return NewValidationError("route %s model %q upstreams[%d]: config is required", prefix, modelName, idx)
		}
		if upstream.Provider == "" {
			return NewValidationError("route %s model %q upstreams[%d]: provider is required", prefix, modelName, idx)
		}
		if upstream.Model == "" {
			return NewValidationError("route %s model %q upstreams[%d]: model is required", prefix, modelName, idx)
		}
		prov, exists := c.Provider[upstream.Provider]
		if !exists {
			return NewValidationError("route %s model %q upstreams[%d]: unknown provider %s", prefix, modelName, idx, upstream.Provider)
		}
		if route.Protocol != "" && !ProviderSupportsRouteProtocol(prov.Protocol, route.Protocol) {
			return NewValidationError("route %s model %q upstreams[%d]: provider %s does not support route protocol %s", prefix, modelName, idx, upstream.Provider, route.Protocol)
		}
		compiled.Upstreams = append(compiled.Upstreams, CompiledRouteUpstream{
			Provider:      upstream.Provider,
			UpstreamModel: upstream.Model,
			RenameModel:   upstream.Model != modelName,
		})
	}

	route.exactModels[modelName] = compiled
	return nil
}

func (c *ConfigStruct) validateMCPConfig() error {
	for name, mcp := range c.MCP {
		mcp.Name = name
		if mcp.Command == "" {
			return NewValidationError("mcp %s: command is required", name)
		}
		if mcp.SSH == "" {
			continue
		}

		sshCfg, ok := c.SSH[mcp.SSH]
		if !ok {
			return NewValidationError("mcp %s: unknown ssh config %q", name, mcp.SSH)
		}
		mcp.SSHCfg = sshCfg
	}

	return nil
}

// validateHookConfig validates a single HookConfig and parses its timeout.
func validateHookConfig(ctx string, hook *HookConfig, webhooks map[string]*WebhookConfig) error {
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
