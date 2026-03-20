package selector

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/pkg/protocol/anthropic"
)

const (
	baseSuppressDuration   = 30 * time.Second
	maxConsecutiveFailures = 5
	outcomeWindowSize      = 1000
	maxSuppressReasons     = 20
	suppressReasonTTL      = time.Hour
)

type outcome struct {
	timestamp   time.Time
	success     bool
	latencyMs   int64
	errorSource string
}

// SuppressReason records the cause and time of a provider suppression event.
type SuppressReason struct {
	Time   time.Time `json:"time"`
	Reason string    `json:"reason"`
}

// providerState tracks runtime health state for a single provider.
type providerState struct {
	consecutiveFailures int
	suppressUntil       time.Time
	manualSuppress      bool // manually suppressed by admin
	availableModels     map[string]bool
	rawModels           []json.RawMessage
	displayProtocols    []string
	lastProtocolProbe   *ProtocolProbe
	modelProtocolProbes map[string]map[string]ModelProtocolProbe

	outcomes     []outcome
	outcomeStart int

	suppressReasons []SuppressReason

	preStreamErrors int64
	inStreamErrors  int64
	failoverCount   int64
}

type ProtocolProbe struct {
	CheckedAt time.Time `json:"checked_at"`
	Status    string    `json:"status"`
	Source    string    `json:"source"`
	Error     string    `json:"error,omitempty"`
}

