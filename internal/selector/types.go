package selector

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/wweir/warden/config"
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
	manualSuppress      bool
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

// Selector selects the best provider for a request based on config order,
// model matching, and failure suppression.
type Selector struct {
	mu     sync.RWMutex
	states map[string]*providerState
}

// RouteTarget is the resolved upstream target for one public model request.
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

// NewSelector creates a new Selector and initializes state for all providers.
func NewSelector(cfg *config.ConfigStruct) *Selector {
	states := make(map[string]*providerState, len(cfg.Provider))
	for name, prov := range cfg.Provider {
		states[name] = &providerState{
			displayProtocols: append([]string(nil), config.SupportedRouteProtocols(prov)...),
		}
	}
	return &Selector{
		states: states,
	}
}

func (s *providerState) recordOutcome(success bool, latencyMs int64, errorSource string) {
	entry := outcome{
		timestamp:   time.Now(),
		success:     success,
		latencyMs:   latencyMs,
		errorSource: errorSource,
	}
	if len(s.outcomes) < outcomeWindowSize {
		s.outcomes = append(s.outcomes, entry)
		return
	}
	s.outcomes[s.outcomeStart] = entry
	s.outcomeStart = (s.outcomeStart + 1) % outcomeWindowSize
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
