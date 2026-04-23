package gateway

import (
	"net/http"
	"time"

	"github.com/wweir/warden/config"
	inferencepkg "github.com/wweir/warden/internal/gateway/inference"
	loggingpkg "github.com/wweir/warden/internal/gateway/logging"
	observepkg "github.com/wweir/warden/internal/gateway/observe"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
	sel "github.com/wweir/warden/internal/selector"
)

type inferenceSession struct {
	gateway         *Gateway
	writer          http.ResponseWriter
	request         *http.Request
	route           *config.RouteConfig
	serviceProtocol string
	endpoint        string
	startTime       time.Time
	requestID       string
	model           string
	stream          bool
	rawBody         []byte
	manager         *inferencepkg.Manager
	metricLabels    telemetrypkg.Labels
	provider        *config.ProviderConfig
	target          *sel.RouteTarget
}

func (g *Gateway) newInferenceManager(
	route *config.RouteConfig,
	serviceProtocol, endpoint string,
	req inferenceRequest,
	allowFailover bool,
) (*inferencepkg.Manager, error) {
	return inferencepkg.NewManager(
		g.cfg,
		g.selector,
		route,
		serviceProtocol,
		endpoint,
		req.Model,
		req.ExplicitProvider,
		allowFailover,
		func(failed *inferencepkg.ResolvedTarget) {
			g.RecordFailoverMetric(telemetrypkg.BuildMetricLabels(route, serviceProtocol, endpoint, failed.Target))
		},
	)
}

func newInferenceSession(
	g *Gateway,
	w http.ResponseWriter,
	r *http.Request,
	route *config.RouteConfig,
	serviceProtocol, endpoint string,
	req inferenceRequest,
	manager *inferencepkg.Manager,
	startTime time.Time,
	requestID string,
) *inferenceSession {
	session := &inferenceSession{
		gateway:         g,
		writer:          w,
		request:         r,
		route:           route,
		serviceProtocol: serviceProtocol,
		endpoint:        endpoint,
		startTime:       startTime,
		requestID:       requestID,
		model:           req.Model,
		stream:          req.Stream,
		rawBody:         req.RawBody,
		manager:         manager,
	}
	session.refreshCurrent()
	return session
}

func (s *inferenceSession) refreshCurrent() {
	current := s.manager.Current()
	s.provider = current.Provider
	s.target = current.Target
	s.metricLabels = applyInferenceMetricHeaders(
		s.writer,
		s.request,
		s.route,
		s.serviceProtocol,
		s.endpoint,
		s.provider.Name,
		s.target,
	)
}

func (s *inferenceSession) logAttempt(model string) {
	loggingpkg.LogRequest(s.request, s.provider.Name, model)
}

func (s *inferenceSession) logParams() observepkg.InferenceLogParams {
	return observepkg.NewInferenceLogParams(
		s.request,
		s.startTime,
		s.requestID,
		s.route.Prefix,
		s.endpoint,
		s.model,
		s.stream,
		s.rawBody,
		s.manager.Failovers(),
		s.metricLabels,
		s.provider.Name,
	)
}

func (s *inferenceSession) publishPendingLog() {
	if s.stream {
		observepkg.PublishPendingInferenceLog(s.logParams(), s.gateway.broadcaster.Publish)
	}
}

func (s *inferenceSession) recordTTFT(latency time.Duration) {
	if s.stream {
		s.gateway.RecordTTFTMetric(s.metricLabels, latency)
	}
}

func (s *inferenceSession) observeMatchedModel() {
	if s.target == nil {
		return
	}
	s.gateway.selector.ObserveMatchedModel(s.target)
}

func (s *inferenceSession) handleError(err error) bool {
	if s.request.Context().Err() != nil {
		return false
	}
	if !s.manager.HandleError(err) {
		return false
	}
	s.refreshCurrent()
	return true
}
