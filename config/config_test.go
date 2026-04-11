package config

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func testExactModel(protocol string, upstreams ...*RouteUpstreamConfig) *ExactRouteModelConfig {
	_ = protocol

	return &ExactRouteModelConfig{
		Upstreams: upstreams,
	}
}

func testWildcardModel(protocol string, providers ...string) *WildcardRouteModelConfig {
	_ = protocol

	return &WildcardRouteModelConfig{
		Providers: providers,
	}
}

func TestProviderConfig_HTTPClient_Caching(t *testing.T) {
	prov := &ProviderConfig{
		Name:    "test",
		URL:     "http://localhost:8080",
		Timeout: "30s",
	}

	// Get client for default timeout
	client1 := prov.HTTPClient(0)
	client2 := prov.HTTPClient(0)

	// Should return same cached instance
	if client1 != client2 {
		t.Error("HTTPClient should return cached instance for same timeout")
	}

	// Get client for different timeout
	client3 := prov.HTTPClient(60 * time.Second)
	if client1 == client3 {
		t.Error("HTTPClient should return different instance for different timeout")
	}

	// Get client for same custom timeout again
	client4 := prov.HTTPClient(60 * time.Second)
	if client3 != client4 {
		t.Error("HTTPClient should cache instance for custom timeout")
	}
}

func TestValidateToolHookHTTPType(t *testing.T) {
	cfg := &ConfigStruct{
		Webhook: map[string]*WebhookConfig{
			"audit": {URL: "http://127.0.0.1:8080/hook"},
		},
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: RouteProtocolChat,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": testExactModel(RouteProtocolChat, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
				Hooks: []*HookRuleConfig{{
					Match: "*",
					Hook:  HookConfig{Type: "http", When: "pre", Timeout: "3s", Webhook: "audit"},
				}},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if cfg.Route["/test"].Hooks[0].Hook.WebhookCfg == nil {
		t.Fatal("expected webhook config to be resolved")
	}
}

func TestValidateToolHookHTTPTypeUnknownWebhook(t *testing.T) {
	cfg := &ConfigStruct{
		Webhook: map[string]*WebhookConfig{
			"audit": {URL: "http://127.0.0.1:8080/hook"},
		},
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: RouteProtocolChat,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": testExactModel(RouteProtocolChat, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
				Hooks: []*HookRuleConfig{{
					Match: "*",
					Hook:  HookConfig{Type: "http", When: "pre", Timeout: "3s", Webhook: "missing"},
				}},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "unknown webhook") {
		t.Fatalf("expected unknown webhook error, got %v", err)
	}
}

func TestValidateToolHookHTTPTypeMissingWebhook(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: RouteProtocolChat,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": testExactModel(RouteProtocolChat, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
				Hooks: []*HookRuleConfig{{
					Match: "*",
					Hook:  HookConfig{Type: "http", When: "pre", Timeout: "3s"},
				}},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "webhook is required") {
		t.Fatalf("expected webhook required error, got %v", err)
	}
}

