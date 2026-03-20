package gateway

import "github.com/wweir/warden/config"

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
