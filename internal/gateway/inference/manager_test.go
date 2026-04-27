package inference

import (
	"context"
	"fmt"
	"testing"

	"github.com/wweir/warden/config"
	sel "github.com/wweir/warden/internal/selector"
)

func testExactModel(protocol string, upstreams ...*config.RouteUpstreamConfig) *config.ExactRouteModelConfig {
	_ = protocol

	return &config.ExactRouteModelConfig{
		Upstreams: upstreams,
	}
}

func mustValidateRoute(t *testing.T, cfg *config.ConfigStruct) *config.RouteConfig {
	t.Helper()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	return cfg.Route["/test"]
}

func newSingleProviderManager(t *testing.T) *Manager {
	t.Helper()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"primary": {URL: "http://primary.example.com", Protocol: "openai"},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": testExactModel(config.RouteProtocolChat,
						&config.RouteUpstreamConfig{Provider: "primary", Model: "gpt-4o"},
					),
				},
			},
		},
	}
	route := mustValidateRoute(t, cfg)
	selector := sel.NewSelector(cfg)

	manager, err := NewManager(cfg, selector, route, config.RouteProtocolChat, "chat", "gpt-4o", "", true, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}

func TestManagerHandleErrorRetriesOnlyAvailableProviderOnce(t *testing.T) {
	t.Parallel()

	manager := newSingleProviderManager(t)

	current := manager.Current()
	if current.Provider.Name != "primary" {
		t.Fatalf("current provider = %q, want primary", current.Provider.Name)
	}

	retried := manager.HandleError(&sel.UpstreamError{Code: 500, Body: `{"error":{"type":"server_error","message":"boom"}}`})
	if !retried {
		t.Fatal("HandleError() = false, want true")
	}
	if manager.Current().Provider.Name != "primary" {
		t.Fatalf("current provider after retry = %q, want primary", manager.Current().Provider.Name)
	}
	if len(manager.Failovers()) != 0 {
		t.Fatalf("failovers len = %d, want 0", len(manager.Failovers()))
	}

	retried = manager.HandleError(&sel.UpstreamError{Code: 500, Body: `{"error":{"type":"server_error","message":"boom"}}`})
	if retried {
		t.Fatal("HandleError() second retry = true, want false")
	}
}

func TestManagerHandleErrorDoesNotRetryBadRequestValidation(t *testing.T) {
	t.Parallel()

	manager := newSingleProviderManager(t)

	retried := manager.HandleError(&sel.UpstreamError{Code: 400, Body: `{"error":"'input_type' parameter is required for asymmetric models"}`})
	if retried {
		t.Fatal("HandleError() = true, want false for non-retryable request validation error")
	}
}

func TestManagerHandleErrorDoesNotRetryWrappedContextCanceled(t *testing.T) {
	t.Parallel()

	manager := newSingleProviderManager(t)

	retried := manager.HandleError(fmt.Errorf("send request: %w", context.Canceled))
	if retried {
		t.Fatal("HandleError() = true, want false for wrapped context.Canceled")
	}
}

func TestManagerHandleErrorDoesNotRetryWrappedContextDeadlineExceeded(t *testing.T) {
	t.Parallel()

	manager := newSingleProviderManager(t)

	retried := manager.HandleError(fmt.Errorf("send request: %w", context.DeadlineExceeded))
	if retried {
		t.Fatal("HandleError() = true, want false for wrapped context.DeadlineExceeded")
	}
}

func TestManagerHandleErrorFailoversWhenAlternativeExists(t *testing.T) {
	t.Parallel()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"primary":   {URL: "http://primary.example.com", Protocol: "openai"},
			"secondary": {URL: "http://secondary.example.com", Protocol: "openai"},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": testExactModel(config.RouteProtocolChat,
						&config.RouteUpstreamConfig{Provider: "primary", Model: "gpt-4o"},
						&config.RouteUpstreamConfig{Provider: "secondary", Model: "gpt-4.1"},
					),
				},
			},
		},
	}
	route := mustValidateRoute(t, cfg)
	selector := sel.NewSelector(cfg)

	manager, err := NewManager(cfg, selector, route, config.RouteProtocolChat, "chat", "gpt-4o", "", true, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	failedOver := manager.HandleError(&sel.UpstreamError{Code: 500, Body: `{"error":{"type":"server_error","message":"boom"}}`})
	if !failedOver {
		t.Fatal("HandleError() = false, want true")
	}
	if manager.Current().Provider.Name != "secondary" {
		t.Fatalf("current provider after failover = %q, want secondary", manager.Current().Provider.Name)
	}
	if len(manager.Failovers()) != 1 {
		t.Fatalf("failovers len = %d, want 1", len(manager.Failovers()))
	}
}
