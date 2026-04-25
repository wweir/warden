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
	ServiceProtocolEmbeddings       = "embeddings"
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
	return RouteProtocolsFromServiceProtocols(SupportedServiceProtocols(prov))
}

func SupportedRouteProtocols(prov *ProviderConfig) []string {
	return CandidateRouteProtocols(prov)
}

func ProviderSupportsConfiguredProtocol(prov *ProviderConfig, routeProtocol string) bool {
	return slices.Contains(SupportedRouteProtocols(prov), routeProtocol)
}

func SupportedDisplayProtocols(prov *ProviderConfig) []string {
	serviceProtocols := SupportedServiceProtocols(prov)
	if len(serviceProtocols) == 0 {
		return nil
	}
	displayProtocols := RouteProtocolsFromServiceProtocols(serviceProtocols)
	if slices.Contains(serviceProtocols, ServiceProtocolEmbeddings) {
		displayProtocols = append(displayProtocols, ServiceProtocolEmbeddings)
	}
	return displayProtocols
}

func SupportedServiceProtocols(prov *ProviderConfig) []string {
	if prov == nil {
		return nil
	}
	if normalized := normalizeConfiguredServiceProtocols(prov.ServiceProtocols); len(normalized) > 0 {
		return normalized
	}
	return DefaultServiceProtocols(prov)
}

func DefaultServiceProtocols(prov *ProviderConfig) []string {
	if prov == nil {
		return nil
	}
	switch providerAdapterProtocol(prov) {
	case ProviderProtocolAnthropic:
		return []string{RouteProtocolChat, RouteProtocolAnthropic}
	case ProviderProtocolOpenAI:
		supported := []string{
			RouteProtocolChat,
			RouteProtocolResponsesStateless,
			RouteProtocolResponsesStateful,
			ServiceProtocolEmbeddings,
		}
		if prov.AnthropicToChat {
			supported = append(supported, RouteProtocolAnthropic)
		}
		return supported
	case ProviderProtocolCopilot:
		return []string{RouteProtocolChat}
	default:
		return nil
	}
}

func ProviderSupportsServiceProtocol(prov *ProviderConfig, serviceProtocol string) bool {
	return slices.Contains(SupportedServiceProtocols(prov), serviceProtocol)
}

func ProviderSupportsAnyServiceProtocol(prov *ProviderConfig, serviceProtocols []string) bool {
	for _, serviceProtocol := range serviceProtocols {
		if ProviderSupportsServiceProtocol(prov, serviceProtocol) {
			return true
		}
	}
	return false
}

func IsResponsesRouteProtocol(routeProtocol string) bool {
	return routeProtocol == RouteProtocolResponsesStateless || routeProtocol == RouteProtocolResponsesStateful
}

func SupportedServiceProtocolsForConfiguredProtocol(routeProtocol string) []string {
	switch routeProtocol {
	case RouteProtocolChat:
		return []string{RouteProtocolChat, ServiceProtocolEmbeddings}
	case RouteProtocolResponsesStateless:
		return []string{RouteProtocolResponsesStateless, ServiceProtocolEmbeddings}
	case RouteProtocolResponsesStateful:
		return []string{RouteProtocolResponsesStateless, RouteProtocolResponsesStateful, ServiceProtocolEmbeddings}
	case RouteProtocolAnthropic:
		return []string{RouteProtocolAnthropic, ServiceProtocolEmbeddings}
	default:
		return nil
	}
}

func configuredRouteServiceProtocols(route *RouteConfig) []string {
	if route == nil {
		return nil
	}
	if normalized := normalizeConfiguredServiceProtocols(route.ServiceProtocols); len(normalized) > 0 {
		return normalized
	}
	return SupportedServiceProtocolsForConfiguredProtocol(route.Protocol)
}

func RouteProtocolsFromServiceProtocols(serviceProtocols []string) []string {
	if len(serviceProtocols) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var routeProtocols []string
	add := func(protocol string) {
		if protocol == "" || seen[protocol] {
			return
		}
		seen[protocol] = true
		routeProtocols = append(routeProtocols, protocol)
	}
	for _, protocol := range serviceProtocols {
		switch protocol {
		case RouteProtocolChat:
			add(RouteProtocolChat)
		case RouteProtocolResponsesStateless:
			add(RouteProtocolResponsesStateless)
		case RouteProtocolResponsesStateful:
			add(RouteProtocolResponsesStateless)
			add(RouteProtocolResponsesStateful)
		case RouteProtocolAnthropic:
			add(RouteProtocolAnthropic)
		}
	}
	return routeProtocols
}

func (r *RouteConfig) ConfiguredProtocol() string {
	return r.Protocol
}

func (r *RouteConfig) EffectiveServiceProtocols() []string {
	return append([]string(nil), r.serviceProtocols...)
}

func (r *RouteConfig) SupportsServiceProtocol(serviceProtocol string) bool {
	return slices.Contains(r.serviceProtocols, serviceProtocol)
}

func RouteHasServiceProtocolSupport(route *RouteConfig, providers map[string]*ProviderConfig, serviceProtocol string) bool {
	if route == nil || len(providers) == 0 {
		return false
	}
	for _, model := range route.exactModels {
		for _, upstream := range model.Upstreams {
			if ProviderSupportsServiceProtocol(providers[upstream.Provider], serviceProtocol) {
				return true
			}
		}
	}
	for _, model := range route.wildcards {
		for _, upstream := range model.Upstreams {
			if ProviderSupportsServiceProtocol(providers[upstream.Provider], serviceProtocol) {
				return true
			}
		}
	}
	return false
}

func PruneUnsupportedRouteServiceProtocols(route *RouteConfig, providers map[string]*ProviderConfig) {
	if route == nil || len(route.serviceProtocols) == 0 {
		return
	}
	result := route.serviceProtocols[:0]
	for _, serviceProtocol := range route.serviceProtocols {
		if RouteHasServiceProtocolSupport(route, providers, serviceProtocol) {
			result = append(result, serviceProtocol)
		}
	}
	route.serviceProtocols = result
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

func providerAdapterProtocol(prov *ProviderConfig) string {
	if prov == nil {
		return ""
	}
	if normalized := normalizeProviderAdapterProtocol(prov.Family); normalized != "" {
		return normalized
	}
	return normalizeProviderAdapterProtocol(prov.Protocol)
}

func normalizeConfiguredServiceProtocols(protocols []string) []string {
	if len(protocols) == 0 {
		return nil
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(protocols))
	add := func(protocol string) {
		if protocol == "" || seen[protocol] {
			return
		}
		seen[protocol] = true
		result = append(result, protocol)
	}
	for _, raw := range protocols {
		protocol := normalizeRouteProtocol(raw)
		switch protocol {
		case RouteProtocolChat, RouteProtocolResponsesStateless, RouteProtocolResponsesStateful, RouteProtocolAnthropic, ServiceProtocolEmbeddings:
			add(protocol)
			if protocol == RouteProtocolResponsesStateful {
				add(RouteProtocolResponsesStateless)
			}
		}
	}
	return result
}

func validConfiguredServiceProtocols(protocols []string) bool {
	for _, raw := range protocols {
		switch normalizeRouteProtocol(raw) {
		case RouteProtocolChat, RouteProtocolResponsesStateless, RouteProtocolResponsesStateful, RouteProtocolAnthropic, ServiceProtocolEmbeddings:
		default:
			return false
		}
	}
	return true
}
