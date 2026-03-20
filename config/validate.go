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
	if err := c.validateRoutePrefixes(); err != nil {
		return err
	}
	if err := c.validateProviderConfig(); err != nil {
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
		prov.Family = normalizeProviderProtocol(prov.Family)
		prov.Protocol = normalizeProviderProtocol(prov.Protocol)
		if prov.Family != "" && prov.Protocol != "" && prov.Family != prov.Protocol {
			return NewValidationError("provider %s: family %q conflicts with protocol %q", name, prov.Family, prov.Protocol)
		}
		if prov.Protocol == "" {
			prov.Protocol = prov.Family
		}
		if prov.Protocol == "" {
			return NewValidationError("provider %s: family is required", name)
		}
		prov.Family = prov.Protocol
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
		case ProviderProtocolOpenAI, ProviderProtocolAnthropic, ProviderProtocolOllama:
		case ProviderProtocolQwen, ProviderProtocolCopilot:
			if prov.APIKey.Value() == "" {
				if p := provider.Get(prov.Protocol); p != nil {
					if err := p.CheckCredsReadable(prov.ConfigDir); err != nil {
						return NewValidationError("provider %s: %v", name, err)
					}
				}
			}
		default:
			return NewValidationError("provider %s: invalid protocol %s (must be openai/anthropic/ollama/qwen/copilot)", name, prov.Protocol)
		}

		if err := validateProviderProtocolFilters(name, prov); err != nil {
			return err
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

		if err := validateConfiguredProtocolName(prefix, "", route.Protocol); err != nil {
			return err
		}
		route.serviceProtocols = SupportedServiceProtocolsForConfiguredProtocol(route.Protocol)
		if len(route.serviceProtocols) == 0 {
			return NewValidationError("route %s: protocol is required", prefix)
		}

		if len(route.ExactModels) == 0 && len(route.WildcardModels) == 0 {
			return NewValidationError("route %s: exact_models or wildcard_models is required", prefix)
		}

		for i, rule := range route.Hooks {
			if rule.Match == "" {
				return NewValidationError("route %s hooks[%d]: match is required", prefix, i)
			}
			if err := validateHookConfig(fmt.Sprintf("route %s hooks[%d]", prefix, i), &rule.Hook, c.Webhook); err != nil {
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
	if route.Protocol == RouteProtocolResponsesStateful && len(modelCfg.Upstreams) != 1 {
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
		if !ProviderSupportsConfiguredProtocol(prov, route.Protocol) {
			return NewValidationError("route %s exact model %q upstreams[%d]: provider %s does not support route protocol %s", prefix, modelName, idx, upstream.Provider, route.Protocol)
		}
		if route.Protocol == RouteProtocolResponsesStateful && prov.ResponsesToChat {
			return NewValidationError("route %s exact model %q upstreams[%d]: provider %s enables responses_to_chat and cannot back route protocol %s", prefix, modelName, idx, upstream.Provider, route.Protocol)
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

func (c *ConfigStruct) validateWildcardRouteModel(prefix string, route *RouteConfig, pattern string, modelCfg *WildcardRouteModelConfig) error {
	if modelCfg == nil {
		return NewValidationError("route %s wildcard model %q: config is required", prefix, pattern)
	}
	if !hasWildcardPattern(pattern) {
		return NewValidationError("route %s wildcard model %q: pattern must contain *", prefix, pattern)
	}
	if route.Protocol == RouteProtocolResponsesStateful && len(modelCfg.Providers) != 1 {
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
		if !ProviderSupportsConfiguredProtocol(prov, route.Protocol) {
			return NewValidationError("route %s wildcard model %q providers[%d]: provider %s does not support route protocol %s", prefix, pattern, idx, provName, route.Protocol)
		}
		if route.Protocol == RouteProtocolResponsesStateful && prov.ResponsesToChat {
			return NewValidationError("route %s wildcard model %q providers[%d]: provider %s enables responses_to_chat and cannot back route protocol %s", prefix, pattern, idx, provName, route.Protocol)
		}
		compiled.Upstreams = append(compiled.Upstreams, CompiledRouteUpstream{
			Provider:      provName,
			UpstreamModel: "",
		})
	}
	route.wildcards = append(route.wildcards, compiled)
	return nil
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

func validateProviderProtocolFilters(name string, prov *ProviderConfig) error {
	candidates := CandidateRouteProtocols(prov)
	candidateSet := make(map[string]bool, len(candidates))
	for _, protocol := range candidates {
		candidateSet[protocol] = true
	}

	normalizeList := func(field string, values []string) ([]string, error) {
		if len(values) == 0 {
			return nil, nil
		}
		normalized := make([]string, 0, len(values))
		seen := make(map[string]bool, len(values))
		for idx, value := range values {
			protocol := normalizeRouteProtocol(value)
			if err := validateConfiguredProtocolName("", "", protocol); err != nil {
				return nil, NewValidationError("provider %s %s[%d]: invalid protocol %q (must be chat/responses_stateless/responses_stateful/anthropic)", name, field, idx, value)
			}
			if !candidateSet[protocol] {
				return nil, NewValidationError("provider %s %s[%d]: protocol %q is not supported by provider family %s", name, field, idx, protocol, prov.Protocol)
			}
			if seen[protocol] {
				continue
			}
			seen[protocol] = true
			normalized = append(normalized, protocol)
		}
		return normalized, nil
	}

	var err error
	prov.EnabledProtocols, err = normalizeList("enabled_protocols", prov.EnabledProtocols)
	if err != nil {
		return err
	}
	prov.DisabledProtocols, err = normalizeList("disabled_protocols", prov.DisabledProtocols)
	if err != nil {
		return err
	}

	disabled := make(map[string]bool, len(prov.DisabledProtocols))
	for _, protocol := range prov.DisabledProtocols {
		disabled[protocol] = true
	}
	for _, protocol := range prov.EnabledProtocols {
		if disabled[protocol] {
			return NewValidationError("provider %s: protocol %q cannot appear in both enabled_protocols and disabled_protocols", name, protocol)
		}
	}

	if len(SupportedRouteProtocols(prov)) == 0 {
		return NewValidationError("provider %s: enabled_protocols/disabled_protocols remove all compatible route protocols", name)
	}

	return nil
}

func routeModelPromptEnabled(explicit *bool, prompt string) bool {
	if explicit != nil {
		return *explicit
	}
	return strings.TrimSpace(prompt) != ""
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
	case ProviderProtocolQwen:
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
