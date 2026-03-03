package gateway

import (
	"context"
	"net/http"
)

type clientRequestContextKey struct{}

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
