package integration

import "github.com/wweir/warden/config"

func exactModel(protocol string, upstreams ...*config.RouteUpstreamConfig) *config.ExactRouteModelConfig {
	_ = protocol

	return &config.ExactRouteModelConfig{
		Upstreams: upstreams,
	}
}
