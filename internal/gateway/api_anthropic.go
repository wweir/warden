package gateway

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/sower-proxy/deferlog/v2"
	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
	sel "github.com/wweir/warden/internal/selector"
	"github.com/wweir/warden/pkg/protocol/anthropic"
)

// handleAnthropicMessages handles Anthropic Messages API requests.
func (g *Gateway) handleAnthropicMessages(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	var err error
	defer func() { deferlog.DebugError(err, "handle anthropic messages", "route", route.Prefix) }()

	r = r.WithContext(withRouteHooks(withClientRequest(r.Context(), r), route.Hooks))

	startTime := time.Now()
	reqID := reqlog.GenerateID()

	rawReqBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.Body.Close()

	model := gjson.GetBytes(rawReqBody, "model").String()
	stream := gjson.GetBytes(rawReqBody, "stream").Bool()
	if !gjson.ValidBytes(rawReqBody) {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	explicitProvider := r.Header.Get("X-Provider")
	var excluded []string
	resolved, err := g.selectRouteTarget(route, config.RouteProtocolAnthropic, model, explicitProvider, excluded)
	if err != nil {
		writeModelSelectionError(w, err)
		return
	}

	selectedProvider := resolved.prov
	selectedTarget := resolved.target
	metricLabels := buildMetricLabels(route, config.RouteProtocolAnthropic, "messages", selectedTarget)
	metricLabels.APIKey = apiKeyNameFromContext(r.Context())
	applyMetricHeaders(w, metricLabels)

	authRetried := map[string]bool{}
	allowFailover := explicitProvider == ""
	var failovers []reqlog.Failover

	for {
		provReqBody := prepareRawBody(rawReqBody, selectedTarget)
		if selectedProvider.Protocol == config.ProviderProtocolOpenAI && selectedProvider.AnthropicToChat {
			g.handleAnthropicMessagesViaChat(w, r, route, provReqBody, model, stream, selectedProvider, selectedTarget, resolved.model, startTime, reqID, allowFailover, excluded, authRetried, failovers)
			return
		}

		logRequest(r, selectedProvider.Name, model)
		logParams := newInferenceLogParams(r, startTime, reqID, route.Prefix, "messages", model, stream, rawReqBody, failovers, metricLabels, selectedProvider.Name)
		if stream {
			g.publishPendingInferenceLog(logParams)
		}

		if stream {
			streamReader, latency, sendErr := sendStreamingRequest(r.Context(), selectedProvider, protocolEndpoint(selectedProvider.Protocol, false), provReqBody)
			if sendErr != nil {
				err = sendErr
				g.selector.RecordOutcomeWithSource(selectedProvider.Name, sendErr, latency, "pre_stream")
				g.RecordStreamErrorMetric(metricLabels, "pre_stream")
				if tryAuthRetry(sendErr, selectedProvider, authRetried) {
					continue
				}
				if allowFailover {
					if next := g.tryFailover(sendErr, resolved, &excluded, route, config.RouteProtocolAnthropic, "messages", model, &failovers); next != nil {
						resolved = next
						selectedProvider = next.prov
						selectedTarget = next.target
						metricLabels = buildMetricLabels(route, config.RouteProtocolAnthropic, "messages", selectedTarget)
						metricLabels.APIKey = apiKeyNameFromContext(r.Context())
						applyMetricHeaders(w, metricLabels)
						continue
					}
				}
				g.recordInferenceLog(logParams, nil, sendErr.Error(), nil)
				writeUpstreamAwareError(w, sendErr)
				return
			}
			defer streamReader.Close()

			g.RecordTTFTMetric(metricLabels, latency)
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			rawResp, streamErr := relayAnthropicStream(streamReader, w)
			w.(http.Flusher).Flush()
			errMsg := ""
			if streamErr != nil {
				err = streamErr
				errMsg = streamErr.Error()
				if streamRelayErrorSource(streamErr) == streamRelaySourceUpstream {
					g.selector.RecordOutcomeWithSource(selectedProvider.Name, streamErr, latency, "in_stream")
					g.RecordStreamErrorMetric(metricLabels, "in_stream")
				}
				slog.Warn("Anthropic stream terminated early", "error", streamErr)
			} else {
				g.selector.RecordOutcome(selectedProvider.Name, nil, latency)
			}

			g.runRouteToolHooks(r.Context(), parseChatToolCalls(selectedProvider.Protocol, rawResp, true), "Anthropic: failed to run tool hooks")
			g.recordInferenceLog(
				logParams,
				rawResp,
				errMsg,
				func(respBody []byte) ([]byte, []byte, error) {
					assembled := anthropic.AssembleStream(respBody)
					if assembled == nil {
						return nil, respBody, fmt.Errorf("assemble anthropic stream")
					}
					return assembled, respBody, nil
				},
			)
			return
		}

		respBody, latency, sendErr := sendRequest(r.Context(), selectedProvider, protocolEndpoint(selectedProvider.Protocol, false), provReqBody, false)
		if sendErr != nil {
			err = sendErr
			g.selector.RecordOutcome(selectedProvider.Name, sendErr, latency)
			if tryAuthRetry(sendErr, selectedProvider, authRetried) {
				continue
			}
			if allowFailover {
				if next := g.tryFailover(sendErr, resolved, &excluded, route, config.RouteProtocolAnthropic, "messages", model, &failovers); next != nil {
					resolved = next
					selectedProvider = next.prov
					selectedTarget = next.target
					metricLabels = buildMetricLabels(route, config.RouteProtocolAnthropic, "messages", selectedTarget)
					metricLabels.APIKey = apiKeyNameFromContext(r.Context())
					applyMetricHeaders(w, metricLabels)
					continue
				}
			}
			g.recordInferenceLog(logParams, nil, sendErr.Error(), nil)
			writeUpstreamAwareError(w, sendErr)
			return
		}

		g.selector.RecordOutcome(selectedProvider.Name, nil, latency)
		g.runRouteToolHooks(r.Context(), parseChatToolCalls(selectedProvider.Protocol, respBody, false), "Anthropic: failed to run tool hooks")

		w.Header().Set("Content-Type", "application/json")
		if _, writeErr := w.Write(respBody); writeErr != nil {
			slog.Warn("Failed to write anthropic response", "error", writeErr)
		}
		g.recordInferenceLog(logParams, respBody, "", nil)
		return
	}
}

func (g *Gateway) handleAnthropicMessagesViaChat(
	w http.ResponseWriter,
	r *http.Request,
	route *config.RouteConfig,
	rawReqBody []byte,
	model string,
	stream bool,
	provCfg *config.ProviderConfig,
	target *sel.RouteTarget,
	matchedRouteModel *config.CompiledRouteModel,
	startTime time.Time,
	reqID string,
	allowFailover bool,
	excluded []string,
	authRetried map[string]bool,
	failovers []reqlog.Failover,
) bool {
	var err error
	defer func() { deferlog.DebugError(err, "handle anthropic messages via chat", "route", route.Prefix) }()

	metricLabels := buildMetricLabels(route, config.RouteProtocolAnthropic, "messages", target)
	metricLabels.APIKey = apiKeyNameFromContext(r.Context())
	applyMetricHeaders(w, metricLabels)

	chatReq, err := anthropic.MessagesRequestToChatRequest(rawReqBody)
	if err != nil {
		http.Error(w, fmt.Sprintf("convert to chat: %v", err), http.StatusBadRequest)
		return true
	}

	origModel := model
	chatReq.Model = target.UpstreamModel

	for {
		logRequest(r, provCfg.Name, origModel)
		logParams := newInferenceLogParams(r, startTime, reqID, route.Prefix, "messages", model, stream, rawReqBody, failovers, metricLabels, provCfg.Name)
		if stream {
			g.publishPendingInferenceLog(logParams)
		}

		if stream {
			reqBody, marshalErr := marshalProtocolRequest(provCfg.Protocol, chatReq)
			if marshalErr != nil {
				http.Error(w, fmt.Sprintf("marshal chat request: %v", marshalErr), http.StatusInternalServerError)
				return true
			}

			streamReader, latency, sendErr := sendStreamingRequest(r.Context(), provCfg, protocolEndpoint(provCfg.Protocol, false), reqBody)
			if sendErr != nil {
				err = sendErr
				g.selector.RecordOutcomeWithSource(provCfg.Name, sendErr, latency, "pre_stream")
				g.RecordStreamErrorMetric(metricLabels, "pre_stream")
				if tryAuthRetry(sendErr, provCfg, authRetried) {
					continue
				}
				if allowFailover {
					failed := &resolvedRouteTarget{model: matchedRouteModel, target: target, prov: provCfg}
					if next := g.tryFailover(sendErr, failed, &excluded, route, config.RouteProtocolAnthropic, "messages", origModel, &failovers); next != nil {
						provCfg = next.prov
						target = next.target
						chatReq.Model = target.UpstreamModel
						metricLabels = buildMetricLabels(route, config.RouteProtocolAnthropic, "messages", target)
						metricLabels.APIKey = apiKeyNameFromContext(r.Context())
						applyMetricHeaders(w, metricLabels)
						continue
					}
				}
				g.recordInferenceLog(logParams, nil, sendErr.Error(), nil)
				writeUpstreamAwareError(w, sendErr)
				return true
			}
			defer streamReader.Close()

			g.RecordTTFTMetric(metricLabels, latency)

			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			rawChat, clientBody, streamErr := streamChatAsAnthropic(streamReader, w)
			w.(http.Flusher).Flush()
			errMsg := ""
			if streamErr != nil {
				err = streamErr
				errMsg = streamErr.Error()
				if streamRelayErrorSource(streamErr) == streamRelaySourceUpstream {
					g.selector.RecordOutcomeWithSource(provCfg.Name, streamErr, latency, "in_stream")
					g.RecordStreamErrorMetric(metricLabels, "in_stream")
				}
				slog.Warn("AnthropicToChat stream terminated early", "error", streamErr)
			} else {
				g.selector.RecordOutcome(provCfg.Name, nil, latency)
			}

			g.runRouteToolHooks(r.Context(), parseChatToolCalls(provCfg.Protocol, rawChat, true), "AnthropicToChat stream: failed to run tool hooks")
			g.recordInferenceLog(
				logParams,
				clientBody,
				errMsg,
				func(respBody []byte) ([]byte, []byte, error) {
					assembled := anthropic.AssembleStream(respBody)
					if assembled == nil {
						return nil, respBody, fmt.Errorf("assemble anthropic stream")
					}
					return assembled, respBody, nil
				},
			)
			return true
		}

		chatResp, _, latency, forwardErr := g.forwardNonStreamRequest(r.Context(), provCfg, chatReq)
		if forwardErr != nil {
			err = forwardErr
			g.selector.RecordOutcome(provCfg.Name, forwardErr, latency)
			if tryAuthRetry(forwardErr, provCfg, authRetried) {
				continue
			}
			if allowFailover {
				failed := &resolvedRouteTarget{model: matchedRouteModel, target: target, prov: provCfg}
				if next := g.tryFailover(forwardErr, failed, &excluded, route, config.RouteProtocolAnthropic, "messages", origModel, &failovers); next != nil {
					provCfg = next.prov
					target = next.target
					chatReq.Model = target.UpstreamModel
					metricLabels = buildMetricLabels(route, config.RouteProtocolAnthropic, "messages", target)
					metricLabels.APIKey = apiKeyNameFromContext(r.Context())
					applyMetricHeaders(w, metricLabels)
					continue
				}
			}
			g.recordInferenceLog(logParams, nil, forwardErr.Error(), nil)
			writeUpstreamAwareError(w, forwardErr)
			return true
		}

		clientResp, convErr := anthropic.ChatResponseToMessagesResponse(chatResp)
		if convErr != nil {
			err = convErr
			http.Error(w, fmt.Sprintf("convert chat response to anthropic: %v", convErr), http.StatusBadGateway)
			return true
		}

		g.selector.RecordOutcome(provCfg.Name, nil, latency)
		if len(chatResp.Choices) > 0 {
			g.runRouteToolHooks(r.Context(), toolCallsToInfos(chatResp.Choices[0].Message.ToolCalls), "AnthropicToChat: failed to run tool hooks")
		}

		w.Header().Set("Content-Type", "application/json")
		if _, writeErr := w.Write(clientResp); writeErr != nil {
			slog.Warn("Failed to write anthropic bridge response", "error", writeErr)
		}
		g.recordInferenceLog(logParams, clientResp, "", nil)
		return true
	}
}
