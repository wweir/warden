package gateway

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/wweir/warden/config"
	inferencepkg "github.com/wweir/warden/internal/gateway/inference"
	observepkg "github.com/wweir/warden/internal/gateway/observe"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
	tokenusagepkg "github.com/wweir/warden/internal/gateway/tokenusage"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/pkg/toolhook"
)

// asyncHookFn runs async hooks after the response is written and reports verdicts.
type asyncHookFn func(func([]toolhook.HookVerdict))

// inferenceSpec drives handleInference. It captures the per-route-protocol
// hooks (body preparation, streaming relay, tool-hook execution, response
// writing) without describing the attempt loop itself.
type inferenceSpec struct {
	serviceProtocol string
	endpoint        string
	streamWarn      string

	canHandle       func(provider *config.ProviderConfig) bool
	upstreamPath    func(providerProtocol string) string
	prepareBody     func(providerProtocol string, rawBody []byte) ([]byte, error)
	runToolHooks    func(ctx context.Context, providerProtocol string, respBody []byte, stream bool) ([]byte, []toolhook.HookVerdict, asyncHookFn)
	writeNonStream  func(w http.ResponseWriter, respBody []byte)
	streamAssembler func(providerProtocol string) observepkg.StreamLogAssembler

	// streamRelay is invoked when stream=true and canRelayStream allows
	// relay (or canRelayStream is nil). nil disables stream relay entirely.
	streamRelay    func(src io.Reader, dst http.ResponseWriter) ([]byte, error)
	canRelayStream func(providerProtocol string) bool

	// writeBufferedStream is invoked when stream=true but the spec chose to
	// buffer the upstream response instead of relaying live. It must write
	// the assembled stream payload to the client.
	writeBufferedStream func(w http.ResponseWriter, providerProtocol string, respBody []byte)

	// allowNonStreamFallback transparently switches to a non-stream response
	// when SendStreamingRequest reports the upstream returned a regular JSON
	// body. Used by openai-compatible endpoints; anthropic does not need it.
	allowNonStreamFallback bool
}

func (g *Gateway) handleInference(
	w http.ResponseWriter,
	r *http.Request,
	route *config.RouteConfig,
	req inferenceRequest,
	manager *inferencepkg.Manager,
	startTime time.Time,
	reqID string,
	spec inferenceSpec,
) bool {
	session := newInferenceSession(g, w, r, route, spec.serviceProtocol, spec.endpoint, req, manager, startTime, reqID)

	for {
		if spec.canHandle != nil && !spec.canHandle(session.provider) {
			return false
		}

		session.logAttempt(req.Model)
		logParams := session.logParams()
		session.publishPendingLog()

		provReqBody, err := spec.prepareBody(session.providerProtocol, prepareRawBody(req.RawBody, session.target))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return true
		}

		if req.Stream && specCanRelayStream(spec, session.providerProtocol) {
			if g.handleInferenceStreamRelay(w, r, session, spec, logParams, provReqBody) {
				continue
			}
			return true
		}

		if g.handleInferenceBuffered(w, r, session, spec, logParams, provReqBody, req.Stream) {
			continue
		}
		return true
	}
}

func specCanRelayStream(spec inferenceSpec, providerProtocol string) bool {
	if spec.streamRelay == nil {
		return false
	}
	if spec.canRelayStream == nil {
		return true
	}
	return spec.canRelayStream(providerProtocol)
}

// handleInferenceStreamRelay returns true when the caller should retry the
// attempt loop (auth retry or failover succeeded). It returns false in every
// other terminal case (success, NonStreamingResponseError fallback, or final
// failure with the response already written).
func (g *Gateway) handleInferenceStreamRelay(
	w http.ResponseWriter,
	r *http.Request,
	session *inferenceSession,
	spec inferenceSpec,
	logParams observepkg.InferenceLogParams,
	provReqBody []byte,
) bool {
	streamReader, latency, sendErr := upstreampkg.SendStreamingRequest(
		r.Context(),
		r,
		session.provider,
		upstreampkg.JoinBaseURLPath(session.target.URL, spec.upstreamPath(session.providerProtocol)),
		provReqBody,
	)
	if sendErr != nil {
		if spec.allowNonStreamFallback {
			var nonStream *upstreampkg.NonStreamingResponseError
			if errors.As(sendErr, &nonStream) {
				g.finishInferenceNonStreamFallback(r, session, spec, logParams, nonStream.Body, latency)
				return false
			}
		}
		g.selector.RecordOutcomeWithSource(session.provider.Name, session.providerProtocol, sendErr, latency, "pre_stream")
		g.RecordStreamErrorMetric(session.metricLabels, "pre_stream")
		if session.handleError(sendErr) {
			return true
		}
		observepkg.RecordError(logParams, nil, sendErr.Error(), nil, g.recordAndBroadcast)
		upstreampkg.WriteUpstreamAwareError(w, sendErr)
		return false
	}
	defer streamReader.Close()

	session.observeMatchedModel()
	session.recordTTFT(latency)
	writeEventStreamHeaders(w)

	rawResp, streamErr := spec.streamRelay(streamReader, w)
	errMsg := ""
	if streamErr != nil {
		errMsg = streamErr.Error()
		if shouldRecordUpstreamStreamError(r, streamErr) {
			g.selector.RecordOutcomeWithSource(session.provider.Name, session.providerProtocol, streamErr, latency, "in_stream")
			g.RecordStreamErrorMetric(session.metricLabels, "in_stream")
		}
		warn := spec.streamWarn
		if warn == "" {
			warn = "Inference stream terminated early"
		}
		slog.Warn(warn, "error", streamErr)
	} else {
		g.selector.RecordOutcome(session.provider.Name, session.providerProtocol, nil, latency)
	}

	_, blockVerdicts, runAsync := spec.runToolHooks(r.Context(), session.providerProtocol, rawResp, true)
	g.recordInferenceStreamResponse(session, spec, logParams.WithTTFT(latency), rawResp, errMsg, blockVerdicts, runAsync)
	return false
}

