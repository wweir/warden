package gateway

import (
	"context"
	"net/http"

	"github.com/wweir/warden/config"
)

type clientRequestContextKey struct{}
type routeHooksContextKey struct{}
type apiKeyNameContextKey struct{}

func withClientRequest(ctx context.Context, req *http.Request) context.Context {
	if ctx == nil || req == nil {
		return ctx
	}
	return context.WithValue(ctx, clientRequestContextKey{}, req)
}

func clientRequestFromContext(ctx context.Context) (*http.Request, bool) {
	if ctx == nil {
		return nil, false
	}
	req, ok := ctx.Value(clientRequestContextKey{}).(*http.Request)
	return req, ok
}

func withRouteHooks(ctx context.Context, hooks []*config.HookRuleConfig) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, routeHooksContextKey{}, hooks)
}

func routeHooksFromContext(ctx context.Context) []*config.HookRuleConfig {
	if ctx == nil {
		return nil
	}
	hooks, _ := ctx.Value(routeHooksContextKey{}).([]*config.HookRuleConfig)
	return hooks
}

func withAPIKeyName(ctx context.Context, name string) context.Context {
	if ctx == nil || name == "" {
		return ctx
	}
	return context.WithValue(ctx, apiKeyNameContextKey{}, name)
}

func apiKeyNameFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	name, _ := ctx.Value(apiKeyNameContextKey{}).(string)
	return name
}
