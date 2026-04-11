package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/wweir/warden/config"
	inferencepkg "github.com/wweir/warden/internal/gateway/inference"
	observepkg "github.com/wweir/warden/internal/gateway/observe"
	tokenusagepkg "github.com/wweir/warden/internal/gateway/tokenusage"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
)

type bufferedInferenceSpec struct {
	serviceProtocol string
	endpoint        string
	canHandle       func(provider *config.ProviderConfig) bool
	upstreamPath    func(providerProtocol string) string
	prepareBody     func(providerProtocol string, rawBody []byte) ([]byte, error)
	runToolHooks    func(ctx context.Context, providerProtocol string, respBody []byte, stream bool)
	writeStream     func(w http.ResponseWriter, providerProtocol string, respBody []byte)
	writeNonStream  func(w http.ResponseWriter, respBody []byte)
	streamAssembler func(providerProtocol string) observepkg.StreamLogAssembler
}

func (g *Gateway) handleBufferedInference(
	w http.ResponseWriter,
	r *http.Request,
	route *config.RouteConfig,
	req inferenceRequest,
	manager *inferencepkg.Manager,
	startTime time.Time,
	reqID string,
	spec bufferedInferenceSpec,
) bool {
	session := newInferenceSession(g, w, r, route, spec.serviceProtocol, spec.endpoint, req, manager, startTime, reqID)

	for {
		if spec.canHandle != nil && !spec.canHandle(session.provider) {
			return false
		}

		session.logAttempt(req.Model)
		logParams := session.logParams()
		session.publishPendingLog()

		provReqBody, err := spec.prepareBody(session.provider.Protocol, prepareRawBody(req.RawBody, session.target))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return true
		}

		respBody, latency, err := upstreampkg.SendRequest(
			r.Context(),
			r,
			session.provider,
			spec.upstreamPath(session.provider.Protocol),
			provReqBody,
			req.Stream,
		)
		if err != nil {
			g.selector.RecordOutcome(session.provider.Name, err, latency)
			if session.handleError(err) {
				continue
			}
			observepkg.RecordInferenceLog(logParams, nil, err.Error(), nil, tokenusagepkg.Missing(""), g.RecordTokenMetrics, g.recordAndBroadcast)
			upstreampkg.WriteUpstreamAwareError(w, err)
			return true
		}

		g.selector.RecordOutcome(session.provider.Name, nil, latency)
		session.recordTTFT(latency)
		spec.runToolHooks(r.Context(), session.provider.Protocol, respBody, req.Stream)

		if req.Stream {
			spec.writeStream(w, session.provider.Protocol, respBody)
			observepkg.RecordInferenceLog(
				logParams,
				respBody,
				"",
				spec.streamAssembler(session.provider.Protocol),
				observeStreamTokenUsage(spec.serviceProtocol, session.provider.Protocol, respBody),
				g.RecordTokenMetrics,
				g.recordAndBroadcast,
			)
			return true
		}

		spec.writeNonStream(w, respBody)
		observepkg.RecordInferenceLog(logParams, respBody, "", nil, observeJSONTokenUsage(respBody), g.RecordTokenMetrics, g.recordAndBroadcast)
		return true
	}
}