// handleInferenceBuffered returns true when the attempt should retry, false
// when the request has been fully handled (including final emit / error
// response writing).
func (g *Gateway) handleInferenceBuffered(
	w http.ResponseWriter,
	r *http.Request,
	session *inferenceSession,
	spec inferenceSpec,
	logParams observepkg.InferenceLogParams,
	provReqBody []byte,
	stream bool,
) bool {
	respBody, latency, err := upstreampkg.SendRequest(
		r.Context(),
		r,
		session.provider,
		upstreampkg.JoinBaseURLPath(session.target.URL, spec.upstreamPath(session.providerProtocol)),
		provReqBody,
		stream,
	)
	if err != nil {
		g.selector.RecordOutcome(session.provider.Name, session.providerProtocol, err, latency)
		if session.handleError(err) {
			return true
		}
		observepkg.RecordError(logParams, nil, err.Error(), nil, g.recordAndBroadcast)
		upstreampkg.WriteUpstreamAwareError(w, err)
		return false
	}

	g.selector.RecordOutcome(session.provider.Name, session.providerProtocol, nil, latency)
	session.observeMatchedModel()
	session.recordTTFT(latency)
	respBody, blockVerdicts, runAsync := spec.runToolHooks(r.Context(), session.providerProtocol, respBody, stream)

	if stream {
		spec.writeBufferedStream(w, session.providerProtocol, respBody)
		g.recordInferenceStreamResponse(session, spec, logParams.WithTTFT(latency), respBody, "", blockVerdicts, runAsync)
		return false
	}

	spec.writeNonStream(w, respBody)
	completedLogParams := logParams.WithTTFT(latency).WithDuration(time.Since(logParams.StartTime).Milliseconds())
	observepkg.RecordSuccess(completedLogParams, respBody, observeJSONTokenUsage(spec.serviceProtocol, respBody), nil, g.RecordTokenMetrics, blockVerdicts, g.recordAndBroadcast)
	runAsync(func(asyncVerdicts []toolhook.HookVerdict) {
		if len(asyncVerdicts) == 0 {
			return
		}
		observepkg.RecordSuccess(
			completedLogParams,
			respBody,
			observeJSONTokenUsage(spec.serviceProtocol, respBody),
			nil,
			nil,
			append(append([]toolhook.HookVerdict{}, blockVerdicts...), asyncVerdicts...),
			g.recordAndBroadcast,
		)
	})
	return false
}

func (g *Gateway) finishInferenceNonStreamFallback(r *http.Request, session *inferenceSession, spec inferenceSpec, logParams observepkg.InferenceLogParams, respBody []byte, latency time.Duration) {
	g.selector.RecordOutcome(session.provider.Name, session.providerProtocol, nil, latency)
	session.observeMatchedModel()
	session.recordTTFT(latency)
	respBody, blockVerdicts, runAsync := spec.runToolHooks(r.Context(), session.providerProtocol, respBody, false)
	spec.writeNonStream(session.writer, respBody)
	completedLogParams := logParams.WithDuration(time.Since(logParams.StartTime).Milliseconds())
	observepkg.RecordSuccess(
		completedLogParams,
		respBody,
		observeJSONTokenUsage(session.serviceProtocol, respBody),
		nil,
		g.RecordTokenMetrics,
		blockVerdicts,
		g.recordAndBroadcast,
	)
	runAsync(func(asyncVerdicts []toolhook.HookVerdict) {
		if len(asyncVerdicts) == 0 {
			return
		}
		observepkg.RecordSuccess(
			completedLogParams,
			respBody,
			observeJSONTokenUsage(session.serviceProtocol, respBody),
			nil,
			nil,
			append(append([]toolhook.HookVerdict{}, blockVerdicts...), asyncVerdicts...),
			g.recordAndBroadcast,
		)
	})
}

func (g *Gateway) recordInferenceStreamResponse(session *inferenceSession, spec inferenceSpec, logParams observepkg.InferenceLogParams, respBody []byte, errMsg string, blockVerdicts []toolhook.HookVerdict, runAsync asyncHookFn) {
	completedLogParams := logParams.WithDuration(time.Since(logParams.StartTime).Milliseconds())
	emit := func(verdicts []toolhook.HookVerdict, recordTokens func(telemetrypkg.Labels, tokenusagepkg.Observation, int64)) {
		if errMsg != "" {
			observepkg.RecordError(completedLogParams, respBody, errMsg, verdicts, g.recordAndBroadcast)
			return
		}
		observepkg.RecordSuccess(
			completedLogParams,
			respBody,
			observeStreamTokenUsage(spec.serviceProtocol, session.providerProtocol, respBody),
			spec.streamAssembler(session.providerProtocol),
			recordTokens,
			verdicts,
			g.recordAndBroadcast,
		)
	}
	emit(blockVerdicts, g.RecordTokenMetrics)
	runAsync(func(asyncVerdicts []toolhook.HookVerdict) {
		if len(asyncVerdicts) == 0 {
			return
		}
		emit(append(append([]toolhook.HookVerdict{}, blockVerdicts...), asyncVerdicts...), nil)
	})
}
