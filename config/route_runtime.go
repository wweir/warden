package config

import (
	"path"
	"slices"
	"strings"
)

const (
	RouteProtocolChat      = "chat"
	RouteProtocolResponses = "responses"
	RouteProtocolAnthropic = "anthropic"
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

func SupportedRouteProtocols(providerProtocol string) []string {
	switch providerProtocol {
	case "anthropic":
		return []string{RouteProtocolAnthropic}
	case "openai", "ollama", "qwen", "copilot":
		return []string{RouteProtocolChat, RouteProtocolResponses}
	default:
		return nil
	}
}

func ProviderSupportsRouteProtocol(providerProtocol, routeProtocol string) bool {
	return slices.Contains(SupportedRouteProtocols(providerProtocol), routeProtocol)
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
	return r.wildcards
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