func TestValidateResponsesToChatRequiresOpenAI(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"anthropic": {
				URL:             "https://api.anthropic.com/v1",
				Protocol:        "anthropic",
				ResponsesToChat: true,
			},
		},
		Route: map[string]*RouteConfig{
			"/anthropic": {
				Protocol: RouteProtocolAnthropic,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": testWildcardModel(RouteProtocolAnthropic, "anthropic"),
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "responses_to_chat requires protocol 'openai'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAnthropicToChatRequiresOpenAI(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"anthropic": {
				URL:             "https://api.anthropic.com/v1",
				Protocol:        "anthropic",
				AnthropicToChat: true,
			},
		},
		Route: map[string]*RouteConfig{
			"/anthropic": {
				Protocol: RouteProtocolAnthropic,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": testWildcardModel(RouteProtocolAnthropic, "anthropic"),
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "anthropic_to_chat requires protocol 'openai'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAnthropicRouteAllowsOpenAIProviderWhenAnthropicToChatEnabled(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {
				URL:             "https://api.openai.com/v1",
				Protocol:        "openai",
				AnthropicToChat: true,
			},
		},
		Route: map[string]*RouteConfig{
			"/anthropic": {
				Protocol: RouteProtocolAnthropic,
				ExactModels: map[string]*ExactRouteModelConfig{
					"claude-compatible": testExactModel(RouteProtocolAnthropic, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateStatefulRouteRejectsResponsesToChatProvider(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {
				URL:             "https://api.openai.com/v1",
				Protocol:        "openai",
				ResponsesToChat: true,
			},
		},
		Route: map[string]*RouteConfig{
			"/openai": {
				Protocol: RouteProtocolResponsesStateful,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": testExactModel(RouteProtocolResponsesStateful, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "enables responses_to_chat and cannot back route protocol responses_stateful") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateProviderRequiresAbsoluteURL(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: RouteProtocolChat,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": testExactModel(RouteProtocolChat, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "absolute http/https URL") {
		t.Fatalf("expected provider absolute URL error, got %v", err)
	}
}

func TestValidateWebhookRequiresAbsoluteURL(t *testing.T) {
	cfg := &ConfigStruct{
		Webhook: map[string]*WebhookConfig{
			"audit": {URL: "/hook"},
		},
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: RouteProtocolChat,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": testExactModel(RouteProtocolChat, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "absolute http/https URL") {
		t.Fatalf("expected webhook absolute URL error, got %v", err)
	}
}

func TestValidateProviderRejectsUnsupportedProxyScheme(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {
				URL:      "https://api.openai.com/v1",
				Protocol: "openai",
				Proxy:    "ftp://proxy.example.com:8080",
			},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: RouteProtocolChat,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": testExactModel(RouteProtocolChat, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "proxy URL scheme must be one of") {
		t.Fatalf("expected proxy scheme error, got %v", err)
	}
}

func TestValidateRouteConfigExplicitModelSections(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: RouteProtocolChat,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": testExactModel(RouteProtocolChat, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4.1"}),
				},
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"gpt-*": testWildcardModel(RouteProtocolChat, "openai"),
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if cfg.Route["/test"].MatchModel("gpt-4.2") == nil {
		t.Fatal("expected wildcard model to be compiled")
	}
}

func TestValidateRouteConfigPromptEnabledExplicitFalseDisablesInjection(t *testing.T) {
	disabled := false
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: RouteProtocolChat,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": {
						PromptEnabled: &disabled,
						SystemPrompt:  "should not be injected",
						Upstreams:     []*RouteUpstreamConfig{{Provider: "openai", Model: "gpt-4o"}},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	matched := cfg.Route["/test"].MatchModel("gpt-4o")
	if matched == nil {
		t.Fatal("expected exact model to be compiled")
	}
	if matched.PromptEnabled {
		t.Fatal("PromptEnabled = true, want false")
	}
	if matched.SystemPrompt != "" {
		t.Fatalf("SystemPrompt = %q, want empty", matched.SystemPrompt)
	}
}

func TestValidateRouteConfigPromptEnabledLegacyInference(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"gpt-*": {
						SystemPrompt: "legacy prompt",
						Providers:    []string{"openai"},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	matched := cfg.Route["/test"].MatchModel("gpt-4o")
	if matched == nil {
		t.Fatal("expected wildcard model to be compiled")
	}
	if !matched.PromptEnabled {
		t.Fatal("PromptEnabled = false, want true for legacy non-empty system_prompt")
	}
	if matched.SystemPrompt != "legacy prompt" {
		t.Fatalf("SystemPrompt = %q, want legacy prompt", matched.SystemPrompt)
	}
}

func TestSupportedRouteProtocolsByProviderProtocol(t *testing.T) {
	tests := []struct {
		name     string
		provider *ProviderConfig
		want     []string
	}{
		{
			name:     "openai compatible",
			provider: &ProviderConfig{Protocol: "openai"},
			want: []string{
				RouteProtocolChat,
				RouteProtocolResponsesStateless,
				RouteProtocolResponsesStateful,
			},
		},
		{
			name:     "qwen",
			provider: &ProviderConfig{Protocol: "qwen"},
			want:     []string{RouteProtocolChat},
		},
		{
			name:     "copilot",
			provider: &ProviderConfig{Protocol: "copilot"},
			want:     []string{RouteProtocolChat},
		},
		{
			name:     "anthropic",
			provider: &ProviderConfig{Protocol: "anthropic"},
			want:     []string{RouteProtocolChat, RouteProtocolAnthropic},
		},
		{
			name:     "openai anthropic_to_chat",
			provider: &ProviderConfig{Protocol: "openai", AnthropicToChat: true},
			want: []string{
				RouteProtocolChat,
				RouteProtocolResponsesStateless,
				RouteProtocolResponsesStateful,
				RouteProtocolAnthropic,
			},
		},
		{
			name:     "ollama",
			provider: &ProviderConfig{Protocol: "ollama"},
			want:     []string{RouteProtocolChat},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SupportedRouteProtocols(tt.provider)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("SupportedRouteProtocols() = %v, want %v", got, tt.want)
			}
		})
	}
}


