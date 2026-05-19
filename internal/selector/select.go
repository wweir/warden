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
	s.mu.Lock()
	defer s.mu.Unlock()

	candidates := s.buildCandidates(cfg, serviceProtocol, matched, requestedModel, exclude)
	if len(candidates) == 0 {
		return nil, nil, ErrProviderNotFound
	}

	now := time.Now()
	for _, c := range candidates {
		if c.state.manualSuppress {
			continue
		}
		modeSuppress := c.state.modeSuppressUntil[c.target.Format]
		if modeSuppress.IsZero() {
			modeSuppress = c.state.suppressUntil
		}
		if modeSuppress.IsZero() || now.After(modeSuppress) {
			return c.target, c.provCfg, nil
		}
	}

	autoSuppressed := make([]candidate, 0, len(candidates))
	manuallySuppressed := 0
	for _, c := range candidates {
		if c.state.manualSuppress {
			manuallySuppressed++
			continue
		}
		autoSuppressed = append(autoSuppressed, c)
	}
	if len(autoSuppressed) == 0 {
		return nil, nil, ErrProviderNotFound
	}
	if manuallySuppressed > 0 {
		released := make([]string, 0, len(autoSuppressed))
		for _, c := range autoSuppressed {
			modeSuppress := c.state.modeSuppressUntil[c.target.Format]
			if !modeSuppress.IsZero() {
				c.state.modeSuppressUntil[c.target.Format] = time.Time{}
				released = append(released, c.target.ProviderName+"/"+c.target.Format)
			} else if !c.state.suppressUntil.IsZero() {
				c.state.suppressUntil = time.Time{}
				c.state.consecutiveFailures = 0
				released = append(released, c.target.ProviderName)
			}
		}
		if len(released) > 0 {
			slog.Warn("Released auto suppression because manual suppression left no available provider",
				"route_model", matched.PublicModel,
				"requested_model", requestedModel,
				"providers", released,
			)
		}
		return autoSuppressed[0].target, autoSuppressed[0].provCfg, nil
	}

	earliest := autoSuppressed[0]
	for _, c := range autoSuppressed[1:] {
		modeSuppress := c.state.modeSuppressUntil[c.target.Format]
		if modeSuppress.IsZero() {
			modeSuppress = c.state.suppressUntil
		}
		earliestSuppress := earliest.state.modeSuppressUntil[earliest.target.Format]
		if earliestSuppress.IsZero() {
			earliestSuppress = earliest.state.suppressUntil
		}
		if modeSuppress.Before(earliestSuppress) {
			earliest = c
		}
	}
	suppressedInfo := make([]any, 0, len(autoSuppressed)*4+2)
	earliestModeSuppress := earliest.state.modeSuppressUntil[earliest.target.Format]
	suppressedInfo = append(suppressedInfo, "selected", earliest.target.ProviderName, "format", earliest.target.Format, "suppress_until", earliestModeSuppress)
	for _, c := range autoSuppressed {
		modeSuppress := c.state.modeSuppressUntil[c.target.Format]
		modeFailures := c.state.modeConsecutiveFailures[c.target.Format]
		if modeSuppress.IsZero() {
			modeSuppress = c.state.suppressUntil
			modeFailures = c.state.consecutiveFailures
		}
		suppressedInfo = append(suppressedInfo,
			c.target.ProviderName+"/"+c.target.Format+"_failures", modeFailures,
			c.target.ProviderName+"/"+c.target.Format+"_suppress_until", modeSuppress,
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
		if serviceProtocol != "" && !config.ProviderSupportsServiceProtocol(provCfg, serviceProtocol) {
			break
		}
		format := inferFormatFromProvider(provCfg, serviceProtocol)
		target := buildRouteTarget(matched, upstream, requestedModel, format)
		s.assignTargetURL(target, provCfg)
		return target, provCfg, nil
	}
	return nil, nil, ErrProviderNotFound
}

// CountAvailableProviders returns the number of route candidates that are not manually suppressed.
// Automatic suppression windows are intentionally ignored so callers can decide whether to bypass them.
func (s *Selector) CountAvailableProviders(cfg *config.ConfigStruct, serviceProtocol string, matched *config.CompiledRouteModel, requestedModel string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, c := range s.buildCandidates(cfg, serviceProtocol, matched, requestedModel, nil) {
		if c.state.manualSuppress {
			continue
		}
		count++
	}
	return count
}

func (s *Selector) buildCandidates(cfg *config.ConfigStruct, serviceProtocol string, matched *config.CompiledRouteModel, requestedModel string, exclude []string) []candidate {
	candidates := make([]candidate, 0, len(matched.Upstreams))
	for _, upstream := range matched.Upstreams {
		provCfg, exists := cfg.Provider[upstream.Provider]
		if !exists {
			continue
		}
		if serviceProtocol != "" && !config.ProviderSupportsServiceProtocol(provCfg, serviceProtocol) {
			continue
		}
		st := s.states[upstream.Provider]
		if st == nil {
			continue
		}

		format := inferFormatFromProvider(provCfg, serviceProtocol)
		target := buildRouteTarget(matched, upstream, requestedModel, format)
		s.assignTargetURL(target, provCfg)

		if slices.Contains(exclude, target.Key) {
			continue
		}
		if target.UpstreamModel != "" && st.availableModels != nil && !st.availableModels[target.UpstreamModel] && matched.Wildcard {
			continue
		}
		candidates = append(candidates, candidate{target: target, provCfg: provCfg, state: st})
	}
	return candidates
}

// inferFormatFromProvider determines the format to use for a given service protocol.
func inferFormatFromProvider(provCfg *config.ProviderConfig, serviceProtocol string) string {
	if provCfg == nil {
		return ""
	}

	formats := config.ProviderFormats(provCfg)
	if len(formats) > 0 {
		if len(formats) == 1 {
			return formats[0]
		}
		for _, format := range formats {
			protocols := config.FormatServiceProtocols(provCfg, format)
			for _, p := range protocols {
				if p == serviceProtocol {
					return format
				}
			}
		}
		return formats[0]
	}
	return provCfg.Format
}

func buildRouteTarget(matched *config.CompiledRouteModel, upstream config.CompiledRouteUpstream, requestedModel string, format string) *RouteTarget {
	target := &RouteTarget{
		ProviderName:   upstream.Provider,
		URL:            "",
		Format:         format,
		RequestedModel: requestedModel,
		PublicModel:    matched.PublicModel,
		MatchedPattern: "",
		RenameModel:    upstream.RenameModel,
		Wildcard:       matched.Wildcard,
	}
	if matched.Wildcard {
		target.UpstreamModel = requestedModel
		target.Key = buildTargetKey(upstream.Provider, format, requestedModel)
		target.MatchedPattern = matched.Pattern
		return target
	}
	target.UpstreamModel = upstream.UpstreamModel
	target.Key = buildTargetKey(upstream.Provider, format, upstream.UpstreamModel)
	return target
}

func (s *Selector) assignTargetURL(target *RouteTarget, provCfg *config.ProviderConfig) {
	if target == nil || provCfg == nil {
		return
	}
	if target.Format != "" {
		target.URL = config.FormatEffectiveURL(provCfg, target.Format)
	}
	if target.URL == "" {
		target.URL = provCfg.URL
	}
}

func buildTargetKey(provider, format, model string) string {
	if format != "" {
		return provider + ":" + format + ":" + model
	}
	return provider + ":" + model
}