type ModelProtocolProbe struct {
	Model     string    `json:"model"`
	Protocol  string    `json:"protocol"`
	CheckedAt time.Time `json:"checked_at"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
}

func (s *providerState) recordOutcome(success bool, latencyMs int64, errorSource string) {
	if len(s.outcomes) < outcomeWindowSize {
		s.outcomes = append(s.outcomes, outcome{
			timestamp:   time.Now(),
			success:     success,
			latencyMs:   latencyMs,
			errorSource: errorSource,
		})
	} else {
		s.outcomes[s.outcomeStart] = outcome{
			timestamp:   time.Now(),
			success:     success,
			latencyMs:   latencyMs,
			errorSource: errorSource,
		}
		s.outcomeStart = (s.outcomeStart + 1) % outcomeWindowSize
	}
}

func (s *providerState) windowStats() (total, success, failure int, avgLatencyMs float64) {
	total = len(s.outcomes)
	if total == 0 {
		return 0, 0, 0, 0
	}
	var totalLatency int64
	for _, o := range s.outcomes {
		if o.success {
			success++
		} else {
			failure++
		}
		totalLatency += o.latencyMs
	}
	avgLatencyMs = float64(totalLatency) / float64(total)
	return total, success, failure, avgLatencyMs
}

// Selector selects the best provider for a request based on config order,
// model matching, and failure suppression.
type Selector struct {
	mu     sync.RWMutex
	states map[string]*providerState
}

type RouteTarget struct {
	Key            string
	ProviderName   string
	UpstreamModel  string
	PublicModel    string
	RequestedModel string
	MatchedPattern string
	RenameModel    bool
	Wildcard       bool
}

// NewSelector creates a new Selector and initializes state for all providers.
func NewSelector(cfg *config.ConfigStruct) *Selector {
	states := make(map[string]*providerState, len(cfg.Provider))
	for name, prov := range cfg.Provider {
		states[name] = &providerState{
			displayProtocols: append([]string(nil), config.SupportedRouteProtocols(prov)...),
		}
	}
	return &Selector{states: states}
}

// Select returns the best upstream target for the given matched route model.
func (s *Selector) Select(cfg *config.ConfigStruct, serviceProtocol string, matched *config.CompiledRouteModel, requestedModel string, exclude ...string) (*RouteTarget, *config.ProviderConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()

	type candidate struct {
		target  *RouteTarget
		provCfg *config.ProviderConfig
		state   *providerState
	}
	var candidates []candidate
	for _, upstream := range matched.Upstreams {
		target := &RouteTarget{
			Key:            upstream.Provider + ":" + requestedModel,
			ProviderName:   upstream.Provider,
			UpstreamModel:  upstream.UpstreamModel,
			PublicModel:    matched.PublicModel,
			RequestedModel: requestedModel,
			MatchedPattern: "",
			RenameModel:    upstream.RenameModel,
			Wildcard:       matched.Wildcard,
		}
		if matched.Wildcard {
			target.Key = upstream.Provider + ":" + requestedModel
			target.UpstreamModel = requestedModel
			target.MatchedPattern = matched.Pattern
		} else {
			target.Key = upstream.Provider + ":" + upstream.UpstreamModel
		}
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
		upstreamModel := target.UpstreamModel
		if upstreamModel != "" && st.availableModels != nil && !st.availableModels[upstreamModel] {
			if matched.Wildcard {
				continue
			}
		}
		candidates = append(candidates, candidate{target: target, provCfg: provCfg, state: st})
	}

	for _, c := range candidates {
		if c.state.manualSuppress {
			continue // skip manually suppressed providers
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

	if len(autoSuppressed) > 0 {
		earliest := autoSuppressed[0]
		for _, c := range autoSuppressed[1:] {
			if c.state.suppressUntil.Before(earliest.state.suppressUntil) {
				earliest = c
			}
		}
		suppressedInfo := make([]any, 0, len(autoSuppressed)*4+2)
		suppressedInfo = append(suppressedInfo, "selected", earliest.target.ProviderName, "suppress_until", earliest.state.suppressUntil)
		for _, c := range autoSuppressed {
			suppressedInfo = append(suppressedInfo, c.target.ProviderName+"_failures", c.state.consecutiveFailures,
				c.target.ProviderName+"_suppress_until", c.state.suppressUntil)
		}
		slog.Warn("All auto-suppressed providers unavailable, selecting earliest expiring", suppressedInfo...)
		return earliest.target, earliest.provCfg, nil
	}

	return nil, nil, ErrProviderNotFound
}

// SelectByName returns a specific upstream target by provider name if it exists in the matched route model.
func (s *Selector) SelectByName(cfg *config.ConfigStruct, serviceProtocol string, matched *config.CompiledRouteModel, requestedModel, providerName string) (*RouteTarget, *config.ProviderConfig, error) {
	for _, upstream := range matched.Upstreams {
		if upstream.Provider == providerName {
			provCfg, exists := cfg.Provider[providerName]
			if !exists {
				break
			}
			if serviceProtocol != "" && !config.ProviderSupportsConfiguredProtocol(provCfg, serviceProtocol) {
				break
			}
			target := &RouteTarget{
				ProviderName:   providerName,
				RequestedModel: requestedModel,
				PublicModel:    matched.PublicModel,
				MatchedPattern: "",
				RenameModel:    upstream.RenameModel,
				Wildcard:       matched.Wildcard,
			}
			if matched.Wildcard {
				target.UpstreamModel = requestedModel
				target.Key = providerName + ":" + requestedModel
				target.MatchedPattern = matched.Pattern
			} else {
				target.UpstreamModel = upstream.UpstreamModel
				target.Key = providerName + ":" + upstream.UpstreamModel
			}
			return target, provCfg, nil
		}
	}
	return nil, nil, ErrProviderNotFound
}

// RecordOutcome records the result of an upstream request.
func (s *Selector) RecordOutcome(name string, err error, latency time.Duration) {
	s.RecordOutcomeWithSource(name, err, latency, "")
}

// RecordOutcomeWithSource records the result of an upstream request with error source tracking.
func (s *Selector) RecordOutcomeWithSource(name string, err error, latency time.Duration, errorSource string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, exists := s.states[name]
	if !exists {
		return
	}

	latencyMs := latency.Milliseconds()

	if err == nil {
		st.recordOutcome(true, latencyMs, errorSource)
		st.consecutiveFailures = 0
		st.suppressUntil = time.Time{}
		return
	}

	if ue, ok := err.(*UpstreamError); ok && !ue.IsRetryable() {
		return
	}

	st.recordOutcome(false, latencyMs, errorSource)
	st.consecutiveFailures++
	if st.consecutiveFailures > maxConsecutiveFailures {
		st.consecutiveFailures = maxConsecutiveFailures
	}

	switch errorSource {
	case "pre_stream":
		st.preStreamErrors++
	case "in_stream":
		st.inStreamErrors++
	}

	duration := baseSuppressDuration << (st.consecutiveFailures - 1)
	st.suppressUntil = time.Now().Add(duration)

	reason := err.Error()
	if ue, ok := err.(*UpstreamError); ok {
		body := ue.Body
		if len(body) > 200 {
			body = body[:200]
		}
		reason = fmt.Sprintf("HTTP %d: %s", ue.Code, body)
	}
	now := time.Now()
	cutoff := now.Add(-suppressReasonTTL)
	n := 0
	for _, r := range st.suppressReasons {
		if r.Time.After(cutoff) {
			st.suppressReasons[n] = r
			n++
		}
	}
	st.suppressReasons = st.suppressReasons[:n]
	st.suppressReasons = append(st.suppressReasons, SuppressReason{Time: now, Reason: reason})
	if len(st.suppressReasons) > maxSuppressReasons {
		st.suppressReasons = st.suppressReasons[len(st.suppressReasons)-maxSuppressReasons:]
	}

	attrs := []any{
		"name", name,
		"consecutive_failures", st.consecutiveFailures,
		"suppress_duration", duration,
		"error_source", errorSource,
	}
	if ue, ok := err.(*UpstreamError); ok {
		body := ue.Body
		if len(body) > 200 {
			body = body[:200] + "..."
		}
		attrs = append(attrs, "status", ue.Code, "body", body)
	} else {
		attrs = append(attrs, "error", err)
	}
	slog.Warn("Provider suppressed", attrs...)
}

// RefreshModels queries GET /models for all providers in parallel.
// When a provider has static models configured, they are set immediately as a
// baseline, and a remote fetch is still attempted to discover additional models.
func (s *Selector) RefreshModels(cfg *config.ConfigStruct) {
	var wg sync.WaitGroup
	for name, provCfg := range cfg.Provider {
		// Pre-populate configured models so they are available immediately.
		if len(provCfg.Models) > 0 {
			models := make(map[string]bool, len(provCfg.Models))
			rawModels := make([]json.RawMessage, 0, len(provCfg.Models))
			for _, id := range provCfg.Models {
				models[id] = true
				rawModels = append(rawModels, mustMarshal(map[string]string{
					"id": id, "object": "model", "owned_by": name,
				}))
			}
			s.mu.Lock()
			if st, ok := s.states[name]; ok {
				st.availableModels = models
				st.rawModels = rawModels
			}
			s.mu.Unlock()
			slog.Info("Models loaded from config", "provider", name, "count", len(models))
		}

		wg.Add(1)
		go func(name string, provCfg *config.ProviderConfig) {
			defer wg.Done()

			fetched, fetchedRaw, err := FetchModels(provCfg)
			if err != nil {
				if len(provCfg.Models) == 0 {
					slog.Warn("Models discovery failed, model filter disabled for this provider; set 'models' in config to suppress",
						"provider", name, "error", err)
				} else {
					slog.Warn("Models discovery failed, using configured models only",
						"provider", name, "error", err)
				}
				return
			}

			s.mu.Lock()
			if st, ok := s.states[name]; ok {
				if len(provCfg.Models) > 0 {
					// Merge fetched models into the configured baseline.
					for id := range fetched {
						if !st.availableModels[id] {
							st.availableModels[id] = true
							st.rawModels = append(st.rawModels, mustMarshal(map[string]string{
								"id": id, "object": "model", "owned_by": name,
							}))
						}
					}
					slog.Info("Models merged from config and upstream",
						"provider", name, "count", len(st.availableModels))
				} else {
					st.availableModels = fetched
					st.rawModels = fetchedRaw
					slog.Info("Models discovered from upstream",
						"provider", name, "count", len(fetched))
				}
			}
			s.mu.Unlock()
		}(name, provCfg)
	}
	wg.Wait()
}

// Models returns the public model list exposed by the route.
func (s *Selector) Models(cfg *config.ConfigStruct, route *config.RouteConfig) []json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_ = cfg

	seen := make(map[string]bool)
	var result []json.RawMessage

	for _, modelName := range route.PublicModels() {
		compiled := route.MatchModel(modelName)
		if compiled == nil {
			continue
		}
		if seen[modelName] {
			continue
		}
		seen[modelName] = true
		entry := map[string]any{
			"id":       modelName,
			"object":   "model",
			"owned_by": route.Prefix,
		}
		if !compiled.Wildcard && len(compiled.Upstreams) > 0 && compiled.Upstreams[0].RenameModel {
			entry["aliased"] = compiled.Upstreams[0].UpstreamModel
		}
		result = append(result, mustMarshal(entry))
	}

	for _, wildcard := range route.CompiledWildcardModels() {
		for _, upstream := range wildcard.Upstreams {
			st := s.states[upstream.Provider]
			if st == nil {
				continue
			}
			for _, raw := range st.rawModels {
				var entry map[string]json.RawMessage
				if err := json.Unmarshal(raw, &entry); err != nil {
					continue
				}
				var id string
				if idRaw, ok := entry["id"]; ok {
					_ = json.Unmarshal(idRaw, &id)
				}
				if id == "" || seen[id] {
					continue
				}
				matched, err := path.Match(wildcard.Pattern, id)
				if err != nil || !matched {
					continue
				}
				seen[id] = true
				entry["owned_by"] = mustMarshal(route.Prefix)
				out, _ := json.Marshal(entry)
				result = append(result, out)
			}
		}
	}

	return result
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// ProviderStatus exposes runtime health state for monitoring.
type ProviderStatus struct {
	Name                string           `json:"name"`
	ConsecutiveFailures int              `json:"consecutive_failures"`
	SuppressUntil       time.Time        `json:"suppress_until,omitzero"`
	Suppressed          bool             `json:"suppressed"`
	ManualSuppressed    bool             `json:"manual_suppressed"`
	SuppressReasons     []SuppressReason `json:"suppress_reasons,omitempty"`
	ModelCount          int              `json:"model_count"`
	TotalRequests       int64            `json:"total_requests"`
	SuccessCount        int64            `json:"success_count"`
	FailureCount        int64            `json:"failure_count"`
	AvgLatencyMs        float64          `json:"avg_latency_ms"`
	PreStreamErrors     int64            `json:"pre_stream_errors"`
	InStreamErrors      int64            `json:"in_stream_errors"`
	FailoverCount       int64            `json:"failover_count"`
	DisplayProtocols    []string         `json:"display_protocols,omitempty"`
	LastProtocolProbe   *ProtocolProbe   `json:"last_protocol_probe,omitempty"`
}

func (s *providerState) recentSuppressReasons() []SuppressReason {
	if len(s.suppressReasons) == 0 {
		return nil
	}
	cutoff := time.Now().Add(-suppressReasonTTL)
	var result []SuppressReason
	for _, r := range s.suppressReasons {
		if r.Time.After(cutoff) {
			result = append(result, r)
		}
	}
	return result
}

func (s *providerState) buildStatus(name string) ProviderStatus {
	now := time.Now()
	total, success, failure, avgLatency := s.windowStats()
	ps := ProviderStatus{
		Name:                name,
		ConsecutiveFailures: s.consecutiveFailures,
		SuppressUntil:       s.suppressUntil,
		Suppressed:          now.Before(s.suppressUntil),
		ManualSuppressed:    s.manualSuppress,
		SuppressReasons:     s.recentSuppressReasons(),
		TotalRequests:       int64(total),
		SuccessCount:        int64(success),
		FailureCount:        int64(failure),
		AvgLatencyMs:        avgLatency,
		PreStreamErrors:     s.preStreamErrors,
		InStreamErrors:      s.inStreamErrors,
		FailoverCount:       s.failoverCount,
		DisplayProtocols:    append([]string(nil), s.displayProtocols...),
		LastProtocolProbe:   s.lastProtocolProbe,
	}
	if s.availableModels != nil {
		ps.ModelCount = len(s.availableModels)
	}
	return ps
}

func (s *Selector) SetDisplayProtocols(name string, protocols []string, probe *ProtocolProbe) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, exists := s.states[name]
	if !exists {
		return false
	}
	st.displayProtocols = append([]string(nil), protocols...)
	st.lastProtocolProbe = probe
	return true
}

func (s *Selector) ModelProtocolProbes(name string) []ModelProtocolProbe {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st, exists := s.states[name]
	if !exists || len(st.modelProtocolProbes) == 0 {
		return nil
	}
	var out []ModelProtocolProbe
	for _, byProtocol := range st.modelProtocolProbes {
		for _, probe := range byProtocol {
			out = append(out, probe)
		}
	}
	slices.SortFunc(out, func(a, b ModelProtocolProbe) int {
		if cmp := strings.Compare(a.Model, b.Model); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Protocol, b.Protocol)
	})
	return out
}

func (s *Selector) UpsertModelProtocolProbe(name string, probe ModelProtocolProbe) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, exists := s.states[name]
	if !exists {
		return false
	}
	if st.modelProtocolProbes == nil {
		st.modelProtocolProbes = make(map[string]map[string]ModelProtocolProbe)
	}
	if st.modelProtocolProbes[probe.Model] == nil {
		st.modelProtocolProbes[probe.Model] = make(map[string]ModelProtocolProbe)
	}
	st.modelProtocolProbes[probe.Model][probe.Protocol] = probe
	return true
}

// ProviderStatuses returns a snapshot of all provider health states.
func (s *Selector) ProviderStatuses() []ProviderStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ProviderStatus, 0, len(s.states))
	for name, st := range s.states {
		result = append(result, st.buildStatus(name))
	}
	slices.SortFunc(result, func(a, b ProviderStatus) int {
		return strings.Compare(a.Name, b.Name)
	})
	return result
}

// ProviderDetail returns a single provider's status. Returns nil if not found.
func (s *Selector) ProviderDetail(name string) *ProviderStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st, exists := s.states[name]
	if !exists {
		return nil
	}
	ps := st.buildStatus(name)
	return &ps
}

// RecordFailover increments the failover counter for a provider.
func (s *Selector) RecordFailover(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if st, exists := s.states[name]; exists {
		st.failoverCount++
	}
}

// ProviderModels returns raw model objects for a single provider.
func (s *Selector) ProviderModels(name string) []json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st, exists := s.states[name]
	if !exists {
		return nil
	}
	return st.rawModels
}

// SetManualSuppress sets or clears manual suppression for a provider.
// Returns true if the provider exists, false otherwise.
func (s *Selector) SetManualSuppress(name string, suppress bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, exists := s.states[name]
	if !exists {
		return false
	}
	st.manualSuppress = suppress
	if suppress {
		slog.Info("Provider manually suppressed", "name", name)
	} else {
		slog.Info("Provider manual suppression cleared", "name", name)
	}
	return true
}

// SetAuthHeaders injects authentication headers based on protocol type.
func SetAuthHeaders(h http.Header, provCfg *config.ProviderConfig) {
	h.Set("Content-Type", "application/json")
	apiKey := provCfg.GetAPIKey()
	if apiKey != "" {
		switch provCfg.Protocol {
		case "anthropic":
			anthropic.SetAuthHeaders(h, apiKey)
		default:
			h.Set("Authorization", "Bearer "+apiKey)
		}
	}
	for k, v := range provCfg.Headers {
		h.Set(k, v)
	}
}

// modelsResponse is the common format for GET /models across all protocols.
type modelsResponse struct {
	Data    []json.RawMessage `json:"data"`
	HasMore bool              `json:"has_more"`
	LastID  string            `json:"last_id"`
}

// FetchModels queries GET <base_url>/models to discover available model IDs.
func FetchModels(provCfg *config.ProviderConfig) (map[string]bool, []json.RawMessage, error) {
	client := provCfg.HTTPClient(30 * time.Second)
	models := make(map[string]bool)
	var rawModels []json.RawMessage
	afterID := ""

	for {
		url := provCfg.URL + protocolModelsEndpoint(provCfg.Protocol)
		if afterID != "" {
			url += "?after_id=" + afterID
		}

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("create models request: %w", err)
		}
		SetAuthHeaders(req.Header, provCfg)

		resp, err := client.Do(req)
		if err != nil {
			return nil, nil, fmt.Errorf("fetch models: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("read models response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("fetch models: %s", formatModelsFetchHTTPError(resp.StatusCode, body))
		}

		ct := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") && len(body) > 0 && body[0] != '{' && body[0] != '[' {
			return nil, nil, fmt.Errorf("unexpected response Content-Type %q, not JSON", ct)
		}

		var result modelsResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, nil, fmt.Errorf("parse models response: %w", err)
		}

		for _, raw := range result.Data {
			var entry struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &entry); err == nil {
				models[entry.ID] = true
			}
			rawModels = append(rawModels, raw)
		}

		if !result.HasMore {
			break
		}
		afterID = result.LastID
	}

	return models, rawModels, nil
}

// protocolModelsEndpoint returns the models endpoint path for a given protocol.
func protocolModelsEndpoint(protocol string) string {
	switch protocol {
	case "anthropic":
		return "/v1/models"
	default:
		return "/models"
	}
}
