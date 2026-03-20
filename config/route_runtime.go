package config

import (
	"path"
	"slices"
	"strings"
)

const (
	RouteProtocolChat               = "chat"
	RouteProtocolResponsesStateless = "responses_stateless"
	RouteProtocolResponsesStateful  = "responses_stateful"
	RouteProtocolAnthropic          = "anthropic"
)

type CompiledRouteModel struct {
	Key           string
	Pattern       string
	PublicModel   string
	PromptEnabled bool
	SystemPrompt  string
	Wildcard      bool
	Specificity   routePatternSpecificity
	Upstreams     []CompiledRouteUpstream
}

type CompiledRouteUpstream struct {
	Provider      string
	UpstreamModel string
	RenameModel   bool
}

type routePatternSpecificity struct {
	literalCount  int
	wildcardCount int
}

func CompatibleRouteProtocols(prov *ProviderConfig) []string {
	return CandidateRouteProtocols(prov)
}

func CandidateRouteProtocols(prov *ProviderConfig) []string {
	if prov == nil {
		return nil
	}
	switch providerFamily(prov) {
	case ProviderProtocolAnthropic:
		return []string{RouteProtocolChat, RouteProtocolAnthropic}
	case ProviderProtocolOpenAI:
		return []string{
			RouteProtocolChat,
			RouteProtocolResponsesStateless,
			RouteProtocolResponsesStateful,
		}
	case ProviderProtocolQwen, ProviderProtocolCopilot, ProviderProtocolOllama:
		return []string{RouteProtocolChat}
	default:
		return nil
	}
}

func SupportedRouteProtocols(prov *ProviderConfig) []string {
	if prov == nil {
		return nil
	}

	candidates := CandidateRouteProtocols(prov)
	if len(candidates) == 0 {
		return nil
	}

	enabled := make(map[string]bool, len(prov.EnabledProtocols))
	for _, protocol := range prov.EnabledProtocols {
		enabled[normalizeRouteProtocol(protocol)] = true
	}
	disabled := make(map[string]bool, len(prov.DisabledProtocols))
	for _, protocol := range prov.DisabledProtocols {
		disabled[normalizeRouteProtocol(protocol)] = true
	}

	filtered := make([]string, 0, len(candidates))
	for _, protocol := range candidates {
		if len(enabled) > 0 && !enabled[protocol] {
			continue
		}
		if disabled[protocol] {
			continue
		}
		filtered = append(filtered, protocol)
	}
	return filtered
}

func ProviderSupportsConfiguredProtocol(prov *ProviderConfig, routeProtocol string) bool {
	return slices.Contains(SupportedRouteProtocols(prov), routeProtocol)
}

func IsResponsesRouteProtocol(routeProtocol string) bool {
	return routeProtocol == RouteProtocolResponsesStateless || routeProtocol == RouteProtocolResponsesStateful
}

func SupportedServiceProtocolsForConfiguredProtocol(routeProtocol string) []string {
	switch routeProtocol {
	case RouteProtocolChat:
		return []string{RouteProtocolChat}
	case RouteProtocolResponsesStateless:
		return []string{RouteProtocolResponsesStateless}
	case RouteProtocolResponsesStateful:
		return []string{RouteProtocolResponsesStateless, RouteProtocolResponsesStateful}
	case RouteProtocolAnthropic:
		return []string{RouteProtocolAnthropic}
	default:
		return nil
	}
}

func (r *RouteConfig) ConfiguredProtocol() string {
	return r.Protocol
}

func (r *RouteConfig) ServiceProtocols() []string {
	return append([]string(nil), r.serviceProtocols...)
}

func (r *RouteConfig) SupportsServiceProtocol(serviceProtocol string) bool {
	return slices.Contains(r.serviceProtocols, serviceProtocol)
}

func (r *RouteConfig) MatchModel(model string) *CompiledRouteModel {
	if exact := r.exactModels[model]; exact != nil {
		return exact
	}

	var matched *CompiledRouteModel
	for _, candidate := range r.wildcards {
		ok, err := path.Match(candidate.Pattern, model)
		if err != nil || !ok {
			continue
		}
		if matched == nil || comparePatternSpecificity(candidate.Specificity, matched.Specificity) > 0 {
			matched = candidate
		}
	}
	return matched
}

func (r *RouteConfig) PublicModels() []string {
	models := make([]string, 0, len(r.exactModels))
	for name := range r.exactModels {
		models = append(models, name)
	}
	slices.Sort(models)
	return models
}

func (r *RouteConfig) CompiledWildcardModels() []*CompiledRouteModel {
	return append([]*CompiledRouteModel(nil), r.wildcards...)
}

func (r *RouteConfig) ProviderNames() []string {
	seen := map[string]bool{}
	var names []string
	for _, model := range r.exactModels {
		for _, upstream := range model.Upstreams {
			if seen[upstream.Provider] {
				continue
			}
			seen[upstream.Provider] = true
			names = append(names, upstream.Provider)
		}
	}
	for _, model := range r.wildcards {
		for _, upstream := range model.Upstreams {
			if seen[upstream.Provider] {
				continue
			}
			seen[upstream.Provider] = true
			names = append(names, upstream.Provider)
		}
	}
	slices.Sort(names)
	return names
}

func comparePatternSpecificity(a, b routePatternSpecificity) int {
	if a.literalCount != b.literalCount {
		return a.literalCount - b.literalCount
	}
	if a.wildcardCount != b.wildcardCount {
		return b.wildcardCount - a.wildcardCount
	}
	return 0
}

func buildPatternSpecificity(pattern string) routePatternSpecificity {
	spec := routePatternSpecificity{}
	for _, ch := range pattern {
		if ch == '*' {
			spec.wildcardCount++
			continue
		}
		spec.literalCount++
	}
	return spec
}

func hasWildcardPattern(model string) bool {
	return strings.ContainsRune(model, '*')
}

func wildcardPatternsConflict(a, b string) bool {
	type state struct {
		i int
		j int
	}
	seen := map[state]bool{}
	var visit func(i, j int) bool
	visit = func(i, j int) bool {
		st := state{i: i, j: j}
		if seen[st] {
			return false
		}
		seen[st] = true

		for i < len(a) && j < len(b) && a[i] != '*' && b[j] != '*' {
			if a[i] != b[j] {
				return false
			}
			i++
			j++
		}
		if i == len(a) && j == len(b) {
			return true
		}
		if i < len(a) && a[i] == '*' {
			if visit(i+1, j) {
				return true
			}
			if j < len(b) && visit(i, j+1) {
				return true
			}
		}
		if j < len(b) && b[j] == '*' {
			if visit(i, j+1) {
				return true
			}
			if i < len(a) && visit(i+1, j) {
				return true
			}
		}
		return false
	}
	return visit(0, 0)
}

func providerFamily(prov *ProviderConfig) string {
	if prov == nil {
		return ""
	}
	if normalized := normalizeProviderProtocol(prov.Family); normalized != "" {
		return normalized
	}
	return normalizeProviderProtocol(prov.Protocol)
}
