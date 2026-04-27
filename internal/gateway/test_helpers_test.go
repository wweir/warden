package gateway

import (
	"testing"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
)

func exactModel(protocol string, upstreams ...*config.RouteUpstreamConfig) *config.ExactRouteModelConfig {
	_ = protocol

	return &config.ExactRouteModelConfig{
		Upstreams: upstreams,
	}
}

func wildcardModel(protocol string, providers ...string) *config.WildcardRouteModelConfig {
	_ = protocol

	return &config.WildcardRouteModelConfig{
		Providers: providers,
	}
}

func exactModelProtocols(protocols map[string][]*config.RouteUpstreamConfig) *config.ExactRouteModelConfig {
	for _, upstreams := range protocols {
		return &config.ExactRouteModelConfig{Upstreams: upstreams}
	}
	return &config.ExactRouteModelConfig{}
}

func mustSingleLogRecord(t *testing.T, records []reqlog.Record) reqlog.Record {
	t.Helper()
	if len(records) != 1 {
		t.Fatalf("log records count = %d, want 1: %+v", len(records), records)
	}
	return records[0]
}
