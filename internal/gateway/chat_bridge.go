package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wweir/warden/config"
	inferencepkg "github.com/wweir/warden/internal/gateway/inference"
	observepkg "github.com/wweir/warden/internal/gateway/observe"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
	tokenusagepkg "github.com/wweir/warden/internal/gateway/tokenusage"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	sel "github.com/wweir/warden/internal/selector"
	"github.com/wweir/warden/pkg/protocol/openai"
	"github.com/wweir/warden/pkg/toolhook"
)

type chatBridgeSpec struct {
	serviceProtocol           string
	endpoint                  string
	streamWarn                string
	writeResponseWarn         string
	buildChatRequest          func(rawReqBody []byte) (openai.ChatCompletionRequest, string, error)
	streamRelay               func(src io.Reader, dst http.ResponseWriter, publicModel string) ([]byte, []byte, error)
	streamLogAssembler        observepkg.StreamLogAssembler
	runNonStreamToolHooks     func(ctx context.Context, chatResp openai.ChatCompletionResponse) ([]toolhook.HookVerdict, asyncHookFn)
	injectBlockVerdicts       func(respBody []byte, verdicts []toolhook.HookVerdict) []byte // apply block verdicts to the converted response body
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
	retriedDeveloperFallback := make(map[string]bool)

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
				if retryWithSystemRole(sendErr, session.provider.Name, retriedDeveloperFallback, &chatReq) {
					continue
				}
				g.selector.RecordOutcomeWithSource(session.provider.Name, sendErr, latency, "pre_stream")
				g.RecordStreamErrorMetric(session.metricLabels, "pre_stream")
				if session.handleError(sendErr) {
					chatReq.Model = session.target.UpstreamModel
					continue
				}
				observepkg.RecordError(logParams, nil, sendErr.Error(), nil, g.recordAndBroadcast)
				upstreampkg.WriteUpstreamAwareError(w, sendErr)
				return
			}
			defer streamReader.Close()

			session.observeMatchedModel()
			session.recordTTFT(latency)
			writeEventStreamHeaders(w)

			rawChat, clientBody, streamErr := spec.streamRelay(streamReader, w, model)
			w.(http.Flusher).Flush()
			errMsg := ""
			if streamErr != nil {
				errMsg = streamErr.Error()
				if shouldRecordUpstreamStreamError(r, streamErr) {
					g.selector.RecordOutcomeWithSource(session.provider.Name, streamErr, latency, "in_stream")
					g.RecordStreamErrorMetric(session.metricLabels, "in_stream")
				}
				slog.Warn(spec.streamWarn, "error", streamErr)
			} else {
				g.selector.RecordOutcome(session.provider.Name, nil, latency)
			}

			// Stream: all hook checks degrade to async audits because the live response cannot be rewritten.
			completedLogParams := logParams.WithTTFT(latency).WithDuration(time.Since(logParams.StartTime).Milliseconds())
			emitStreamLog := func(verdicts []toolhook.HookVerdict, recordTokens func(telemetrypkg.Labels, tokenusagepkg.Observation, int64)) {
				if errMsg != "" {
					observepkg.RecordError(completedLogParams, clientBody, errMsg, verdicts, g.recordAndBroadcast)
					return
				}
				observepkg.RecordSuccess(
					completedLogParams,
					clientBody,
					observeStreamTokenUsage(config.RouteProtocolChat, session.provider.Protocol, rawChat),
					spec.streamLogAssembler,
					recordTokens,
					verdicts,
					g.recordAndBroadcast,
				)
			}
			go func() {
				asyncVerdicts := observepkg.RunDegradedAsyncToolHooks(r.Context(), g.hookGatewayTarget(), observepkg.ParseChatToolCalls(session.provider.Protocol, rawChat, true))
				if len(asyncVerdicts) == 0 {
					return
				}
				emitStreamLog(asyncVerdicts, nil)
			}()
			emitStreamLog(nil, g.RecordTokenMetrics)
			return
		}

		chatResp, rawRespBody, latency, forwardErr := g.forwardNonStreamRequest(r.Context(), session.provider, chatReq)
		if forwardErr != nil {
			if retryWithSystemRole(forwardErr, session.provider.Name, retriedDeveloperFallback, &chatReq) {
				continue
			}
			g.selector.RecordOutcome(session.provider.Name, forwardErr, latency)
			if session.handleError(forwardErr) {
				chatReq.Model = session.target.UpstreamModel
				continue
			}
			observepkg.RecordError(logParams, nil, forwardErr.Error(), nil, g.recordAndBroadcast)
			upstreampkg.WriteUpstreamAwareError(w, forwardErr)
			return
		}
		session.observeMatchedModel()

		respBody, convErr := spec.convertNonStreamResponse(chatResp, model)
		if convErr != nil {
			g.selector.RecordOutcome(session.provider.Name, nil, latency)
			observepkg.RecordError(logParams, rawRespBody, convErr.Error(), nil, g.recordAndBroadcast)
			spec.writeConvertResponseError(w, convErr)
			return
		}

		g.selector.RecordOutcome(session.provider.Name, nil, latency)
		blockVerdicts, runAsync := spec.runNonStreamToolHooks(r.Context(), chatResp)
		if spec.injectBlockVerdicts != nil {
			respBody = spec.injectBlockVerdicts(respBody, blockVerdicts)
		}
		writeJSONResponse(w, respBody, spec.writeResponseWarn)
		completedLogParams := logParams.WithTTFT(latency).WithDuration(time.Since(logParams.StartTime).Milliseconds())
		observepkg.RecordSuccess(completedLogParams, respBody, observeBridgeJSONTokenUsage(respBody), nil, g.RecordTokenMetrics, blockVerdicts, g.recordAndBroadcast)
		runAsync(func(asyncVerdicts []toolhook.HookVerdict) {
			if len(asyncVerdicts) == 0 {
				return
			}
			observepkg.RecordSuccess(
				completedLogParams,
				respBody,
				observeBridgeJSONTokenUsage(respBody),
				nil,
				nil,
				append(append([]toolhook.HookVerdict{}, blockVerdicts...), asyncVerdicts...),
				g.recordAndBroadcast,
			)
		})
		return
	}
}

func retryWithSystemRole(err error, providerName string, retried map[string]bool, chatReq *openai.ChatCompletionRequest) bool {
	if retried[providerName] || !developerRoleRejected(err) {
		return false
	}

	messages, changed := openai.DowngradeDeveloperMessages(chatReq.Messages)
	if !changed {
		return false
	}

	chatReq.Messages = messages
	retried[providerName] = true
	return true
}

func developerRoleRejected(err error) bool {
	var upErr *sel.UpstreamError
	if !errors.As(err, &upErr) {
		return false
	}
	if upErr.Code != http.StatusBadRequest {
		return false
	}

	body := strings.ToLower(upErr.Body)
	return strings.Contains(body, "developer") &&
		(strings.Contains(body, "unsupported") ||
			strings.Contains(body, "not supported") ||
			strings.Contains(body, "invalid") ||
			strings.Contains(body, "unknown"))
}
