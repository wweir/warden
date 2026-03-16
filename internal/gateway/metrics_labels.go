package gateway

import (
	"net/http"

	"github.com/wweir/warden/config"
	sel "github.com/wweir/warden/internal/selector"
)

type requestMetricLabels struct {
	Route          string
	Protocol       string
	Provider       string
	RouteModel     string
	ProviderModel  string
	MatchedPattern string
	Endpoint       string
}

func buildMetricLabels(route *config.RouteConfig, protocol, endpoint string, target *sel.RouteTarget) requestMetricLabels {
	labels := requestMetricLabels{
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

func applyMetricHeaders(w http.ResponseWriter, labels requestMetricLabels) {
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

func metricLabelsFromHeader(h http.Header) requestMetricLabels {
	return requestMetricLabels{
		Route:          h.Get("X-Route"),
		Protocol:       h.Get("X-Protocol"),
		Provider:       h.Get("X-Provider"),
		RouteModel:     h.Get("X-Route-Model"),
		ProviderModel:  h.Get("X-Provider-Model"),
		MatchedPattern: h.Get("X-Matched-Pattern"),
		Endpoint:       h.Get("X-Endpoint"),
	}
}
