package gateway

import (
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
	if !gjson.ValidBytes(rawReqBody) {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	var excluded []string
	authRetried := map[string]bool{}
	explicitProvider := r.Header.Get("X-Provider")
	resolved, err := g.selectRouteTarget(route, config.RouteProtocolResponses, model, explicitProvider, excluded)
	if err != nil {
		writeModelSelectionError(w, err)
		return
	}
	provCfg := resolved.prov
	selectedTarget := resolved.target
	matchedRouteModel := resolved.model
	allowFailover := explicitProvider == ""
	metricLabels := buildMetricLabels(route, config.RouteProtocolResponses, "responses", selectedTarget)
	metricLabels.APIKey = apiKeyNameFromContext(r.Context())
	applyMetricHeaders(w, metricLabels)

	if prompt := matchedRouteModel.SystemPrompt; prompt != "" {
		rawReqBody = openai.InjectSystemPromptResponsesRaw(rawReqBody, prompt)
	}

	if provCfg.ResponsesToChat && provCfg.Protocol == "openai" {
		g.handleResponsesViaChat(w, r, route, rawReqBody, model, stream, provCfg, selectedTarget, matchedRouteModel, startTime, reqID, allowFailover, excluded, authRetried)
		return
	}

	logRecord := func(respBody []byte, errMsg string) {
		rec := reqlog.Record{
			Timestamp:   startTime,
			RequestID:   reqID,
			Route:       route.Prefix,
			Endpoint:    "responses",
			Model:       model,
			Stream:      stream,
			Provider:    provCfg.Name,
			UserAgent:   r.UserAgent(),
			DurationMs:  time.Since(startTime).Milliseconds(),
			Error:       errMsg,
			Fingerprint: reqlog.BuildFingerprint(rawReqBody),
			Request:     rawReqBody,
			Response:    respBody,
		}
		if stream && len(respBody) > 0 && errMsg == "" {
			events := protocol.ParseEvents(respBody)
			if cr := openai.ExtractCompletedResponse(events); cr != nil {
				if assembled, err := json.Marshal(cr); err == nil {
					rec.Response = assembled
					usage := ExtractTokenUsage(assembled)
					g.RecordTokenMetrics(metricLabels, usage, rec.DurationMs)
				}
			}
		} else if len(respBody) > 0 && errMsg == "" {
			usage := ExtractTokenUsage(respBody)
			g.RecordTokenMetrics(metricLabels, usage, rec.DurationMs)
		}
		g.recordAndBroadcast(rec)
	}

	for {
		logRequest(r, provCfg.Name, model)

		provReqBody := prepareRawBody(rawReqBody, selectedTarget)
		respBody, latency, err := sendRequest(r.Context(), provCfg, protocolEndpoint(provCfg.Protocol, true), provReqBody, stream)
		if err != nil {
			g.selector.RecordOutcome(provCfg.Name, err, latency)
			if tryAuthRetry(err, provCfg, authRetried) {
				continue
			}
			if allowFailover {
				if next := g.tryFailover(err, resolved, &excluded, route, config.RouteProtocolResponses, "responses", model); next != nil {
					resolved = next
					provCfg = next.prov
					selectedTarget = next.target
					matchedRouteModel = next.model
					metricLabels = buildMetricLabels(route, config.RouteProtocolResponses, "responses", selectedTarget)
					applyMetricHeaders(w, metricLabels)
					continue
				}
			}
			logRecord(nil, err.Error())
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
			logRecord(respBody, "")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(respBody)
		logRecord(respBody, "")
		return
	}
}

// handleResponsesViaChat handles Responses API requests by converting to/from Chat Completions.
// This is used when responses_to_chat is enabled for a provider.
func (g *Gateway) handleResponsesViaChat(w http.ResponseWriter, r *http.Request, route *config.RouteConfig,
	rawReqBody []byte, model string, stream bool, provCfg *config.ProviderConfig, target *sel.RouteTarget, matchedRouteModel *config.CompiledRouteModel,
	startTime time.Time, reqID string,
	allowFailover bool, excluded []string, authRetried map[string]bool,
) {
	var err error
	defer func() { deferlog.DebugError(err, "handle responses via chat", "route", route.Prefix) }()

	metricLabels := buildMetricLabels(route, config.RouteProtocolResponses, "responses", target)
	metricLabels.APIKey = apiKeyNameFromContext(r.Context())
	applyMetricHeaders(w, metricLabels)

	logRecord := func(respBody []byte, errMsg string) {
		rec := reqlog.Record{
			Timestamp:   startTime,
			RequestID:   reqID,
			Route:       route.Prefix,
			Endpoint:    "responses",
			Model:       model,
			Stream:      stream,
			Provider:    provCfg.Name,
			UserAgent:   r.UserAgent(),
			DurationMs:  time.Since(startTime).Milliseconds(),
			Error:       errMsg,
			Fingerprint: reqlog.BuildFingerprint(rawReqBody),
			Request:     rawReqBody,
			Response:    respBody,
		}
		if stream && len(respBody) > 0 && errMsg == "" {
			events := protocol.ParseEvents(respBody)
			if cr := openai.ExtractCompletedResponse(events); cr != nil {
				if assembled, err := json.Marshal(cr); err == nil {
					rec.Response = assembled
					usage := ExtractTokenUsage(assembled)
					g.RecordTokenMetrics(metricLabels, usage, rec.DurationMs)
				}
			}
		} else if len(respBody) > 0 && errMsg == "" {
			usage := ExtractTokenUsage(respBody)
			g.RecordTokenMetrics(metricLabels, usage, rec.DurationMs)
		}
		g.recordAndBroadcast(rec)
	}

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

		if stream {
			rawResp, latency, sendErr := sendUpstreamChatRawWithLatency(r.Context(), provCfg, chatReq)
			if sendErr != nil {
				g.selector.RecordOutcomeWithSource(provCfg.Name, sendErr, latency, "pre_stream")
				g.RecordStreamErrorMetric(metricLabels, "pre_stream")
				if tryAuthRetry(sendErr, provCfg, authRetried) {
					continue
				}
				if allowFailover {
					failed := &resolvedRouteTarget{model: matchedRouteModel, target: target, prov: provCfg}
					if next := g.tryFailover(sendErr, failed, &excluded, route, config.RouteProtocolResponses, "responses", origModel); next != nil {
						provCfg = next.prov
						target = next.target
						chatReq.Model = target.UpstreamModel
						metricLabels = buildMetricLabels(route, config.RouteProtocolResponses, "responses", target)
						applyMetricHeaders(w, metricLabels)
						continue
					}
				}
				logRecord(nil, sendErr.Error())
				writeUpstreamAwareError(w, sendErr)
				return
			}
			g.selector.RecordOutcome(provCfg.Name, nil, latency)
			g.RecordTTFTMetric(metricLabels, latency)
			g.runRouteToolHooks(r.Context(), parseChatToolCalls(provCfg.Protocol, rawResp, true), "ResponsesToChat stream: failed to run tool hooks")

			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			respSSE := openai.ChatSSEToResponsesSSE(rawResp, model)
			if _, writeErr := w.Write(respSSE); writeErr != nil {
				slog.Warn("Failed to write converted stream response", "error", writeErr)
			}
			w.(http.Flusher).Flush()
			logRecord(respSSE, "")
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
				if next := g.tryFailover(forwardErr, failed, &excluded, route, config.RouteProtocolResponses, "responses", origModel); next != nil {
					provCfg = next.prov
					target = next.target
					chatReq.Model = target.UpstreamModel
					metricLabels = buildMetricLabels(route, config.RouteProtocolResponses, "responses", target)
					applyMetricHeaders(w, metricLabels)
					continue
				}
			}
			logRecord(nil, forwardErr.Error())
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
		logRecord(respBody, "")
		return
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
