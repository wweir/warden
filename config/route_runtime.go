package config

import (
	"net/url"
	"slices"
	"strings"
)

const (
	RouteProtocolChat         = "chat"
	RouteProtocolResponses    = "responses"
	RouteProtocolAnthropic    = "anthropic"
	ServiceProtocolEmbeddings = "embeddings"
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

// ProviderEndpoint is a normalized view of a provider's endpoint,
// whether from explicit endpoint.* or from shorthand provider-level fields.
type ProviderEndpoint struct {
	Name                 string
	URL                  string
	Format               string
	Protocols            []string
	Headers              map[string]string
	Models               []string
	ResponsesToChat      bool
	AnthropicToChat      bool
	AnthropicToResponses bool
}

// ProviderEndpoints expands a provider config into its effective endpoints.
// If no explicit endpoints are declared, provider-level url/format/protocols
// become a single implicit "default" endpoint.
func ProviderEndpoints(prov *ProviderConfig) []*ProviderEndpoint {
	if prov == nil {
		return nil
	}

	if len(prov.Endpoints) > 0 {
		names := make([]string, 0, len(prov.Endpoints))
		for name := range prov.Endpoints {
			names = append(names, name)
		}
		slices.Sort(names)

		result := make([]*ProviderEndpoint, 0, len(names))
		for _, name := range names {
			ep := prov.Endpoints[name]
			if ep == nil {
				continue
			}
			format := normalizeProviderFormat(ep.Format)
			if format == "" {
				format = ProviderFormatOpenAI
			}
			result = append(result, &ProviderEndpoint{
				Name:                 name,
				URL:                  ep.URL,
				Format:               format,
				Protocols:            endpointProtocols(ep, format),
				Headers:              mergeHeaders(prov.Headers, ep.Headers),
				Models:               firstNonEmpty(ep.Models, prov.Models),
				ResponsesToChat:      ep.ResponsesToChat,
				AnthropicToChat:      ep.AnthropicToChat,
				AnthropicToResponses: ep.AnthropicToResponses,
			})
		}
		return result
	}

	// Shorthand: provider-level fields become a single default endpoint.
	format := normalizeProviderFormat(prov.Format)
	if format == "" {
		format = ProviderFormatOpenAI
	}
	return []*ProviderEndpoint{{
		Name:                 "default",
		URL:                  prov.URL,
		Format:               format,
		Protocols:            shorthandProtocols(prov, format),
		Headers:              copyMap(prov.Headers),
		Models:               prov.Models,
		ResponsesToChat:      prov.ResponsesToChat,
		AnthropicToChat:      prov.AnthropicToChat,
		AnthropicToResponses: prov.AnthropicToResponses,
	}}
}

func endpointProtocols(ep *ProviderEndpointConfig, format string) []string {
	if normalized := normalizeConfiguredServiceProtocols(ep.Protocols); len(normalized) > 0 {
		return withBridgeProtocols(normalized, format, ep.ResponsesToChat, ep.AnthropicToChat, ep.AnthropicToResponses)
	}
	return defaultProtocolsForFormat(format, ep.ResponsesToChat, ep.AnthropicToChat, ep.AnthropicToResponses)
}

func shorthandProtocols(prov *ProviderConfig, format string) []string {
	if normalized := normalizeConfiguredServiceProtocols(prov.Protocols); len(normalized) > 0 {
		return withBridgeProtocols(normalized, format, prov.ResponsesToChat, prov.AnthropicToChat, prov.AnthropicToResponses)
	}
	return defaultProtocolsForFormat(format, prov.ResponsesToChat, prov.AnthropicToChat, prov.AnthropicToResponses)
}

func defaultProtocolsForFormat(format string, responsesToChat, anthropicToChat, anthropicToResponses bool) []string {
	switch format {
	case ProviderFormatAnthropic:
		supported := []string{RouteProtocolChat, RouteProtocolAnthropic}
		if anthropicToResponses {
			supported = append(supported, RouteProtocolResponses)
		}
		return supported
	case ProviderFormatCopilot:
		return []string{RouteProtocolChat}
	default: // openai
		supported := []string{
			RouteProtocolChat,
			RouteProtocolResponses,
			ServiceProtocolEmbeddings,
		}
		if anthropicToChat {
			supported = append(supported, RouteProtocolAnthropic)
		}
		return supported
	}
}

func withBridgeProtocols(base []string, format string, responsesToChat, anthropicToChat, anthropicToResponses bool) []string {
	seen := map[string]bool{}
	for _, p := range base {
		seen[p] = true
	}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			base = append(base, p)
		}
	}
	switch format {
	case ProviderFormatOpenAI:
		if anthropicToChat {
			add(RouteProtocolAnthropic)
		}
	case ProviderFormatAnthropic:
		if anthropicToResponses {
			add(RouteProtocolResponses)
		}
	}
	return base
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
	endpoints := ProviderEndpoints(prov)
	seen := map[string]bool{}
	var protocols []string
	for _, ep := range endpoints {
		for _, p := range ep.Protocols {
			if !seen[p] {
				seen[p] = true
				protocols = append(protocols, p)
			}
		}
	}
	return protocols
}

