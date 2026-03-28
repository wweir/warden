package inference

import (
	"fmt"
	"log/slog"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
	sel "github.com/wweir/warden/internal/selector"
)

type ResolvedTarget struct {
	Model    *config.CompiledRouteModel
	Target   *sel.RouteTarget
	Provider *config.ProviderConfig
}

type FailoverCallback func(failed *ResolvedTarget)

type Manager struct {
	cfg              *config.ConfigStruct
	selector         *sel.Selector
	route            *config.RouteConfig
	serviceProtocol  string
	endpoint         string
	requestedModel   string
	explicitProvider string
	allowFailover    bool
	onFailover       FailoverCallback

	current     *ResolvedTarget
	excluded    []string
	authRetried map[string]bool
	failovers   []reqlog.Failover
}

func NewManager(
	cfg *config.ConfigStruct,
	selector *sel.Selector,
	route *config.RouteConfig,
	serviceProtocol, endpoint, requestedModel, explicitProvider string,
	allowFailover bool,
	onFailover FailoverCallback,
) (*Manager, error) {
	current, err := selectRouteTarget(cfg, selector, route, serviceProtocol, requestedModel, explicitProvider, nil)
	if err != nil {
		return nil, err
	}

	return &Manager{
		cfg:              cfg,
		selector:         selector,
		route:            route,
		serviceProtocol:  serviceProtocol,
		endpoint:         endpoint,
		requestedModel:   requestedModel,
		explicitProvider: explicitProvider,
		allowFailover:    allowFailover,
		onFailover:       onFailover,
		current:          current,
		authRetried:      map[string]bool{},
	}, nil
}

func (m *Manager) Current() *ResolvedTarget {
	return m.current
}

func (m *Manager) Failovers() []reqlog.Failover {
	return m.failovers
}

func (m *Manager) HandleError(err error) bool {
	if m.current == nil || m.current.Provider == nil {
		return false
	}
	if TryAuthRetry(err, m.current.Provider, m.authRetried) {
		return true
	}
	if !m.allowFailover || m.explicitProvider != "" || !sel.IsRetryableError(err) {
		return false
	}
	if m.current.Target == nil || m.current.Model == nil {
		return false
	}

	m.excluded = append(m.excluded, m.current.Target.Key)
	m.selector.RecordFailover(m.current.Provider.Name)
	if m.onFailover != nil {
		m.onFailover(m.current)
	}

	nextTarget, nextProv, selErr := m.selector.Select(m.cfg, m.serviceProtocol, m.current.Model, m.requestedModel, m.excluded...)
	if selErr != nil {
		return false
	}

	m.failovers = append(m.failovers, reqlog.Failover{
		FailedProvider:      m.current.Provider.Name,
		FailedProviderModel: m.current.Target.UpstreamModel,
		NextProvider:        nextProv.Name,
		NextProviderModel:   nextTarget.UpstreamModel,
		Error:               err.Error(),
	})
	slog.Warn("Provider failover", "failed", m.current.Provider.Name, "next", nextProv.Name, "error", err, "route", m.route.Prefix, "endpoint", m.endpoint)
	m.current = &ResolvedTarget{
		Model:    m.current.Model,
		Target:   nextTarget,
		Provider: nextProv,
	}
	return true
}

func MatchRouteModel(route *config.RouteConfig, requestedModel string) (*config.CompiledRouteModel, error) {
	if requestedModel == "" {
		return nil, fmt.Errorf("model is required")
	}
	matched := route.MatchModel(requestedModel)
	if matched == nil {
		return nil, fmt.Errorf("model %q is not configured for route %s protocol %s", requestedModel, route.Prefix, route.ConfiguredProtocol())
	}
	return matched, nil
}

func SelectRouteTarget(
	cfg *config.ConfigStruct,
	selector *sel.Selector,
	route *config.RouteConfig,
	serviceProtocol, requestedModel, explicitProvider string,
	exclude []string,
) (*ResolvedTarget, error) {
	return selectRouteTarget(cfg, selector, route, serviceProtocol, requestedModel, explicitProvider, exclude)
}

func selectRouteTarget(
	cfg *config.ConfigStruct,
	selector *sel.Selector,
	route *config.RouteConfig,
	serviceProtocol, requestedModel, explicitProvider string,
	exclude []string,
) (*ResolvedTarget, error) {
	matched, err := MatchRouteModel(route, requestedModel)
	if err != nil {
		return nil, err
	}

	var target *sel.RouteTarget
	var prov *config.ProviderConfig
	if explicitProvider != "" {
		target, prov, err = selector.SelectByName(cfg, serviceProtocol, matched, requestedModel, explicitProvider)
	} else {
		target, prov, err = selector.Select(cfg, serviceProtocol, matched, requestedModel, exclude...)
	}
	if err != nil {
		if explicitProvider != "" {
			return nil, fmt.Errorf("provider %q not found in route model %q", explicitProvider, requestedModel)
		}
		return nil, err
	}
	return &ResolvedTarget{Model: matched, Target: target, Provider: prov}, nil
}
