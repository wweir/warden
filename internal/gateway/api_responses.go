package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/protocol/openai"
)

// handleResponses handles Responses API requests (POST /*/responses).
func (g *Gateway) handleResponses(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	var err error
	defer func() { deferlog.DebugError(err, "handle responses", "route", route.Prefix) }()

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
	serviceProtocol := responsesRequestProtocol(rawReqBody)
	stateful := serviceProtocol == config.RouteProtocolResponsesStateful
	if !gjson.ValidBytes(rawReqBody) {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if !route.SupportsServiceProtocol(serviceProtocol) {
		http.Error(w, unsupportedResponsesProtocolMessage(route.ConfiguredProtocol(), serviceProtocol), http.StatusBadRequest)
		return
	}

	var excluded []string
	authRetried := map[string]bool{}
	explicitProvider := r.Header.Get("X-Provider")
	resolved, err := g.selectRouteTarget(route, serviceProtocol, model, explicitProvider, excluded)
	if err != nil {
		writeModelSelectionError(w, err)
		return
	}
	provCfg := resolved.prov
	selectedTarget := resolved.target
	matchedRouteModel := resolved.model
	allowFailover := explicitProvider == "" && !stateful
	var failovers []reqlog.Failover
	metricLabels := buildMetricLabels(route, serviceProtocol, "responses", selectedTarget)
	metricLabels.APIKey = apiKeyNameFromContext(r.Context())
	applyMetricHeaders(w, metricLabels)

	if matchedRouteModel.PromptEnabled {
		if prompt := matchedRouteModel.SystemPrompt; prompt != "" {
			rawReqBody = openai.InjectSystemPromptResponsesRaw(rawReqBody, prompt)
		}
	}

	if serviceProtocol == config.RouteProtocolResponsesStateless && provCfg.ResponsesToChat && provCfg.Protocol == "openai" {
		g.handleResponsesViaChat(w, r, route, rawReqBody, model, stream, provCfg, selectedTarget, matchedRouteModel, startTime, reqID, allowFailover, excluded, authRetried, failovers)
		return
	}

	for {
		logRequest(r, provCfg.Name, model)
		logParams := newInferenceLogParams(r, startTime, reqID, route.Prefix, "responses", model, stream, rawReqBody, failovers, metricLabels, provCfg.Name)
		if stream {
			g.publishPendingInferenceLog(logParams)
		}

		provReqBody := prepareRawBody(rawReqBody, selectedTarget)
		respBody, latency, err := sendRequest(r.Context(), provCfg, protocolEndpoint(provCfg.Protocol, true), provReqBody, stream)
		if err != nil {
			g.selector.RecordOutcome(provCfg.Name, err, latency)
			if tryAuthRetry(err, provCfg, authRetried) {
				continue
			}
			if allowFailover {
				if next := g.tryFailover(err, resolved, &excluded, route, serviceProtocol, "responses", model, &failovers); next != nil {
					resolved = next
					provCfg = next.prov
					selectedTarget = next.target
					matchedRouteModel = next.model
					metricLabels = buildMetricLabels(route, serviceProtocol, "responses", selectedTarget)
					applyMetricHeaders(w, metricLabels)
					continue
				}
			}
			g.recordInferenceLog(
				logParams,
				nil,
				err.Error(),
				nil,
			)
			writeUpstreamAwareError(w, err)
			return
		}
		g.selector.RecordOutcome(provCfg.Name, nil, latency)
		if stream {
			g.RecordTTFTMetric(metricLabels, latency)
		}

		g.runRouteToolHooks(r.Context(), parseResponsesToolCalls(respBody, stream), "Responses: failed to run tool hooks")

		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.Write(respBody)
			w.(http.Flusher).Flush()
			g.recordInferenceLog(
				logParams,
				respBody,
				"",
				func(respBody []byte) ([]byte, []byte, error) {
					assembled, err := openai.AssembleResponsesStream(respBody)
					return assembled, respBody, err
				},
			)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(respBody)
		g.recordInferenceLog(
			logParams,
			respBody,
			"",
			nil,
		)
		return
	}
}

// handleResponsesViaChat handles Responses API requests by converting to/from Chat Completions.
// This is used when responses_to_chat is enabled for a provider.
func (g *Gateway) handleResponsesViaChat(w http.ResponseWriter, r *http.Request, route *config.RouteConfig,
	rawReqBody []byte, model string, stream bool, provCfg *config.ProviderConfig, target *sel.RouteTarget, matchedRouteModel *config.CompiledRouteModel,
	startTime time.Time, reqID string,
	allowFailover bool, excluded []string, authRetried map[string]bool, failovers []reqlog.Failover,
) {
	var err error
	defer func() { deferlog.DebugError(err, "handle responses via chat", "route", route.Prefix) }()

	metricLabels := buildMetricLabels(route, config.RouteProtocolResponsesStateless, "responses", target)
	metricLabels.APIKey = apiKeyNameFromContext(r.Context())
	applyMetricHeaders(w, metricLabels)
	var respReq openai.ResponsesRequest
	if err = json.Unmarshal(rawReqBody, &respReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	chatReq, err := openai.ResponsesRequestToChatRequest(respReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("convert to chat: %v", err), http.StatusBadRequest)
		return
	}

	origModel := chatReq.Model
	chatReq.Model = target.UpstreamModel

	for {
		logRequest(r, provCfg.Name, origModel)
		logParams := newInferenceLogParams(r, startTime, reqID, route.Prefix, "responses", model, stream, rawReqBody, failovers, metricLabels, provCfg.Name)
		if stream {
			g.publishPendingInferenceLog(logParams)
		}

		if stream {
			reqBody, marshalErr := marshalProtocolRequest(provCfg.Protocol, chatReq)
			if marshalErr != nil {
				http.Error(w, fmt.Sprintf("marshal chat request: %v", marshalErr), http.StatusInternalServerError)
				return
			}
			streamReader, latency, sendErr := sendStreamingRequest(r.Context(), provCfg, protocolEndpoint(provCfg.Protocol, false), reqBody)
			if sendErr != nil {
				g.selector.RecordOutcomeWithSource(provCfg.Name, sendErr, latency, "pre_stream")
				g.RecordStreamErrorMetric(metricLabels, "pre_stream")
				if tryAuthRetry(sendErr, provCfg, authRetried) {
					continue
				}
				if allowFailover {
					failed := &resolvedRouteTarget{model: matchedRouteModel, target: target, prov: provCfg}
					if next := g.tryFailover(sendErr, failed, &excluded, route, config.RouteProtocolResponsesStateless, "responses", origModel, &failovers); next != nil {
						provCfg = next.prov
						target = next.target
						chatReq.Model = target.UpstreamModel
						metricLabels = buildMetricLabels(route, config.RouteProtocolResponsesStateless, "responses", target)
						applyMetricHeaders(w, metricLabels)
						continue
					}
				}
				g.recordInferenceLog(
					logParams,
					nil,
					sendErr.Error(),
					nil,
				)
				writeUpstreamAwareError(w, sendErr)
				return
			}
			defer streamReader.Close()
			g.RecordTTFTMetric(metricLabels, latency)

			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			rawChat, respSSE, streamErr := streamChatAsResponses(streamReader, w, model)
			w.(http.Flusher).Flush()
			errMsg := ""
			if streamErr != nil {
				err = streamErr
				errMsg = streamErr.Error()
				if streamRelayErrorSource(streamErr) == streamRelaySourceUpstream {
					g.selector.RecordOutcomeWithSource(provCfg.Name, streamErr, latency, "in_stream")
					g.RecordStreamErrorMetric(metricLabels, "in_stream")
				}
				slog.Warn("ResponsesToChat stream terminated early", "error", streamErr)
			} else {
				g.selector.RecordOutcome(provCfg.Name, nil, latency)
			}
			g.runRouteToolHooks(r.Context(), parseChatToolCalls(provCfg.Protocol, rawChat, true), "ResponsesToChat stream: failed to run tool hooks")
			g.recordInferenceLog(
				logParams,
				respSSE,
				errMsg,
				func(respBody []byte) ([]byte, []byte, error) {
					assembled, err := openai.AssembleResponsesStream(respBody)
					return assembled, respBody, err
				},
			)
			return
		}

		chatResp, _, latency, forwardErr := g.forwardNonStreamRequest(r.Context(), provCfg, chatReq)
		if forwardErr != nil {
			g.selector.RecordOutcome(provCfg.Name, forwardErr, latency)
			if tryAuthRetry(forwardErr, provCfg, authRetried) {
				continue
			}
			if allowFailover {
				failed := &resolvedRouteTarget{model: matchedRouteModel, target: target, prov: provCfg}
				if next := g.tryFailover(forwardErr, failed, &excluded, route, config.RouteProtocolResponsesStateless, "responses", origModel, &failovers); next != nil {
					provCfg = next.prov
					target = next.target
					chatReq.Model = target.UpstreamModel
					metricLabels = buildMetricLabels(route, config.RouteProtocolResponsesStateless, "responses", target)
					applyMetricHeaders(w, metricLabels)
					continue
				}
			}
			g.recordInferenceLog(
				newInferenceLogParams(r, startTime, reqID, route.Prefix, "responses", model, stream, rawReqBody, failovers, metricLabels, provCfg.Name),
				nil,
				forwardErr.Error(),
				nil,
			)
			writeUpstreamAwareError(w, forwardErr)
			return
		}
		g.selector.RecordOutcome(provCfg.Name, nil, latency)
		if len(chatResp.Choices) > 0 {
			g.runRouteToolHooks(r.Context(), toolCallsToInfos(chatResp.Choices[0].Message.ToolCalls), "ResponsesToChat: failed to run tool hooks")
		}

		respResp, convErr := openai.ChatResponseToResponsesResponse(chatResp, model)
		if convErr != nil {
			http.Error(w, fmt.Sprintf("convert response: %v", convErr), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		respBody, _ := json.Marshal(respResp)
		if _, writeErr := w.Write(respBody); writeErr != nil {
			slog.Warn("Failed to write converted response", "error", writeErr)
		}
		g.recordInferenceLog(
			logParams,
			respBody,
			"",
			nil,
		)
		return
	}
}

func streamChatAsResponses(src io.Reader, dst http.ResponseWriter, model string) ([]byte, []byte, error) {
	reader := bufio.NewReader(src)
	state := openai.NewChatResponsesStreamState()
	var rawChat bytes.Buffer
	var rawResp bytes.Buffer
	streamComplete := false

	for {
		frame, err := readSSEFrame(reader)
		if len(frame) > 0 {
			rawChat.Write(frame)
			events := protocol.ParseEvents(frame)
			for _, evt := range events {
				if evt.Data == "[DONE]" {
					streamComplete = true
					continue
				}
				converted := state.ConvertEvent(evt)
				if len(converted) == 0 {
					continue
				}
				rawResp.Write(converted)
				if _, writeErr := dst.Write(converted); writeErr != nil {
					return rawChat.Bytes(), rawResp.Bytes(), &streamRelayError{source: streamRelaySourceDownstream, err: writeErr}
				}
				dst.(http.Flusher).Flush()
			}
		}

		if err != nil {
			if err != io.EOF {
				completed := openai.BuildChatResponsesCompletedEvent(rawChat.Bytes(), model, false)
				rawResp.Write(completed)
				if _, writeErr := dst.Write(completed); writeErr != nil {
					return rawChat.Bytes(), rawResp.Bytes(), &streamRelayError{source: streamRelaySourceDownstream, err: writeErr}
				}
				dst.(http.Flusher).Flush()
				return rawChat.Bytes(), rawResp.Bytes(), &streamRelayError{source: streamRelaySourceUpstream, err: err}
			}
			break
		}
	}

	completed := openai.BuildChatResponsesCompletedEvent(rawChat.Bytes(), model, streamComplete)
	rawResp.Write(completed)
	if _, writeErr := dst.Write(completed); writeErr != nil {
		return rawChat.Bytes(), rawResp.Bytes(), &streamRelayError{source: streamRelaySourceDownstream, err: writeErr}
	}
	dst.(http.Flusher).Flush()
	if !streamComplete {
		return rawChat.Bytes(), rawResp.Bytes(), &streamRelayError{source: streamRelaySourceUpstream, err: io.ErrUnexpectedEOF}
	}
	return rawChat.Bytes(), rawResp.Bytes(), nil
}

func readSSEFrame(r *bufio.Reader) ([]byte, error) {
	var frame bytes.Buffer
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			frame.Write(line)
			if bytes.Equal(line, []byte("\n")) || bytes.Equal(line, []byte("\r\n")) {
				return frame.Bytes(), nil
			}
		}
		if err != nil {
			if err == io.EOF && frame.Len() > 0 {
				return frame.Bytes(), io.EOF
			}
			return nil, err
		}
	}
}

func sendUpstreamChatRawWithLatency(ctx context.Context, provCfg *config.ProviderConfig, req openai.ChatCompletionRequest) ([]byte, time.Duration, error) {
	reqBody, err := marshalProtocolRequest(provCfg.Protocol, req)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}
	return sendRequest(ctx, provCfg, protocolEndpoint(provCfg.Protocol, false), reqBody, true)
}

