package selector

import (
	"log/slog"
	"slices"
	"time"

	"github.com/wweir/warden/config"
)

type candidate struct {
	target  *RouteTarget
	provCfg *config.ProviderConfig
	state   *providerState
}

// Select returns the best upstream target for the given matched route model.
func (s *Selector) Select(cfg *config.ConfigStruct, serviceProtocol string, matched *config.CompiledRouteModel, requestedModel string, exclude ...string) (*RouteTarget, *config.ProviderConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	candidates := s.buildCandidates(cfg, serviceProtocol, matched, requestedModel, exclude)
	if len(candidates) == 0 {
		return nil, nil, ErrProviderNotFound
	}

	now := time.Now()
	for _, c := range candidates {
		if c.state.manualSuppress {
			continue
		}
		if now.After(c.state.suppressUntil) {
			return c.target, c.provCfg, nil
		}
	}

	autoSuppressed := make([]candidate, 0, len(candidates))
	for _, c := range candidates {
		if c.state.manualSuppress {
			continue
		}
		autoSuppressed = append(autoSuppressed, c)
	}
	if len(autoSuppressed) == 0 {
		return nil, nil, ErrProviderNotFound
	}

	earliest := autoSuppressed[0]
	for _, c := range autoSuppressed[1:] {
		if c.state.suppressUntil.Before(earliest.state.suppressUntil) {
			earliest = c
		}
	}
	suppressedInfo := make([]any, 0, len(autoSuppressed)*4+2)
	suppressedInfo = append(suppressedInfo, "selected", earliest.target.ProviderName, "suppress_until", earliest.state.suppressUntil)
	for _, c := range autoSuppressed {
		suppressedInfo = append(suppressedInfo,
			c.target.ProviderName+"_failures", c.state.consecutiveFailures,
			c.target.ProviderName+"_suppress_until", c.state.suppressUntil,
		)
	}
	slog.Warn("All auto-suppressed providers unavailable, selecting earliest expiring", suppressedInfo...)
	return earliest.target, earliest.provCfg, nil
}

// SelectByName returns a specific upstream target by provider name if it exists in the matched route model.
func (s *Selector) SelectByName(cfg *config.ConfigStruct, serviceProtocol string, matched *config.CompiledRouteModel, requestedModel, providerName string) (*RouteTarget, *config.ProviderConfig, error) {
	for _, upstream := range matched.Upstreams {
		if upstream.Provider != providerName {
			continue
		}
		provCfg, exists := cfg.Provider[providerName]
		if !exists {
			break
		}
		if serviceProtocol != "" && !config.ProviderSupportsConfiguredProtocol(provCfg, serviceProtocol) {
			break
		}
		return buildRouteTarget(matched, upstream, requestedModel), provCfg, nil
	}
	return nil, nil, ErrProviderNotFound
}

func (s *Selector) buildCandidates(cfg *config.ConfigStruct, serviceProtocol string, matched *config.CompiledRouteModel, requestedModel string, exclude []string) []candidate {
	candidates := make([]candidate, 0, len(matched.Upstreams))
	for _, upstream := range matched.Upstreams {
		target := buildRouteTarget(matched, upstream, requestedModel)
		if slices.Contains(exclude, target.Key) {
			continue
		}
		provCfg, exists := cfg.Provider[upstream.Provider]
		if !exists {
			continue
		}
		if serviceProtocol != "" && !config.ProviderSupportsConfiguredProtocol(provCfg, serviceProtocol) {
			continue
		}
		st := s.states[upstream.Provider]
		if st == nil {
			continue
		}
		if target.UpstreamModel != "" && st.availableModels != nil && !st.availableModels[target.UpstreamModel] && matched.Wildcard {
			continue
		}
		candidates = append(candidates, candidate{target: target, provCfg: provCfg, state: st})
	}
	return candidates
}

func buildRouteTarget(matched *config.CompiledRouteModel, upstream config.CompiledRouteUpstream, requestedModel string) *RouteTarget {
	target := &RouteTarget{
		ProviderName:   upstream.Provider,
		RequestedModel: requestedModel,
		PublicModel:    matched.PublicModel,
		MatchedPattern: "",
		RenameModel:    upstream.RenameModel,
		Wildcard:       matched.Wildcard,
	}
	if matched.Wildcard {
		target.UpstreamModel = requestedModel
		target.Key = upstream.Provider + ":" + requestedModel
		target.MatchedPattern = matched.Pattern
		return target
	}
	target.UpstreamModel = upstream.UpstreamModel
	target.Key = upstream.Provider + ":" + upstream.UpstreamModel
	return target
}
