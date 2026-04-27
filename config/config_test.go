package config

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
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

func TestRouteConfigCloneAPIKeysReturnsIndependentCopy(t *testing.T) {
	route := &RouteConfig{
		APIKeys: map[string]SecretString{
			"cli": "secret",
		},
	}

	cloned := route.CloneAPIKeys()
	cloned["cli"] = "changed"
	cloned["new"] = "another"

	if got := route.APIKeys["cli"].Value(); got != "secret" {
		t.Fatalf("route API key = %q, want secret", got)
	}
	if _, exists := route.APIKeys["new"]; exists {
		t.Fatalf("route API keys unexpectedly mutated: %#v", route.APIKeys)
	}
}

func TestRouteConfigAddAPIKeyRejectsExistingName(t *testing.T) {
	route := &RouteConfig{
		APIKeys: map[string]SecretString{
			"cli": "secret",
		},
	}

	if ok := route.AddAPIKey("cli", "new-secret"); ok {
		t.Fatal("AddAPIKey() = true, want false for duplicate name")
	}
	if got := route.APIKeys["cli"].Value(); got != "secret" {
		t.Fatalf("route API key = %q, want secret", got)
	}
}

func TestValidateRouteAPIKeysRejectsDuplicateValuesPerRoute(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
				Protocol: RouteProtocolChat,
				APIKeys: map[string]SecretString{
					"cli": "shared-secret",
					"sdk": "shared-secret",
				},
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": testExactModel(RouteProtocolChat, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `duplicate key value already used by "`) {
		t.Fatalf("expected duplicate route api key error, got %v", err)
	}
}

func TestRouteConfigMarshalJSONUsesSafeAPIKeySnapshot(t *testing.T) {
	route := &RouteConfig{
		Protocol: RouteProtocolChat,
		APIKeys: map[string]SecretString{
			"cli": "secret",
		},
	}

	data, err := json.Marshal(route)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), `"api_keys":{"cli":"`+EncodeSecret("secret")+`"}`) {
		t.Fatalf("marshal output = %s", data)
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

func TestValidateLogTargetRejectsNilEntry(t *testing.T) {
	cfg := &ConfigStruct{}
	if err := yaml.Unmarshal([]byte("log:\n  targets:\n    - null\n"), cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "log.targets[0]: target is required") {
		t.Fatalf("expected nil target validation error, got %v", err)
	}
}

func TestValidateWebhookRejectsInvalidBodyTemplate(t *testing.T) {
	cfg := &ConfigStruct{
		Webhook: map[string]*WebhookConfig{
			"audit": {
				URL:          "https://example.com/logs",
				BodyTemplate: "{{ if }}",
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "invalid body_template") {
		t.Fatalf("expected invalid body_template error, got %v", err)
	}
}

func TestValidateWebhookRejectsNonPositiveTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
	}{
		{name: "zero", timeout: "0s"},
		{name: "negative", timeout: "-1s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ConfigStruct{
				Webhook: map[string]*WebhookConfig{
					"audit": {
						URL:     "https://example.com/logs",
						Timeout: tt.timeout,
					},
				},
			}

			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), "timeout must be greater than 0") {
				t.Fatalf("expected positive timeout validation error, got %v", err)
			}
		})
	}
}

func TestValidateToolHookAIRequiresExistingChatRouteWithoutAPIKeys(t *testing.T) {
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
					Hook:  HookConfig{Type: "ai", When: "block", Route: "/missing", Model: "gpt-4o-mini", Prompt: "{{.FullName}}"},
				}},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `route "/missing" does not exist`) {
		t.Fatalf("expected missing ai route error, got %v", err)
	}
}

func TestValidateToolHookAIRejectsNonChatRouteAndAllowsProtectedRoute(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
				Protocol: RouteProtocolChat,
				APIKeys:  map[string]SecretString{"hook": "secret"},
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": testExactModel(RouteProtocolChat, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
			"/responses": {
				Protocol: RouteProtocolResponsesStateless,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o-mini": testExactModel(RouteProtocolResponsesStateless, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o-mini"}),
				},
			},
			"/test": {
				Protocol: RouteProtocolChat,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": testExactModel(RouteProtocolChat, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
				Hooks: []*HookRuleConfig{{
					Match: "*",
					Hook:  HookConfig{Type: "ai", When: "block", Route: "/responses", Model: "gpt-4o-mini", Prompt: "{{.FullName}}"},
				}},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `must use protocol "chat"`) {
		t.Fatalf("expected non-chat ai route error, got %v", err)
	}

	cfg.Route["/test"].Hooks[0].Hook.Route = "/chat"
	err = cfg.Validate()
	if err != nil {
		t.Fatalf("expected protected ai route to validate, got %v", err)
	}
}

func TestValidateToolHookAIRejectsRouteWithHooks(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/review": {
				Protocol: RouteProtocolChat,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o-mini": testExactModel(RouteProtocolChat, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o-mini"}),
				},
				Hooks: []*HookRuleConfig{{
					Match: "*",
					Hook:  HookConfig{Type: "http", When: "async", Webhook: "audit"},
				}},
			},
			"/test": {
				Protocol: RouteProtocolChat,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": testExactModel(RouteProtocolChat, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
				Hooks: []*HookRuleConfig{{
					Match: "*",
					Hook:  HookConfig{Type: "ai", When: "block", Route: "/review", Model: "gpt-4o-mini", Prompt: "{{.FullName}}"},
				}},
			},
		},
		Webhook: map[string]*WebhookConfig{
			"audit": {URL: "http://127.0.0.1:8080/hook"},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `cannot define hooks for ai type`) {
		t.Fatalf("expected ai route hooks error, got %v", err)
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

func TestRouteWildcardModelMatchesSlashSeparatedModelIDs(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*":      testWildcardModel(RouteProtocolChat, "openai"),
					"*:free": testWildcardModel(RouteProtocolChat, "openai"),
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	route := cfg.Route["/test"]
	if got := route.MatchModel("nvidia/llama-3.1-8b-instruct"); got == nil || got.Pattern != "*" {
		t.Fatalf("MatchModel(nvidia/llama-3.1-8b-instruct) pattern = %#v, want *", got)
	}
	if got := route.MatchModel("openrouter/model:free"); got == nil || got.Pattern != "*:free" {
		t.Fatalf("MatchModel(openrouter/model:free) pattern = %#v, want *:free", got)
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

func TestSupportedServiceProtocolsByProviderProtocol(t *testing.T) {
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
				ServiceProtocolEmbeddings,
			},
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
				ServiceProtocolEmbeddings,
				RouteProtocolAnthropic,
			},
		},
		{
			name:     "explicit chat only capability",
			provider: &ProviderConfig{Protocol: "openai", ServiceProtocols: []string{RouteProtocolChat}},
			want:     []string{RouteProtocolChat},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SupportedServiceProtocols(tt.provider)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("SupportedServiceProtocols() = %v, want %v", got, tt.want)
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

func TestValidateProviderRejectsRemovedOllamaAlias(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"local": {
				Family: "ollama",
				URL:    "http://localhost:11434/v1",
			},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"local"}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "'ollama' family/protocol has been removed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "service_protocols") {
		t.Fatalf("error %q does not contain migration guidance", err)
	}
}

func TestValidateProviderExplicitServiceProtocolsOverrideDefaults(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"primary": {
				Protocol:         ProviderProtocolOpenAI,
				URL:              "https://api.openai.com/v1",
				APIKey:           "test-key",
				ServiceProtocols: []string{RouteProtocolChat, ServiceProtocolEmbeddings},
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

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	got := SupportedServiceProtocols(cfg.Provider["primary"])
	want := []string{RouteProtocolChat, ServiceProtocolEmbeddings}
	if !slices.Equal(got, want) {
		t.Fatalf("SupportedServiceProtocols() = %v, want %v", got, want)
	}
}

func TestValidateOpenAIChatOnlyProviderSupportsFormerOllamaShape(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"local": {
				Protocol:         ProviderProtocolOpenAI,
				URL:              "http://localhost:11434/v1",
				ServiceProtocols: []string{RouteProtocolChat},
			},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"local"}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	got := SupportedServiceProtocols(cfg.Provider["local"])
	if !slices.Equal(got, []string{RouteProtocolChat}) {
		t.Fatalf("SupportedServiceProtocols() = %v, want [chat]", got)
	}
}

func TestValidateCLIProxyBackendMetadata(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"codex": {
				Family:           ProviderProtocolOpenAI,
				Backend:          "CLIProxy",
				BackendProvider:  "Codex",
				URL:              "http://127.0.0.1:18741/v1",
				ServiceProtocols: []string{RouteProtocolChat},
			},
		},
		Route: map[string]*RouteConfig{
			"/codex": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"codex"}},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	prov := cfg.Provider["codex"]
	if prov.Backend != ProviderBackendCLIProxy {
		t.Fatalf("provider backend = %q, want %q", prov.Backend, ProviderBackendCLIProxy)
	}
	if prov.BackendProvider != "codex" {
		t.Fatalf("provider backend_provider = %q, want codex", prov.BackendProvider)
	}
}

func TestValidateCLIProxyBackendRequiresOpenAIProvider(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"codex": {
				Family:           ProviderProtocolAnthropic,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "codex",
				URL:              "http://127.0.0.1:18741/v1",
				ServiceProtocols: []string{RouteProtocolChat},
			},
		},
		Route: map[string]*RouteConfig{
			"/codex": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"codex"}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "requires family 'openai'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCLIProxyBackendRequiresProviderAndServiceProtocols(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"codex": {
				Family:  ProviderProtocolOpenAI,
				Backend: ProviderBackendCLIProxy,
				URL:     "http://127.0.0.1:18741/v1",
			},
		},
		Route: map[string]*RouteConfig{
			"/codex": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"codex"}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "backend_provider is required") {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg.Provider["codex"].BackendProvider = "codex"
	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "requires explicit service_protocols") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEmbeddedCLIProxyConfig(t *testing.T) {
	cfg := &ConfigStruct{
		CLIProxy: &CLIProxyConfig{
			Enabled: true,
			AuthDir: "~/.cli-proxy-api",
		},
		Provider: map[string]*ProviderConfig{
			"codex": {
				Family:           ProviderProtocolOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "codex",
				URL:              "http://127.0.0.1:18741/v1",
				ServiceProtocols: []string{RouteProtocolChat},
			},
		},
		Route: map[string]*RouteConfig{
			"/codex": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"codex"}},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if strings.HasPrefix(cfg.CLIProxy.AuthDir, "~/") {
		t.Fatalf("cliproxy auth_dir was not expanded: %q", cfg.CLIProxy.AuthDir)
	}
}

func TestValidateEmbeddedCLIProxyRejectsUnsafeEndpoint(t *testing.T) {
	cfg := &ConfigStruct{
		CLIProxy: &CLIProxyConfig{Enabled: true},
		Provider: map[string]*ProviderConfig{
			"codex": {
				Family:           ProviderProtocolOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "codex",
				URL:              "http://192.168.1.10:18741/v1",
				ServiceProtocols: []string{RouteProtocolChat},
			},
		},
		Route: map[string]*RouteConfig{
			"/codex": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"codex"}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "loopback host") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEmbeddedCLIProxyRejectsMismatchedProviders(t *testing.T) {
	cfg := &ConfigStruct{
		CLIProxy: &CLIProxyConfig{Enabled: true},
		Provider: map[string]*ProviderConfig{
			"codex": {
				Family:           ProviderProtocolOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "codex",
				URL:              "http://127.0.0.1:18741/v1",
				ServiceProtocols: []string{RouteProtocolChat},
			},
			"gemini": {
				Family:           ProviderProtocolOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "gemini",
				URL:              "http://127.0.0.1:18742/v1",
				ServiceProtocols: []string{RouteProtocolChat},
			},
		},
		Route: map[string]*RouteConfig{
			"/codex": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"codex"}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "must share the same endpoint") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateProviderRejectsIncompatibleAnthropicServiceProtocol(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"primary": {
				Protocol:         ProviderProtocolOpenAI,
				URL:              "https://api.openai.com/v1",
				APIKey:           "test-key",
				ServiceProtocols: []string{RouteProtocolAnthropic},
			},
		},
		Route: map[string]*RouteConfig{
			"/anthropic": {
				Protocol: RouteProtocolAnthropic,
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
	if !strings.Contains(err.Error(), "incompatible with adapter capabilities") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateProviderRejectsIncompatibleEmbeddingsServiceProtocol(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"copilot": {
				Protocol:         ProviderProtocolCopilot,
				APIKey:           "test-key",
				ServiceProtocols: []string{RouteProtocolChat, ServiceProtocolEmbeddings},
			},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"copilot"}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "incompatible with adapter capabilities") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRouteDisablesEmbeddingsWhenNoUpstreamSupportsIt(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"anthropic": {
				Protocol: ProviderProtocolAnthropic,
				URL:      "https://anthropic.example.com",
				APIKey:   "test-key",
			},
		},
		Route: map[string]*RouteConfig{
			"/claude": {
				Protocol: RouteProtocolAnthropic,
				ExactModels: map[string]*ExactRouteModelConfig{
					"claude-3-7-sonnet": {
						Upstreams: []*RouteUpstreamConfig{{Provider: "anthropic", Model: "claude-3-7-sonnet"}},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if cfg.Route["/claude"].SupportsServiceProtocol(ServiceProtocolEmbeddings) {
		t.Fatal("anthropic-only route should not expose embeddings")
	}
}

func TestValidateRouteAcceptsExplicitServiceProtocols(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {
				Protocol: ProviderProtocolOpenAI,
				URL:      "https://api.openai.com/v1",
				APIKey:   "test-key",
			},
		},
		Route: map[string]*RouteConfig{
			"/multi": {
				Protocol:         RouteProtocolChat,
				ServiceProtocols: []string{RouteProtocolChat, RouteProtocolResponsesStateless, ServiceProtocolEmbeddings},
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"openai"}},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	for _, protocol := range []string{RouteProtocolChat, RouteProtocolResponsesStateless, ServiceProtocolEmbeddings} {
		if !cfg.Route["/multi"].SupportsServiceProtocol(protocol) {
			t.Fatalf("route should support %s", protocol)
		}
	}
}

func TestValidateRouteRejectsServiceProtocolsWithoutPrimaryProtocol(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {
				Protocol: ProviderProtocolOpenAI,
				URL:      "https://api.openai.com/v1",
				APIKey:   "test-key",
			},
		},
		Route: map[string]*RouteConfig{
			"/bad": {
				Protocol:         RouteProtocolChat,
				ServiceProtocols: []string{ServiceProtocolEmbeddings},
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"openai"}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "service_protocols must include route protocol chat") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRouteRejectsExplicitServiceProtocolWithoutProviderSupport(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"copilot": {
				Protocol: ProviderProtocolCopilot,
				APIKey:   "test-key",
			},
		},
		Route: map[string]*RouteConfig{
			"/multi": {
				Protocol:         RouteProtocolChat,
				ServiceProtocols: []string{RouteProtocolChat, RouteProtocolResponsesStateless},
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"copilot"}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "service_protocols includes responses_stateless but no route upstream/provider supports it") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRouteRejectsEmbeddingsOnlyProviderWithoutPrimaryProtocol(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"embeddings": {
				Protocol:         ProviderProtocolOpenAI,
				URL:              "https://api.openai.com/v1",
				APIKey:           "test-key",
				ServiceProtocols: []string{ServiceProtocolEmbeddings},
			},
		},
		Route: map[string]*RouteConfig{
			"/openai": {
				Protocol: RouteProtocolChat,
				ExactModels: map[string]*ExactRouteModelConfig{
					"text-embedding-3-small": {
						Upstreams: []*RouteUpstreamConfig{{Provider: "embeddings", Model: "text-embedding-3-small"}},
					},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "at least one upstream must support route protocol chat") {
		t.Fatalf("unexpected error: %v", err)
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
			"copilot": {
				Family:  ProviderProtocolCopilot,
				APIKey:  "test-key",
				Timeout: "30s",
			},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {
						Providers: []string{"copilot"},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	prov := cfg.Provider["copilot"]
	if prov.Protocol != ProviderProtocolCopilot {
		t.Fatalf("provider protocol = %q, want %q", prov.Protocol, ProviderProtocolCopilot)
	}
	if prov.URL != "https://api.githubcopilot.com" {
		t.Fatalf("provider URL = %q, want copilot default", prov.URL)
	}
	if !strings.HasSuffix(prov.ConfigDir, "/.config/github-copilot") {
		t.Fatalf("expected copilot default config_dir to be applied, got %q", prov.ConfigDir)
	}
}

func TestValidateProviderRejectsRemovedQwenFamily(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"qwen": {
				Family: "qwen",
				URL:    "https://portal.qwen.ai/v1",
				APIKey: "test-key",
			},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"qwen"}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), `family "qwen" is no longer supported`) {
		t.Fatalf("unexpected error: %v", err)
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
