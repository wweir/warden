package config

import (
	"encoding/json"
	"net/http"
	"net/url"
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
	transport, ok := client1.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client1.Transport)
	}
	if transport.IdleConnTimeout != 30*time.Second {
		t.Fatalf("provider HTTP client IdleConnTimeout = %s, want 30s", transport.IdleConnTimeout)
	}
	if transport.DisableKeepAlives {
		t.Fatal("provider HTTP client should keep connections alive")
	}
}

func TestProviderConfigHTTPClientUsesProviderProxy(t *testing.T) {
	prov := &ProviderConfig{
		Name:  "test",
		URL:   "https://upstream.example.test",
		Proxy: "http://127.0.0.1:18080",
	}

	client := prov.HTTPClient(0)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	reqURL, err := url.Parse("https://upstream.example.test/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, err := transport.Proxy(&http.Request{URL: reqURL})
	if err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if proxyURL == nil || proxyURL.String() != prov.Proxy {
		t.Fatalf("proxy URL = %v, want %s", proxyURL, prov.Proxy)
	}
}

func TestProviderConfigHTTPClientSkipsProxyForCLIProxyBackend(t *testing.T) {
	prov := &ProviderConfig{
		Name:    "codex",
		URL:     "http://127.0.0.1:18741/v1",
		Backend: ProviderBackendCLIProxy,
		Proxy:   "socks5h://192.168.1.2:1080",
	}

	client := prov.HTTPClient(0)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("cliproxy backend HTTP client should connect to the local bridge directly")
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
	cfg := &ConfigStruct{
		Log: &LogConfig{
			Targets: []*LogTarget{nil},
		},
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
				Protocol: RouteProtocolResponses,
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o-mini": testExactModel(RouteProtocolResponses, &RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o-mini"}),
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
				Format:          "anthropic",
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
	if !strings.Contains(err.Error(), "responses_to_chat requires format 'openai'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAnthropicToChatRequiresOpenAI(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"anthropic": {
				URL:             "https://api.anthropic.com/v1",
				Format:          "anthropic",
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
	if !strings.Contains(err.Error(), "anthropic_to_chat requires format 'openai'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAnthropicRouteAllowsOpenAIProviderWhenAnthropicToChatEnabled(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {
				URL:             "https://api.openai.com/v1",
				Format:          "openai",
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

func TestValidateProviderRequiresAbsoluteURL(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "api.openai.com/v1", Format: "openai"},
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
				Format: "openai",
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
			provider: &ProviderConfig{Format: "openai"},
			want: []string{
				RouteProtocolChat,
				RouteProtocolResponses,
			},
		},
		{
			name:     "copilot",
			provider: &ProviderConfig{Format: "copilot"},
			want:     []string{RouteProtocolChat},
		},
		{
			name:     "anthropic",
			provider: &ProviderConfig{Format: "anthropic"},
			want:     []string{RouteProtocolChat, RouteProtocolAnthropic},
		},
		{
			name:     "openai anthropic_to_chat",
			provider: &ProviderConfig{Format: "openai", AnthropicToChat: true},
			want: []string{
				RouteProtocolChat,
				RouteProtocolResponses,
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
			provider: &ProviderConfig{Format: "openai"},
			want: []string{
				RouteProtocolChat,
				RouteProtocolResponses,
				ServiceProtocolEmbeddings,
			},
		},
		{
			name:     "anthropic",
			provider: &ProviderConfig{Format: "anthropic"},
			want:     []string{RouteProtocolChat, RouteProtocolAnthropic},
		},
		{
			name:     "openai anthropic_to_chat",
			provider: &ProviderConfig{Format: "openai", AnthropicToChat: true},
			want: []string{
				RouteProtocolChat,
				RouteProtocolResponses,
				ServiceProtocolEmbeddings,
				RouteProtocolAnthropic,
			},
		},
		{
			name:     "explicit chat only capability",
			provider: &ProviderConfig{Format: "openai", Protocols: []string{RouteProtocolChat}},
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
				Format: "openai",
				URL:    "https://api.openai.com/v1",
				APIKey: "test-key",
			},
		},
		Route: map[string]*RouteConfig{
			"/responses": {
				Protocol: RouteProtocolResponses,
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
	if prov.Format != ProviderFormatOpenAI {
		t.Fatalf("provider format = %q, want %q", prov.Format, ProviderFormatOpenAI)
	}
}

func TestValidateProviderFormatNormalization(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"primary": {
				Format: "openai",
				URL:    "https://api.openai.com/v1",
				APIKey: "test-key",
			},
		},
		Route: map[string]*RouteConfig{
			"/responses": {
				Protocol: RouteProtocolResponses,
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
	if prov.Format != ProviderFormatOpenAI {
		t.Fatalf("provider format = %q, want %q", prov.Format, ProviderFormatOpenAI)
	}
}

func TestValidateProviderExplicitServiceProtocolsOverrideDefaults(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"primary": {
				Format:           ProviderFormatOpenAI,
				URL:              "https://api.openai.com/v1",
				APIKey:           "test-key",
				Protocols: []string{RouteProtocolChat, ServiceProtocolEmbeddings},
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
				Format:           ProviderFormatOpenAI,
				URL:              "http://localhost:11434/v1",
				Protocols: []string{RouteProtocolChat},
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
				Format:           ProviderFormatOpenAI,
				Backend:          "CLIProxy",
				BackendProvider:  "Codex",
				URL:              "http://127.0.0.1:18741/v1",
				Protocols: []string{RouteProtocolChat},
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
				Format:           ProviderFormatAnthropic,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "codex",
				URL:              "http://127.0.0.1:18741/v1",
				Protocols: []string{RouteProtocolChat},
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
	if !strings.Contains(err.Error(), "requires format 'openai'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCLIProxyBackendRequiresProviderAndServiceProtocols(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"codex": {
				Format:  ProviderFormatOpenAI,
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
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected validation to pass after adding backend_provider, got: %v", err)
	}
}

func TestValidateEmbeddedCLIProxyDefaultsAuthDirToEtcWarden(t *testing.T) {
	cfg := &ConfigStruct{CLIProxy: &CLIProxyConfig{}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if cfg.CLIProxy.AuthDir != DefaultCLIProxyAuthDir {
		t.Fatalf("cliproxy auth_dir = %q, want %q", cfg.CLIProxy.AuthDir, DefaultCLIProxyAuthDir)
	}
}

func TestValidateEmbeddedCLIProxyConfig(t *testing.T) {
	cfg := &ConfigStruct{
		CLIProxy: &CLIProxyConfig{
			AuthDir: "~/.cli-proxy-api",
		},
		Provider: map[string]*ProviderConfig{
			"codex": {
				Format:           ProviderFormatOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "codex",
				URL:              "http://127.0.0.1:18741/v1",
				Protocols: []string{RouteProtocolChat},
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
				Format:           ProviderFormatOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "codex",
				URL:              "http://192.168.1.10:18741/v1",
				Protocols: []string{RouteProtocolChat},
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
				Format:           ProviderFormatOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "codex",
				URL:              "http://127.0.0.1:18741/v1",
				Protocols: []string{RouteProtocolChat},
			},
			"gemini": {
				Format:           ProviderFormatOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "gemini",
				URL:              "http://127.0.0.1:18742/v1",
				Protocols: []string{RouteProtocolChat},
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

func TestValidateEmbeddedCLIProxyAcceptsSharedProviderProxy(t *testing.T) {
	cfg := &ConfigStruct{
		CLIProxy: &CLIProxyConfig{Enabled: true},
		Provider: map[string]*ProviderConfig{
			"codex": {
				Format:           ProviderFormatOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "codex",
				URL:              "http://127.0.0.1:18741/v1",
				Proxy:            "http://127.0.0.1:8080",
				Protocols: []string{RouteProtocolChat},
			},
			"gemini": {
				Format:           ProviderFormatOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "gemini",
				URL:              "http://127.0.0.1:18741/v1",
				Proxy:            "http://127.0.0.1:8080",
				Protocols: []string{RouteProtocolChat},
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
}

func TestValidateEmbeddedCLIProxyRejectsConflictingProviderProxy(t *testing.T) {
	cfg := &ConfigStruct{
		CLIProxy: &CLIProxyConfig{Enabled: true},
		Provider: map[string]*ProviderConfig{
			"codex": {
				Format:           ProviderFormatOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "codex",
				URL:              "http://127.0.0.1:18741/v1",
				Proxy:            "http://127.0.0.1:8080",
				Protocols: []string{RouteProtocolChat},
			},
			"gemini": {
				Format:           ProviderFormatOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "gemini",
				URL:              "http://127.0.0.1:18741/v1",
				Proxy:            "http://127.0.0.1:8081",
				Protocols: []string{RouteProtocolChat},
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
	if !strings.Contains(err.Error(), "share one outbound proxy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEmbeddedCLIProxyAllowsProviderProxyConflictWithExplicitCLIProxyProxy(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"codex": {
				Format:           ProviderFormatOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "codex",
				URL:              "http://127.0.0.1:18741/v1",
				Proxy:            "http://127.0.0.1:8080",
				Protocols: []string{RouteProtocolChat},
			},
			"gemini": {
				Format:           ProviderFormatOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "gemini",
				URL:              "http://127.0.0.1:18741/v1",
				Proxy:            "http://127.0.0.1:8081",
				Protocols: []string{RouteProtocolChat},
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
}

func TestValidateRouteDisablesEmbeddingsWhenNoUpstreamSupportsIt(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"anthropic": {
				Format:   ProviderFormatAnthropic,
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
				Format:   ProviderFormatOpenAI,
				URL:      "https://api.openai.com/v1",
				APIKey:   "test-key",
			},
		},
		Route: map[string]*RouteConfig{
			"/multi": {
				Protocol:         RouteProtocolChat,
				ServiceProtocols: []string{RouteProtocolChat, RouteProtocolResponses, ServiceProtocolEmbeddings},
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"openai"}},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	for _, protocol := range []string{RouteProtocolChat, RouteProtocolResponses, ServiceProtocolEmbeddings} {
		if !cfg.Route["/multi"].SupportsServiceProtocol(protocol) {
			t.Fatalf("route should support %s", protocol)
		}
	}
}

func TestValidateRouteRejectsServiceProtocolsWithoutPrimaryProtocol(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {
				Format:   ProviderFormatOpenAI,
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
				Format:   ProviderFormatCopilot,
				APIKey:   "test-key",
			},
		},
		Route: map[string]*RouteConfig{
			"/multi": {
				Protocol:         RouteProtocolChat,
				ServiceProtocols: []string{RouteProtocolChat, RouteProtocolResponses},
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
	if !strings.Contains(err.Error(), "service_protocols includes responses but no route upstream/provider supports it") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRouteRejectsEmbeddingsOnlyProviderWithoutPrimaryProtocol(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"embeddings": {
				Format:           ProviderFormatOpenAI,
				URL:              "https://api.openai.com/v1",
				APIKey:           "test-key",
				Protocols: []string{ServiceProtocolEmbeddings},
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

func TestValidateProviderFamilyAppliesDefaults(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"copilot": {
				Format:  ProviderFormatCopilot,
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
	if prov.Format != ProviderFormatCopilot {
		t.Fatalf("provider protocol = %q, want %q", prov.Format, ProviderFormatCopilot)
	}
	if prov.URL != "https://api.githubcopilot.com" {
		t.Fatalf("provider URL = %q, want copilot default", prov.URL)
	}
	if !strings.HasSuffix(prov.ConfigDir, "/.config/github-copilot") {
		t.Fatalf("expected copilot default config_dir to be applied, got %q", prov.ConfigDir)
	}
}

func TestValidateRouteConfigRequiresRouteProtocol(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol:   "responses_invalid",
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
	if !strings.Contains(err.Error(), "chat/responses/anthropic") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsTopLevelAPIKeys(t *testing.T) {
	cfg := &ConfigStruct{
		APIKeys: map[string]SecretString{
			"legacy": "secret",
		},
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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
			"openai": {URL: "https://api.openai.com/v1", Format: "openai"},
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

func TestValidateProviderAPIKeyCommand(t *testing.T) {
	tests := []struct {
		name     string
		provider *ProviderConfig
		wantErr  string
	}{
		{
			name: "openai command",
			provider: &ProviderConfig{
				Format:               ProviderFormatOpenAI,
				URL:                  "https://api.openai.com/v1",
				APIKeyCommand:        "printf token",
				APIKeyCommandTimeout: "2s",
				APIKeyCommandTTL:     "10m",
			},
		},
		{
			name: "anthropic command",
			provider: &ProviderConfig{
				Format:        ProviderFormatAnthropic,
				URL:           "https://api.anthropic.com/v1",
				APIKeyCommand: "printf token",
			},
		},
		{
			name: "static and command conflict",
			provider: &ProviderConfig{
				Format:        ProviderFormatOpenAI,
				URL:           "https://api.openai.com/v1",
				APIKey:        "static",
				APIKeyCommand: "printf token",
			},
			wantErr: "api_key and api_key_command are mutually exclusive",
		},
		{
			name: "invalid timeout",
			provider: &ProviderConfig{
				Format:               ProviderFormatOpenAI,
				URL:                  "https://api.openai.com/v1",
				APIKeyCommand:        "printf token",
				APIKeyCommandTimeout: "soon",
			},
			wantErr: "invalid api_key_command_timeout",
		},
		{
			name: "zero timeout",
			provider: &ProviderConfig{
				Format:               ProviderFormatOpenAI,
				URL:                  "https://api.openai.com/v1",
				APIKeyCommand:        "printf token",
				APIKeyCommandTimeout: "0s",
			},
			wantErr: "api_key_command_timeout must be > 0",
		},
		{
			name: "negative timeout",
			provider: &ProviderConfig{
				Format:               ProviderFormatOpenAI,
				URL:                  "https://api.openai.com/v1",
				APIKeyCommand:        "printf token",
				APIKeyCommandTimeout: "-1s",
			},
			wantErr: "api_key_command_timeout must be > 0",
		},
		{
			name: "invalid ttl",
			provider: &ProviderConfig{
				Format:           ProviderFormatOpenAI,
				URL:              "https://api.openai.com/v1",
				APIKeyCommand:    "printf token",
				APIKeyCommandTTL: "later",
			},
			wantErr: "invalid api_key_command_ttl",
		},
		{
			name: "negative ttl",
			provider: &ProviderConfig{
				Format:           ProviderFormatOpenAI,
				URL:              "https://api.openai.com/v1",
				APIKeyCommand:    "printf token",
				APIKeyCommandTTL: "-1s",
			},
			wantErr: "api_key_command_ttl must be >= 0",
		},
		{
			name: "cliproxy rejects command",
			provider: &ProviderConfig{
				Format:           ProviderFormatOpenAI,
				Backend:          ProviderBackendCLIProxy,
				BackendProvider:  "codex",
				URL:              "http://127.0.0.1:18741/v1",
				Protocols: []string{RouteProtocolChat},
				APIKeyCommand:    "printf token",
			},
			wantErr: "api_key_command is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ConfigStruct{
				Provider: map[string]*ProviderConfig{"primary": tt.provider},
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
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected %q error, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestValidateProviderAPIKeyCommandDoesNotChangeServiceProtocols(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"primary": {
				Format:           ProviderFormatOpenAI,
				URL:              "https://api.openai.com/v1",
				APIKeyCommand:    "printf token",
				Protocols: []string{RouteProtocolChat, ServiceProtocolEmbeddings},
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

func TestValidateCopilotAPIKeyCommandSkipsConfigDirCredentialCheck(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"copilot": {
				Format:        ProviderFormatCopilot,
				APIKeyCommand: "printf token",
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

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}


// === Access Mode Validation Tests ===


func TestValidateEndpointBasic(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"bailian": {
				APIKey: "sk-sp-xxxxx",
				Endpoints: map[string]*ProviderEndpointConfig{
					"openai": {
						URL:    "https://coding.dashscope.aliyuncs.com/v1",
						Format: "openai",
					},
					"anthropic": {
						URL:    "https://coding.dashscope.aliyuncs.com/apps/anthropic",
						Format: "anthropic",
					},
				},
			},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"bailian"}},
				},
			},
			"/anthropic": {
				Protocol: RouteProtocolAnthropic,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"bailian"}},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateEndpointRejectsShorthandMix(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"bad": {
				URL: "https://bad.example.com",
				Endpoints: map[string]*ProviderEndpointConfig{
					"openai": {
						URL: "https://bad.example.com/v1",
					},
				},
			},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"bad"}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "url must not be set when endpoint") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEndpointURLValidation(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"badurl": {
				Endpoints: map[string]*ProviderEndpointConfig{
					"openai": {
						URL: "not-a-url",
					},
				},
			},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"badurl"}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "url must be an absolute http/https URL") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEndpointCliproxyRequiresOpenAI(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"codex": {
				Backend:         ProviderBackendCLIProxy,
				BackendProvider: "codex",
				Endpoints: map[string]*ProviderEndpointConfig{
					"anthropic": {
						URL:    "http://127.0.0.1:18741/v1",
						Format: "anthropic",
					},
				},
			},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
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
	if !strings.Contains(err.Error(), "requires format 'openai'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEndpointServiceProtocols(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"test": {
				Endpoints: map[string]*ProviderEndpointConfig{
					"openai": {
						URL:       "https://test.example.com",
						Format:    "openai",
						Protocols: []string{RouteProtocolChat, RouteProtocolAnthropic},
					},
				},
			},
		},
		Route: map[string]*RouteConfig{
			"/chat": {
				Protocol: RouteProtocolChat,
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"*": {Providers: []string{"test"}},
				},
			},
		},
	}

	// anthropic without bridge is allowed in protocols list since we don't validate
	// protocol/format compatibility at config level for explicit protocols
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestFormatEffectiveURLWithEndpoints(t *testing.T) {
	prov := &ProviderConfig{
		Endpoints: map[string]*ProviderEndpointConfig{
			"openai": {
				URL:    "https://openai.example.com",
				Format: "openai",
			},
			"anthropic": {
				URL:    "https://anthropic.example.com",
				Format: "anthropic",
			},
		},
	}

	if got := FormatEffectiveURL(prov, ProviderFormatOpenAI); got != "https://openai.example.com" {
		t.Fatalf("FormatEffectiveURL(openai) = %q, want %q", got, "https://openai.example.com")
	}
	if got := FormatEffectiveURL(prov, ProviderFormatAnthropic); got != "https://anthropic.example.com" {
		t.Fatalf("FormatEffectiveURL(anthropic) = %q, want %q", got, "https://anthropic.example.com")
	}
}

func TestFormatHeadersWithEndpoints(t *testing.T) {
	prov := &ProviderConfig{
		Headers: map[string]string{
			"X-Common":   "value",
			"X-Override": "provider",
		},
		Endpoints: map[string]*ProviderEndpointConfig{
			"openai": {
				Format: "openai",
				Headers: map[string]string{
					"X-OpenAI":   "openai-value",
					"X-Override": "openai",
				},
			},
			"anthropic": {
				Format: "anthropic",
				Headers: map[string]string{
					"X-Anthropic": "anthropic-value",
				},
			},
		},
	}

	openaiHeaders := FormatHeaders(prov, ProviderFormatOpenAI)
	if openaiHeaders["X-Common"] != "value" {
		t.Fatalf("openai headers missing common header")
	}
	if openaiHeaders["X-OpenAI"] != "openai-value" {
		t.Fatalf("openai headers missing endpoint-specific header")
	}
	if openaiHeaders["X-Override"] != "openai" {
		t.Fatalf("endpoint-specific header should override provider-level")
	}

	anthropicHeaders := FormatHeaders(prov, ProviderFormatAnthropic)
	if anthropicHeaders["X-Common"] != "value" {
		t.Fatalf("anthropic headers missing common header")
	}
	if anthropicHeaders["X-Anthropic"] != "anthropic-value" {
		t.Fatalf("anthropic headers missing endpoint-specific header")
	}
	if anthropicHeaders["X-Override"] != "provider" {
		t.Fatalf("provider-level header should remain when not overridden")
	}
}

func TestProviderEndpointSelectionByURL(t *testing.T) {
	prov := &ProviderConfig{
		Headers: map[string]string{
			"X-Common": "provider",
		},
		Endpoints: map[string]*ProviderEndpointConfig{
			"openai": {
				URL:    "https://gateway.example.com/openai/v1",
				Format: "openai",
				Headers: map[string]string{
					"Authorization": "Bearer openai-token",
					"X-Mode":         "openai",
				},
			},
			"anthropic": {
				URL:    "https://gateway.example.com/anthropic/v1",
				Format: "anthropic",
				Headers: map[string]string{
					"x-api-key": "anthropic-token",
					"X-Mode":    "anthropic",
				},
			},
		},
	}

	if got := ProviderFormatForURL(prov, "https://gateway.example.com/anthropic/v1/messages"); got != ProviderFormatAnthropic {
		t.Fatalf("ProviderFormatForURL(anthropic) = %q, want %q", got, ProviderFormatAnthropic)
	}
	if got := ProviderFormatForURL(prov, "https://gateway.example.com/anthropic/v1/messages?x=1"); got != ProviderFormatAnthropic {
		t.Fatalf("ProviderFormatForURL(query) = %q, want %q", got, ProviderFormatAnthropic)
	}
	headers := ProviderHeadersForURL(prov, "https://gateway.example.com/anthropic/v1/messages")
	if headers["X-Mode"] != "anthropic" {
		t.Fatalf("ProviderHeadersForURL() = %v, want anthropic endpoint headers", headers)
	}
	if headers["X-Common"] != "provider" {
		t.Fatalf("ProviderHeadersForURL() should keep provider headers, got %v", headers)
	}
}

func TestFormatModelsWithEndpoints(t *testing.T) {
	prov := &ProviderConfig{
		Models: []string{"common-model"},
		Endpoints: map[string]*ProviderEndpointConfig{
			"openai": {
				Format: "openai",
				Models: []string{"gpt-4o", "gpt-4o-mini"},
			},
			"anthropic": {
				Format: "anthropic",
				Models: []string{"claude-3-7-sonnet"},
			},
		},
	}

	openaiModels := FormatModels(prov, ProviderFormatOpenAI)
	if !slices.Equal(openaiModels, []string{"gpt-4o", "gpt-4o-mini"}) {
		t.Fatalf("FormatModels(openai) = %v", openaiModels)
	}

	anthropicModels := FormatModels(prov, ProviderFormatAnthropic)
	if !slices.Equal(anthropicModels, []string{"claude-3-7-sonnet"}) {
		t.Fatalf("FormatModels(anthropic) = %v", anthropicModels)
	}

	// When endpoint-specific models are empty, fall back to provider models
	provNoEndpointModels := &ProviderConfig{
		Models: []string{"fallback-model"},
		Endpoints: map[string]*ProviderEndpointConfig{
			"openai": {
				Format: "openai",
			},
		},
	}
	if got := FormatModels(provNoEndpointModels, ProviderFormatOpenAI); !slices.Equal(got, []string{"fallback-model"}) {
		t.Fatalf("FormatModels(fallback) = %v", got)
	}
}
