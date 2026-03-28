package telemetry

import (
	"net/http"

	"github.com/wweir/warden/config"
	sel "github.com/wweir/warden/internal/selector"
)

type Labels struct {
	Route          string
	Protocol       string
	APIKey         string
	Provider       string
	RouteModel     string
	ProviderModel  string
	MatchedPattern string
	Endpoint       string
}

func BuildMetricLabels(route *config.RouteConfig, protocol, endpoint string, target *sel.RouteTarget) Labels {
	labels := Labels{
		Route:    route.Prefix,
		Protocol: protocol,
		Endpoint: endpoint,
	}
	if target == nil {
		return labels
	}
	labels.Provider = target.ProviderName
	labels.ProviderModel = target.UpstreamModel
	if target.Wildcard {
		labels.RouteModel = target.RequestedModel
		labels.MatchedPattern = target.MatchedPattern
	} else {
		labels.RouteModel = target.PublicModel
	}
	return labels
}

func ApplyMetricHeaders(w http.ResponseWriter, labels Labels) {
	w.Header().Set("X-Route", labels.Route)
	w.Header().Set("X-Protocol", labels.Protocol)
	w.Header().Set("X-Endpoint", labels.Endpoint)
	w.Header().Set("X-Provider", labels.Provider)
	w.Header().Set("X-Route-Model", labels.RouteModel)
	w.Header().Set("X-Provider-Model", labels.ProviderModel)
	w.Header().Set("X-Matched-Pattern", labels.MatchedPattern)
	if labels.RouteModel != "" {
		w.Header().Set("X-Model", labels.RouteModel)
	}
}

func MetricLabelsFromHeader(h http.Header) Labels {
	return Labels{
		Route:          h.Get("X-Route"),
		Protocol:       h.Get("X-Protocol"),
		Provider:       h.Get("X-Provider"),
		RouteModel:     h.Get("X-Route-Model"),
		ProviderModel:  h.Get("X-Provider-Model"),
		MatchedPattern: h.Get("X-Matched-Pattern"),
		Endpoint:       h.Get("X-Endpoint"),
	}
}