func TestValidateProviderFamilyAlias(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"primary": {
				Family: "openai",
				URL:    "https://api.openai.com/v1",
				APIKey: "test-key",
			},
		},
		Route: map[string]*RouteConfig{
			"/responses": {
				Protocol: RouteProtocolResponsesStateless,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"primary"}},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	prov := cfg.Provider["primary"]
	if prov.Protocol != ProviderProtocolOpenAI {
		t.Fatalf("provider protocol = %q, want %q", prov.Protocol, ProviderProtocolOpenAI)
	}
	if prov.Family != ProviderProtocolOpenAI {
		t.Fatalf("provider family = %q, want %q", prov.Family, ProviderProtocolOpenAI)
	}
}

func TestValidateProviderLegacyProtocolAliasStillWorks(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"primary": {
				Protocol: "openai",
				URL:      "https://api.openai.com/v1",
				APIKey:   "test-key",
			},
		},
		Route: map[string]*RouteConfig{
			"/responses": {
				Protocol: RouteProtocolResponsesStateless,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"primary"}},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	prov := cfg.Provider["primary"]
	if prov.Protocol != ProviderProtocolOpenAI {
		t.Fatalf("provider protocol = %q, want %q", prov.Protocol, ProviderProtocolOpenAI)
	}
	if prov.Family != ProviderProtocolOpenAI {
		t.Fatalf("provider family = %q, want %q", prov.Family, ProviderProtocolOpenAI)
	}
}

func TestValidateProviderRequiresExplicitFamilyOrProtocol(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"primary": {
				URL:    "https://api.openai.com/v1",
				APIKey: "test-key",
			},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"primary"}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "family is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateProviderFamilyAppliesDefaults(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"qwen": {
				Family:  ProviderProtocolQwen,
				APIKey:  "test-key",
				Timeout: "30s",
			},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {
						Providers: []string{"qwen"},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	prov := cfg.Provider["qwen"]
	if prov.Protocol != ProviderProtocolQwen {
		t.Fatalf("provider protocol = %q, want %q", prov.Protocol, ProviderProtocolQwen)
	}
	if prov.URL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("provider URL = %q, want qwen default", prov.URL)
	}
	if prov.ConfigDir == "" {
		t.Fatal("expected qwen default config_dir to be applied")
	}
}

func TestValidateRouteConfigRequiresRouteProtocol(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": {},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), `invalid protocol ""`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRouteConfigRejectsInvalidProtocolName(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: "responses",
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": {
						Upstreams: []*RouteUpstreamConfig{{Provider: "openai", Model: "gpt-4o"}},
					},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "responses_stateless/responses_stateful") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsTopLevelAPIKeys(t *testing.T) {
	cfg := &ConfigStruct{
		APIKeys: map[string]SecretString{
			"legacy": "secret",
		},
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: RouteProtocolChat,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": {
						Upstreams: []*RouteUpstreamConfig{{Provider: "openai", Model: "gpt-4o"}},
					},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "top-level api_keys is deprecated") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAcceptsRouteAPIKeys(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: RouteProtocolChat,
				APIKeys: map[string]SecretString{
					"client": "secret",
				},
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": {
						Upstreams: []*RouteUpstreamConfig{{Provider: "openai", Model: "gpt-4o"}},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