// DefaultServiceProtocols returns the default service protocols for a provider.
// Kept for backward compatibility; prefer SupportedServiceProtocols.
func DefaultServiceProtocols(prov *ProviderConfig) []string {
	if prov == nil {
		return nil
	}
	endpoints := ProviderEndpoints(prov)
	if len(endpoints) == 1 {
		return endpoints[0].Protocols
	}
	return SupportedServiceProtocols(prov)
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

func SupportedServiceProtocolsForConfiguredProtocol(routeProtocol string) []string {
	switch routeProtocol {
	case RouteProtocolChat:
		return []string{RouteProtocolChat, ServiceProtocolEmbeddings}
	case RouteProtocolResponses:
		return []string{RouteProtocolResponses, ServiceProtocolEmbeddings}
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
		case RouteProtocolResponses:
			add(RouteProtocolResponses)
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
		if !matchWildcardModelPattern(candidate.Pattern, model) {
			continue
		}
		if matched == nil || comparePatternSpecificity(candidate.Specificity, matched.Specificity) > 0 {
			matched = candidate
		}
	}
	return matched
}

// MatchWildcardModel reports whether a compiled wildcard model pattern matches the complete model ID.
func (r *RouteConfig) MatchWildcardModel(pattern, model string) bool {
	for _, wildcard := range r.wildcards {
		if wildcard.Pattern != pattern {
			continue
		}
		return matchWildcardModelPattern(pattern, model)
	}
	return false
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

func matchWildcardModelPattern(pattern, model string) bool {
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

		for i < len(pattern) && j < len(model) && pattern[i] != '*' {
			if pattern[i] != model[j] {
				return false
			}
			i++
			j++
		}
		if i == len(pattern) {
			return j == len(model)
		}
		if pattern[i] != '*' {
			return false
		}
		if visit(i+1, j) {
			return true
		}
		if j < len(model) && visit(i, j+1) {
			return true
		}
		return false
	}
	return visit(0, 0)
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

// ProviderFormats returns the list of enabled formats for a provider.
// Formats are returned in a stable priority order: openai, anthropic, copilot.
func ProviderFormats(prov *ProviderConfig) []string {
	if prov == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, ep := range ProviderEndpoints(prov) {
		if ep.Format != "" {
			seen[ep.Format] = true
		}
	}

	order := []string{ProviderFormatOpenAI, ProviderFormatAnthropic, ProviderFormatCopilot}
	var formats []string
	for _, f := range order {
		if seen[f] {
			formats = append(formats, f)
		}
	}
	return formats
}

// FormatServiceProtocols returns the service protocols supported by a specific format.
// If multiple endpoints share the same format, returns the union of their protocols.
func FormatServiceProtocols(prov *ProviderConfig, format string) []string {
	if prov == nil {
		return nil
	}
	seen := map[string]bool{}
	var protocols []string
	for _, ep := range ProviderEndpoints(prov) {
		if ep.Format != format {
			continue
		}
		for _, p := range ep.Protocols {
			if !seen[p] {
				seen[p] = true
				protocols = append(protocols, p)
			}
		}
	}
	return protocols
}

// FormatEffectiveURL returns the effective base URL for a format.
// If multiple endpoints share the same format, returns the first match.
func FormatEffectiveURL(prov *ProviderConfig, format string) string {
	if prov == nil {
		return ""
	}
	for _, ep := range ProviderEndpoints(prov) {
		if ep.Format == format {
			return ep.URL
		}
	}
	return ""
}

// ProviderFormatForURL returns the format whose configured endpoint URL best matches targetURL.
// It falls back to the provider's legacy format when no endpoint matches.
func ProviderFormatForURL(prov *ProviderConfig, targetURL string) string {
	if prov == nil {
		return ""
	}
	if ep := ProviderEndpointForURL(prov, targetURL); ep != nil && ep.Format != "" {
		return ep.Format
	}
	if prov.Format != "" {
		return prov.Format
	}
	return ProviderFormatOpenAI
}

// ProviderEndpointForURL returns the effective endpoint whose URL best matches targetURL.
// It prefers the longest URL prefix match and ignores trailing-slash ambiguity.
func ProviderEndpointForURL(prov *ProviderConfig, targetURL string) *ProviderEndpoint {
	if prov == nil || targetURL == "" {
		return nil
	}

	bestLen := 0
	var best *ProviderEndpoint
	for _, ep := range ProviderEndpoints(prov) {
		if ep == nil || ep.URL == "" {
			continue
		}
		if !urlPrefixMatches(targetURL, ep.URL) {
			continue
		}
		if l := len(strings.TrimRight(ep.URL, "/")); l > bestLen {
			bestLen = l
			best = ep
		}
	}
	return best
}

// ProviderHeadersForURL returns the effective headers for the endpoint that best matches targetURL.
// It falls back to provider-level headers when no endpoint-specific match exists.
func ProviderHeadersForURL(prov *ProviderConfig, targetURL string) map[string]string {
	if prov == nil {
		return nil
	}
	if ep := ProviderEndpointForURL(prov, targetURL); ep != nil {
		return copyMap(ep.Headers)
	}
	return copyMap(prov.Headers)
}

// FormatModels returns the model list for a specific format.
// If multiple endpoints share the same format, returns the first match.
func FormatModels(prov *ProviderConfig, format string) []string {
	if prov == nil {
		return nil
	}
	for _, ep := range ProviderEndpoints(prov) {
		if ep.Format == format {
			return ep.Models
		}
	}
	return nil
}

// FormatHeaders returns the headers for a specific format,
// merged with provider-level headers.
// If multiple endpoints share the same format, returns the first match.
func FormatHeaders(prov *ProviderConfig, format string) map[string]string {
	if prov == nil {
		return nil
	}
	for _, ep := range ProviderEndpoints(prov) {
		if ep.Format == format {
			if len(ep.Headers) == 0 {
				return nil
			}
			return copyMap(ep.Headers)
		}
	}
	return nil
}

// FormatHasBridge returns whether a specific bridge is enabled for a format.
// If multiple endpoints share the same format, returns true if any endpoint has it enabled.
func FormatHasBridge(prov *ProviderConfig, format string, bridgeType string) bool {
	if prov == nil {
		return false
	}
	for _, ep := range ProviderEndpoints(prov) {
		if ep.Format != format {
			continue
		}
		switch bridgeType {
		case "responses_to_chat":
			if ep.ResponsesToChat {
				return true
			}
		case "anthropic_to_chat":
			if ep.AnthropicToChat {
				return true
			}
		case "anthropic_to_responses":
			if ep.AnthropicToResponses {
				return true
			}
		}
	}
	return false
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
		case RouteProtocolChat, RouteProtocolResponses, RouteProtocolAnthropic, ServiceProtocolEmbeddings:
			add(protocol)
		}
	}
	return result
}

