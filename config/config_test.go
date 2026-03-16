package config

import (
	"strings"
	"testing"
	"time"
)

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
				Protocol: "chat",
				Models: map[string]*RouteModelConfig{
					"gpt-4o": {Upstreams: []*RouteUpstreamConfig{{Provider: "openai", Model: "gpt-4o"}}},
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
				Protocol: "chat",
				Models: map[string]*RouteModelConfig{
					"gpt-4o": {Upstreams: []*RouteUpstreamConfig{{Provider: "openai", Model: "gpt-4o"}}},
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
				Protocol: "chat",
				Models: map[string]*RouteModelConfig{
					"gpt-4o": {Upstreams: []*RouteUpstreamConfig{{Provider: "openai", Model: "gpt-4o"}}},
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
			"/anthropic": {Providers: []string{"anthropic"}},
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

func TestValidateProviderRequiresAbsoluteURL(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: "chat",
				Models: map[string]*RouteModelConfig{
					"gpt-4o": {Upstreams: []*RouteUpstreamConfig{{Provider: "openai", Model: "gpt-4o"}}},
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
				Protocol: "chat",
				Models: map[string]*RouteModelConfig{
					"gpt-4o": {Upstreams: []*RouteUpstreamConfig{{Provider: "openai", Model: "gpt-4o"}}},
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
				Protocol: "chat",
				Models: map[string]*RouteModelConfig{
					"gpt-4o": {Upstreams: []*RouteUpstreamConfig{{Provider: "openai", Model: "gpt-4o"}}},
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
				Protocol: "chat",
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": {
						Upstreams: []*RouteUpstreamConfig{{Provider: "openai", Model: "gpt-4.1"}},
					},
				},
				WildcardModels: map[string]*WildcardRouteModelConfig{
					"gpt-*": {
						Providers: []string{"openai"},
					},
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

func TestValidateRouteConfigRejectsMixedLegacyAndExplicitModelFields(t *testing.T) {
	cfg := &ConfigStruct{
		Provider: map[string]*ProviderConfig{
			"openai": {URL: "https://api.openai.com/v1", Protocol: "openai"},
		},
		Route: map[string]*RouteConfig{
			"/test": {
				Protocol: "chat",
				ExactModels: map[string]*ExactRouteModelConfig{
					"gpt-4o": {
						Upstreams: []*RouteUpstreamConfig{{Provider: "openai", Model: "gpt-4o"}},
					},
				},
				Models: map[string]*RouteModelConfig{
					"gpt-*": {
						Providers: []string{"openai"},
					},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "cannot mix exact_models/wildcard_models") {
		t.Fatalf("unexpected error: %v", err)
	}
}
