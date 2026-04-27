package gateway

import (
	"context"
	"errors"
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
	"github.com/wweir/warden/pkg/toolhook"
)

// asyncHookFn runs async hooks after the response is written and reports verdicts.
type asyncHookFn func(func([]toolhook.HookVerdict))

type bufferedInferenceSpec struct {
	serviceProtocol string
	endpoint        string
	canHandle       func(provider *config.ProviderConfig) bool
	upstreamPath    func(providerProtocol string) string
	prepareBody     func(providerProtocol string, rawBody []byte) ([]byte, error)
	runToolHooks    func(ctx context.Context, providerProtocol string, respBody []byte, stream bool) ([]byte, []toolhook.HookVerdict, asyncHookFn)
	writeStream     func(w http.ResponseWriter, providerProtocol string, respBody []byte)
	writeNonStream  func(w http.ResponseWriter, respBody []byte)
	streamAssembler func(providerProtocol string) observepkg.StreamLogAssembler
	canRelayStream  func(providerProtocol string) bool
	streamRelay     func(src io.Reader, dst http.ResponseWriter) ([]byte, error)
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

		if req.Stream && canBufferedSpecRelayStream(spec, session.provider.Protocol) {
			streamReader, latency, sendErr := upstreampkg.SendStreamingRequest(
				r.Context(),
				r,
				session.provider,
				spec.upstreamPath(session.provider.Protocol),
				provReqBody,
			)
			if sendErr != nil {
				var nonStream *upstreampkg.NonStreamingResponseError
				if errors.As(sendErr, &nonStream) {
					g.finishBufferedNonStreamFallback(r, session, spec, logParams, nonStream.Body, latency)
					return true
				}
				g.selector.RecordOutcomeWithSource(session.provider.Name, sendErr, latency, "pre_stream")
				g.RecordStreamErrorMetric(session.metricLabels, "pre_stream")
				if session.handleError(sendErr) {
					continue
				}
				observepkg.RecordInferenceLog(logParams, nil, sendErr.Error(), nil, tokenusagepkg.Missing(""), g.RecordTokenMetrics, nil, g.recordAndBroadcast)
				upstreampkg.WriteUpstreamAwareError(w, sendErr)
				return true
			}
			defer streamReader.Close()

			session.observeMatchedModel()
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
				slog.Warn("Inference stream terminated early", "error", streamErr)
			} else {
				g.selector.RecordOutcome(session.provider.Name, nil, latency)
			}

			_, blockVerdicts, runAsync := spec.runToolHooks(r.Context(), session.provider.Protocol, rawResp, true)
			g.recordBufferedStreamResponse(session, spec, logParams, rawResp, errMsg, blockVerdicts, runAsync)
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
			observepkg.RecordInferenceLog(logParams, nil, err.Error(), nil, tokenusagepkg.Missing(""), g.RecordTokenMetrics, nil, g.recordAndBroadcast)
			upstreampkg.WriteUpstreamAwareError(w, err)
			return true
		}

		g.selector.RecordOutcome(session.provider.Name, nil, latency)
		session.observeMatchedModel()
		session.recordTTFT(latency)
		respBody, blockVerdicts, runAsync := spec.runToolHooks(r.Context(), session.provider.Protocol, respBody, req.Stream)

		if req.Stream {
			spec.writeStream(w, session.provider.Protocol, respBody)
			g.recordBufferedStreamResponse(session, spec, logParams, respBody, "", blockVerdicts, runAsync)
			return true
		}

		spec.writeNonStream(w, respBody)
		completedLogParams := logParams.WithDuration(time.Since(logParams.StartTime).Milliseconds())
		runAsync(func(asyncVerdicts []toolhook.HookVerdict) {
			if len(asyncVerdicts) == 0 {
				return
			}
			observepkg.RecordInferenceLog(
				completedLogParams,
				respBody,
				"",
				nil,
				observeJSONTokenUsage(spec.serviceProtocol, respBody),
				nil,
				append(append([]toolhook.HookVerdict{}, blockVerdicts...), asyncVerdicts...),
				g.recordAndBroadcast,
			)
		})
		observepkg.RecordInferenceLog(completedLogParams, respBody, "", nil, observeJSONTokenUsage(spec.serviceProtocol, respBody), g.RecordTokenMetrics, blockVerdicts, g.recordAndBroadcast)
		return true
	}
}

func (g *Gateway) finishBufferedNonStreamFallback(r *http.Request, session *inferenceSession, spec bufferedInferenceSpec, logParams observepkg.InferenceLogParams, respBody []byte, latency time.Duration) {
	g.selector.RecordOutcome(session.provider.Name, nil, latency)
	session.observeMatchedModel()
	session.recordTTFT(latency)
	respBody, blockVerdicts, runAsync := spec.runToolHooks(r.Context(), session.provider.Protocol, respBody, false)
	spec.writeNonStream(session.writer, respBody)
	g.recordBufferedNonStreamFallback(session, logParams, respBody, blockVerdicts, runAsync)
}

func (g *Gateway) recordBufferedStreamResponse(session *inferenceSession, spec bufferedInferenceSpec, logParams observepkg.InferenceLogParams, respBody []byte, errMsg string, blockVerdicts []toolhook.HookVerdict, runAsync asyncHookFn) {
	completedLogParams := logParams.WithDuration(time.Since(logParams.StartTime).Milliseconds())
	runAsync(func(asyncVerdicts []toolhook.HookVerdict) {
		if len(asyncVerdicts) == 0 {
			return
		}
		observepkg.RecordInferenceLog(
			completedLogParams,
			respBody,
			errMsg,
			spec.streamAssembler(session.provider.Protocol),
			observeStreamTokenUsage(spec.serviceProtocol, session.provider.Protocol, respBody),
			nil,
			append(append([]toolhook.HookVerdict{}, blockVerdicts...), asyncVerdicts...),
			g.recordAndBroadcast,
		)
	})
	observepkg.RecordInferenceLog(
		completedLogParams,
		respBody,
		errMsg,
		spec.streamAssembler(session.provider.Protocol),
		observeStreamTokenUsage(spec.serviceProtocol, session.provider.Protocol, respBody),
		g.RecordTokenMetrics,
		blockVerdicts,
		g.recordAndBroadcast,
	)
}

func (g *Gateway) recordBufferedNonStreamFallback(session *inferenceSession, logParams observepkg.InferenceLogParams, respBody []byte, blockVerdicts []toolhook.HookVerdict, runAsync asyncHookFn) {
	completedLogParams := logParams.WithDuration(time.Since(logParams.StartTime).Milliseconds())
	runAsync(func(asyncVerdicts []toolhook.HookVerdict) {
		if len(asyncVerdicts) == 0 {
			return
		}
		observepkg.RecordInferenceLog(
			completedLogParams,
			respBody,
			"",
			nil,
			observeJSONTokenUsage(session.serviceProtocol, respBody),
			nil,
			append(append([]toolhook.HookVerdict{}, blockVerdicts...), asyncVerdicts...),
			g.recordAndBroadcast,
		)
	})
	observepkg.RecordInferenceLog(
		completedLogParams,
		respBody,
		"",
		nil,
		observeJSONTokenUsage(session.serviceProtocol, respBody),
		g.RecordTokenMetrics,
		blockVerdicts,
		g.recordAndBroadcast,
	)
}

func canBufferedSpecRelayStream(spec bufferedInferenceSpec, providerProtocol string) bool {
	if spec.streamRelay == nil {
		return false
	}
	if spec.canRelayStream == nil {
		return true
	}
	return spec.canRelayStream(providerProtocol)
}