func validConfiguredServiceProtocols(protocols []string) bool {
	for _, raw := range protocols {
		switch normalizeRouteProtocol(raw) {
		case RouteProtocolChat, RouteProtocolResponses, RouteProtocolAnthropic, ServiceProtocolEmbeddings:
		default:
			return false
		}
	}
	return true
}

func mergeHeaders(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	result := make(map[string]string)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func urlPrefixMatches(targetURL, baseURL string) bool {
	targetURL = strings.TrimSpace(targetURL)
	baseURL = strings.TrimSpace(baseURL)
	if targetURL == "" || baseURL == "" {
		return false
	}

	targetParsed, targetErr := url.Parse(targetURL)
	baseParsed, baseErr := url.Parse(baseURL)
	if targetErr == nil && baseErr == nil && targetParsed.Scheme != "" && baseParsed.Scheme != "" {
		if !strings.EqualFold(targetParsed.Scheme, baseParsed.Scheme) || !strings.EqualFold(targetParsed.Host, baseParsed.Host) {
			return false
		}
		targetPath := strings.TrimRight(targetParsed.Path, "/")
		basePath := strings.TrimRight(baseParsed.Path, "/")
		if targetPath == basePath {
			return true
		}
		if !strings.HasPrefix(targetPath, basePath) {
			return false
		}
		return len(targetPath) > len(basePath) && targetPath[len(basePath)] == '/'
	}

	targetURL = strings.TrimRight(targetURL, "/")
	baseURL = strings.TrimRight(baseURL, "/")
	if targetURL == baseURL {
		return true
	}
	if !strings.HasPrefix(targetURL, baseURL) {
		return false
	}
	return len(targetURL) > len(baseURL) && targetURL[len(baseURL)] == '/'
}

func copyMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func firstNonEmpty(a, b []string) []string {
	if len(a) > 0 {
		return a
	}
	return b
}
