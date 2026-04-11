package gateway

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/wweir/warden/config"
	bridgepkg "github.com/wweir/warden/internal/gateway/bridge"
	inferencepkg "github.com/wweir/warden/internal/gateway/inference"
	observepkg "github.com/wweir/warden/internal/gateway/observe"
	tokenusagepkg "github.com/wweir/warden/internal/gateway/tokenusage"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
)

type relayInferenceSpec struct {
	serviceProtocol string
	endpoint        string
	streamWarn      string
	canHandle       func(provider *config.ProviderConfig) bool
	upstreamPath    func(providerProtocol string) string
	prepareBody     func(providerProtocol string, rawBody []byte) ([]byte, error)
	streamRelay     func(src io.Reader, dst http.ResponseWriter) ([]byte, error)
	streamAssembler observepkg.StreamLogAssembler
	runToolHooks    func(ctx context.Context, providerProtocol string, respBody []byte, stream bool)
	writeNonStream  func(w http.ResponseWriter, respBody []byte)
}

func (g *Gateway) handleRelayInference(
	w http.ResponseWriter,
	r *http.Request,
	route *config.RouteConfig,
	req inferenceRequest,
	manager *inferencepkg.Manager,
	startTime time.Time,
	reqID string,
	spec relayInferenceSpec,
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

		if req.Stream {
			streamReader, latency, sendErr := upstreampkg.SendStreamingRequest(
				r.Context(),
				r,
				session.provider,
				spec.upstreamPath(session.provider.Protocol),
				provReqBody,
			)
			if sendErr != nil {
				g.selector.RecordOutcomeWithSource(session.provider.Name, sendErr, latency, "pre_stream")
				g.RecordStreamErrorMetric(session.metricLabels, "pre_stream")
				if session.handleError(sendErr) {
					continue
				}
				observepkg.RecordInferenceLog(logParams, nil, sendErr.Error(), nil, tokenusagepkg.Missing(""), g.RecordTokenMetrics, g.recordAndBroadcast)
				upstreampkg.WriteUpstreamAwareError(w, sendErr)
				return true
			}
			defer streamReader.Close()

			session.recordTTFT(latency)
			writeEventStreamHeaders(w)

			rawResp, streamErr := spec.streamRelay(streamReader, w)
			w.(http.Flusher).Flush()
			errMsg := ""
			if streamErr != nil {
				errMsg = streamErr.Error()
				if bridgepkg.ErrorSourceOf(streamErr) == bridgepkg.SourceUpstream {
					g.selector.RecordOutcomeWithSource(session.provider.Name, streamErr, latency, "in_stream")
					g.RecordStreamErrorMetric(session.metricLabels, "in_stream")
				}
				slog.Warn(spec.streamWarn, "error", streamErr)
			} else {
				g.selector.RecordOutcome(session.provider.Name, nil, latency)
			}

			spec.runToolHooks(r.Context(), session.provider.Protocol, rawResp, true)
			observepkg.RecordInferenceLog(
				logParams,
				rawResp,
				errMsg,
				spec.streamAssembler,
				observeStreamTokenUsage(spec.serviceProtocol, session.provider.Protocol, rawResp),
				g.RecordTokenMetrics,
				g.recordAndBroadcast,
			)
			return true
		}

		respBody, latency, sendErr := upstreampkg.SendRequest(
			r.Context(),
			r,
			session.provider,
			spec.upstreamPath(session.provider.Protocol),
			provReqBody,
			false,
		)
		if sendErr != nil {
			g.selector.RecordOutcome(session.provider.Name, sendErr, latency)
			if session.handleError(sendErr) {
				continue
			}
			observepkg.RecordInferenceLog(logParams, nil, sendErr.Error(), nil, tokenusagepkg.Missing(""), g.RecordTokenMetrics, g.recordAndBroadcast)
			upstreampkg.WriteUpstreamAwareError(w, sendErr)
			return true
		}

		g.selector.RecordOutcome(session.provider.Name, nil, latency)
		spec.runToolHooks(r.Context(), session.provider.Protocol, respBody, false)
		spec.writeNonStream(w, respBody)
		observepkg.RecordInferenceLog(logParams, respBody, "", nil, observeJSONTokenUsage(respBody), g.RecordTokenMetrics, g.recordAndBroadcast)
		return true
	}
}
