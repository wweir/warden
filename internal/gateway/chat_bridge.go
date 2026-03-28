package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/wweir/warden/config"
	bridgepkg "github.com/wweir/warden/internal/gateway/bridge"
	inferencepkg "github.com/wweir/warden/internal/gateway/inference"
	observepkg "github.com/wweir/warden/internal/gateway/observe"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/pkg/protocol/openai"
)

type chatBridgeSpec struct {
	serviceProtocol           string
	endpoint                  string
	streamWarn                string
	streamToolHookOp          string
	writeResponseWarn         string
	buildChatRequest          func(rawReqBody []byte) (openai.ChatCompletionRequest, string, error)
	streamRelay               func(src io.Reader, dst http.ResponseWriter, publicModel string) ([]byte, []byte, error)
	streamLogAssembler        observepkg.StreamLogAssembler
	runNonStreamToolHooks     func(ctx context.Context, chatResp openai.ChatCompletionResponse)
	convertNonStreamResponse  func(chatResp openai.ChatCompletionResponse, publicModel string) ([]byte, error)
	writeConvertResponseError func(w http.ResponseWriter, err error)
}

func (g *Gateway) handleChatBridge(
	w http.ResponseWriter,
	r *http.Request,
	route *config.RouteConfig,
	rawReqBody []byte,
	model string,
	stream bool,
	manager *inferencepkg.Manager,
	startTime time.Time,
	reqID string,
	spec chatBridgeSpec,
) {
	chatReq, logModel, err := spec.buildChatRequest(rawReqBody)
	if err != nil {
		http.Error(w, fmt.Sprintf("convert to chat: %v", err), http.StatusBadRequest)
		return
	}

	sessionReq := inferenceRequest{
		RawBody: rawReqBody,
		Model:   model,
		Stream:  stream,
	}
	session := newInferenceSession(g, w, r, route, spec.serviceProtocol, spec.endpoint, sessionReq, manager, startTime, reqID)
	chatReq.Model = session.target.UpstreamModel

	for {
		session.logAttempt(logModel)
		logParams := session.logParams()
		session.publishPendingLog()

		if stream {
			reqBody, marshalErr := upstreampkg.MarshalProtocolRequest(session.provider.Protocol, chatReq)
			if marshalErr != nil {
				http.Error(w, fmt.Sprintf("marshal chat request: %v", marshalErr), http.StatusInternalServerError)
				return
			}

			streamReader, latency, sendErr := upstreampkg.SendStreamingRequest(
				r.Context(),
				r,
				session.provider,
				upstreampkg.ProtocolEndpoint(session.provider.Protocol, false),
				reqBody,
			)
			if sendErr != nil {
				g.selector.RecordOutcomeWithSource(session.provider.Name, sendErr, latency, "pre_stream")
				g.RecordStreamErrorMetric(session.metricLabels, "pre_stream")
				if session.handleError(sendErr) {
					chatReq.Model = session.target.UpstreamModel
					continue
				}
				observepkg.RecordInferenceLog(logParams, nil, sendErr.Error(), nil, g.RecordTokenMetrics, g.recordAndBroadcast)
				upstreampkg.WriteUpstreamAwareError(w, sendErr)
				return
			}
			defer streamReader.Close()

			session.recordTTFT(latency)
			writeEventStreamHeaders(w)

			rawChat, clientBody, streamErr := spec.streamRelay(streamReader, w, model)
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

			observepkg.RunRouteToolHooks(r.Context(), g.cfg.Addr, observepkg.ParseChatToolCalls(session.provider.Protocol, rawChat, true), spec.streamToolHookOp)
			observepkg.RecordInferenceLog(logParams, clientBody, errMsg, spec.streamLogAssembler, g.RecordTokenMetrics, g.recordAndBroadcast)
			return
		}

		chatResp, _, latency, forwardErr := g.forwardNonStreamRequest(r.Context(), session.provider, chatReq)
		if forwardErr != nil {
			g.selector.RecordOutcome(session.provider.Name, forwardErr, latency)
			if session.handleError(forwardErr) {
				chatReq.Model = session.target.UpstreamModel
				continue
			}
			observepkg.RecordInferenceLog(logParams, nil, forwardErr.Error(), nil, g.RecordTokenMetrics, g.recordAndBroadcast)
			upstreampkg.WriteUpstreamAwareError(w, forwardErr)
			return
		}

		respBody, convErr := spec.convertNonStreamResponse(chatResp, model)
		if convErr != nil {
			spec.writeConvertResponseError(w, convErr)
			return
		}

		g.selector.RecordOutcome(session.provider.Name, nil, latency)
		spec.runNonStreamToolHooks(r.Context(), chatResp)
		writeJSONResponse(w, respBody, spec.writeResponseWarn)
		observepkg.RecordInferenceLog(logParams, respBody, "", nil, g.RecordTokenMetrics, g.recordAndBroadcast)
		return
	}
}