// extractFunctionCalls extracts function_call items from output, separating them from other items.
func extractFunctionCalls(output []json.RawMessage) (funcCalls []openai.FunctionCallItem, others []json.RawMessage) {
	for _, raw := range output {
		if gjson.GetBytes(raw, "type").String() != "function_call" {
			others = append(others, raw)
			continue
		}

		var fc openai.FunctionCallItem
		if err := json.Unmarshal(raw, &fc); err != nil {
			others = append(others, raw)
			continue
		}
		funcCalls = append(funcCalls, fc)
	}
	return
}

// funcCallsToInfos converts FunctionCallItem slice to protocol.ToolCallInfo slice.
func funcCallsToInfos(calls []openai.FunctionCallItem) []protocol.ToolCallInfo {
	infos := make([]protocol.ToolCallInfo, len(calls))
	for i, fc := range calls {
		infos[i] = protocol.ToolCallInfo{
			ID:        fc.CallID,
			Name:      fc.Name,
			Arguments: fc.Arguments,
		}
	}
	return infos
}

func isStatefulResponsesRequest(rawReqBody []byte) bool {
	return gjson.GetBytes(rawReqBody, "previous_response_id").String() != ""
}

func responsesRequestProtocol(rawReqBody []byte) string {
	if isStatefulResponsesRequest(rawReqBody) {
		return config.RouteProtocolResponsesStateful
	}
	return config.RouteProtocolResponsesStateless
}

func unsupportedResponsesProtocolMessage(routeProtocol, serviceProtocol string) string {
	switch serviceProtocol {
	case config.RouteProtocolResponsesStateful:
		return fmt.Sprintf("route protocol %s does not support stateful responses requests", routeProtocol)
	case config.RouteProtocolResponsesStateless:
		return fmt.Sprintf("route protocol %s does not support stateless responses requests", routeProtocol)
	default:
		return fmt.Sprintf("route protocol %s does not support responses requests", routeProtocol)
	}
}
