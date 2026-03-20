package gateway

import (
	"fmt"
	"net/http"

	"github.com/wweir/warden/config"
	sel "github.com/wweir/warden/internal/selector"
)

type resolvedRouteTarget struct {
	model  *config.CompiledRouteModel
	target *sel.RouteTarget
	prov   *config.ProviderConfig
}

func matchRouteModel(route *config.RouteConfig, requestedModel string) (*config.CompiledRouteModel, error) {
	return matchRouteModelForProtocol(route, requestedModel)
}

func matchRouteModelForProtocol(route *config.RouteConfig, requestedModel string) (*config.CompiledRouteModel, error) {
	if requestedModel == "" {
		return nil, fmt.Errorf("model is required")
	}
	matched := route.MatchModel(requestedModel)
	if matched == nil {
		return nil, fmt.Errorf("model %q is not configured for route %s protocol %s", requestedModel, route.Prefix, route.ConfiguredProtocol())
	}
	return matched, nil
}

func (g *Gateway) selectRouteTarget(route *config.RouteConfig, serviceProtocol, requestedModel, explicitProvider string, exclude []string) (*resolvedRouteTarget, error) {
	matched, err := matchRouteModelForProtocol(route, requestedModel)
	if err != nil {
		return nil, err
	}

	var target *sel.RouteTarget
	var prov *config.ProviderConfig
	if explicitProvider != "" {
		target, prov, err = g.selector.SelectByName(g.cfg, serviceProtocol, matched, requestedModel, explicitProvider)
	} else {
		target, prov, err = g.selector.Select(g.cfg, serviceProtocol, matched, requestedModel, exclude...)
	}
	if err != nil {
		if explicitProvider != "" {
			return nil, fmt.Errorf("provider %q not found in route model %q", explicitProvider, requestedModel)
		}
		return nil, err
	}
	return &resolvedRouteTarget{model: matched, target: target, prov: prov}, nil
}

func writeModelSelectionError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusBadRequest)
}
