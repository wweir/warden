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
	"github.com/wweir/warden/pkg/protocol/openai"
)

// handleChatCompletion handles Chat Completion requests.
func (g *Gateway) handleChatCompletion(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	var err error
	defer func() { deferlog.DebugError(err, "handle chat completion", "route", route.Prefix) }()

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
	resolved, err := g.selectRouteTarget(route, config.RouteProtocolChat, model, explicitProvider, excluded)
	if err != nil {
		writeModelSelectionError(w, err)
		return
	}
	selectedProvider := resolved.prov
	selectedTarget := resolved.target
	matchedRouteModel := resolved.model
	metricLabels := buildMetricLabels(route, config.RouteProtocolChat, "chat/completions", selectedTarget)
	metricLabels.APIKey = apiKeyNameFromContext(r.Context())
	applyMetricHeaders(w, metricLabels)

	if prompt := matchedRouteModel.SystemPrompt; prompt != "" {
		rawReqBody = openai.InjectSystemPromptRaw(rawReqBody, prompt)
	}

	authRetried := map[string]bool{}
	allowFailover := explicitProvider == ""

	if selectedProvider.ChatToResponses && selectedProvider.Protocol == "openai" {
		g.handleChatViaResponses(w, r, route, rawReqBody, model, stream, selectedProvider, selectedTarget, matchedRouteModel, startTime, reqID, allowFailover, excluded, authRetried)
		return
	}

	logRecord := func(respBody []byte, errMsg string) {
		rec := reqlog.Record{
			Timestamp:   startTime,
			RequestID:   reqID,
			Route:       route.Prefix,
			Endpoint:    "chat/completions",
			Model:       model,
			Stream:      stream,
			Provider:    selectedProvider.Name,
			UserAgent:   r.UserAgent(),
			DurationMs:  time.Since(startTime).Milliseconds(),
			Error:       errMsg,
			Fingerprint: reqlog.BuildFingerprint(rawReqBody),
			Request:     rawReqBody,
			Response:    respBody,
		}
		if stream && len(respBody) > 0 && errMsg == "" {
			if assembled, err := openai.AssembleChatStream(respBody); err == nil {
				rec.Response = assembled
				usage := ExtractTokenUsage(assembled)
				g.RecordTokenMetrics(metricLabels, usage, rec.DurationMs)
			}
		} else if len(respBody) > 0 && errMsg == "" {
			usage := ExtractTokenUsage(respBody)
			g.RecordTokenMetrics(metricLabels, usage, rec.DurationMs)
		}
		g.recordAndBroadcast(rec)
	}

	for {
		logRequest(r, selectedProvider.Name, model)

		provReqBody := prepareRawBody(rawReqBody, selectedTarget)
		reqBody, err := marshalProtocolRaw(selectedProvider.Protocol, provReqBody)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		respBody, latency, err := sendRequest(r.Context(), selectedProvider, protocolEndpoint(selectedProvider.Protocol, false), reqBody, stream)
		if err != nil {
			g.selector.RecordOutcome(selectedProvider.Name, err, latency)
			if tryAuthRetry(err, selectedProvider, authRetried) {
				continue
			}
			if allowFailover {
				if next := g.tryFailover(err, resolved, &excluded, route, config.RouteProtocolChat, "chat/completions", model); next != nil {
					resolved = next
					selectedProvider = next.prov
					selectedTarget = next.target
					matchedRouteModel = next.model
					metricLabels = buildMetricLabels(route, config.RouteProtocolChat, "chat/completions", selectedTarget)
					applyMetricHeaders(w, metricLabels)
					continue
				}
			}
			logRecord(nil, err.Error())
			writeUpstreamAwareError(w, err)
			return
		}
		g.selector.RecordOutcome(selectedProvider.Name, nil, latency)
		if stream {
			g.RecordTTFTMetric(metricLabels, latency)
		}

		g.runRouteToolHooks(r.Context(), parseChatToolCalls(selectedProvider.Protocol, respBody, stream), "Chat: failed to run tool hooks")

		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			clientBody := convertStreamIfNeeded(selectedProvider.Protocol, respBody)
			if _, writeErr := w.Write(clientBody); writeErr != nil {
				slog.Warn("Failed to write stream response", "error", writeErr)
			}
			w.(http.Flusher).Flush()
			logRecord(respBody, "")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if _, writeErr := w.Write(respBody); writeErr != nil {
			slog.Warn("Failed to write response", "error", writeErr)
		}
		logRecord(respBody, "")
		return
	}
}

// tryFailover checks if an error is retryable and selects the next upstream target.
// Returns nil if no failover is possible.
func (g *Gateway) tryFailover(err error, failed *resolvedRouteTarget, excluded *[]string, route *config.RouteConfig, serviceProtocol, endpoint, requestedModel string) *resolvedRouteTarget {
	if !sel.IsRetryableError(err) {
		return nil
	}
	*excluded = append(*excluded, failed.target.Key)
	g.selector.RecordFailover(failed.prov.Name)
	g.RecordFailoverMetric(buildMetricLabels(route, serviceProtocol, endpoint, failed.target))
	nextTarget, nextProv, selErr := g.selector.Select(g.cfg, route, serviceProtocol, failed.model, requestedModel, *excluded...)
	if selErr != nil {
		return nil
	}
	slog.Warn("Provider failover", "failed", failed.prov.Name, "next", nextProv.Name, "error", err)
	return &resolvedRouteTarget{
		model:  failed.model,
		target: nextTarget,
		prov:   nextProv,
	}
}

// tryAuthRetry checks if the error is 401 and retries the same provider after
// invalidating cached credentials. Returns true if the caller should continue the loop.
// retried tracks which providers have already been auth-retried to prevent infinite loops.
func tryAuthRetry(err error, provCfg *config.ProviderConfig, retried map[string]bool) bool {
	ue, ok := err.(*sel.UpstreamError)
	if !ok || !ue.IsAuthError() {
		return false
	}
	if retried[provCfg.Name] {
		return false
	}
	provCfg.InvalidateAuth()
	retried[provCfg.Name] = true
	slog.Info("Auth error, reloading credentials", "provider", provCfg.Name)
	return true
}

// --- upstream communication ---

// forwardNonStreamRequest sends a non-streaming chat completion request upstream.
// Returns parsed response, raw body bytes, and first-token latency for passthrough optimization.
func (g *Gateway) forwardNonStreamRequest(ctx context.Context, provCfg *config.ProviderConfig, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, []byte, time.Duration, error) {
	var resp openai.ChatCompletionResponse

	reqBody, err := marshalProtocolRequest(provCfg.Protocol, req)
	if err != nil {
		return resp, nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	body, latency, err := sendRequest(ctx, provCfg, protocolEndpoint(provCfg.Protocol, false), reqBody, false)
	if err != nil {
		return resp, nil, latency, err
	}

	resp, err = unmarshalProtocolResponse(provCfg.Protocol, body)
	if err != nil {
		return resp, nil, latency, fmt.Errorf("unmarshal response: %w", err)
	}

	return resp, body, latency, nil
}

// handleChatViaResponses handles Chat Completions requests by converting to/from Responses API format.
// This is used when chat_to_responses is enabled for a provider.
func (g *Gateway) handleChatViaResponses(w http.ResponseWriter, r *http.Request, route *config.RouteConfig,
	rawReqBody []byte, model string, stream bool, provCfg *config.ProviderConfig, target *sel.RouteTarget, matchedRouteModel *config.CompiledRouteModel,
	startTime time.Time, reqID string, allowFailover bool,
	excluded []string, authRetried map[string]bool,
) {
	var err error
	defer func() { deferlog.DebugError(err, "handle chat via responses", "route", route.Prefix) }()
	metricLabels := buildMetricLabels(route, config.RouteProtocolChat, "chat/completions", target)
	metricLabels.APIKey = apiKeyNameFromContext(r.Context())
	applyMetricHeaders(w, metricLabels)

	logRecord := func(respBody []byte, errMsg string) {
		rec := reqlog.Record{
			Timestamp:   startTime,
			RequestID:   reqID,
			Route:       route.Prefix,
			Endpoint:    "chat/completions",
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
			chatSSE := openai.ResponsesSSEToChatSSE(respBody)
			if assembled, err := openai.AssembleChatStream(chatSSE); err == nil {
				rec.Response = assembled
				usage := ExtractTokenUsage(assembled)
				g.RecordTokenMetrics(metricLabels, usage, rec.DurationMs)
			}
		} else if len(respBody) > 0 && errMsg == "" {
			usage := ExtractTokenUsage(respBody)
			g.RecordTokenMetrics(metricLabels, usage, rec.DurationMs)
		}
		g.recordAndBroadcast(rec)
	}

	var chatReq openai.ChatCompletionRequest
	if err = json.Unmarshal(rawReqBody, &chatReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	respReq, err := openai.ChatRequestToResponsesRequest(chatReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("convert to responses: %v", err), http.StatusInternalServerError)
		return
	}

	origModel := respReq.Model
	respReq.Model = target.UpstreamModel

	for {
		logRequest(r, provCfg.Name, origModel)

		respReq.Stream = stream
		reqBody, err := json.Marshal(respReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		rawResp, latency, err := sendRequest(r.Context(), provCfg, "/responses", reqBody, stream)
		if err != nil {
			if stream {
				g.selector.RecordOutcomeWithSource(provCfg.Name, err, latency, "pre_stream")
				g.RecordStreamErrorMetric(metricLabels, "pre_stream")
			} else {
				g.selector.RecordOutcome(provCfg.Name, err, latency)
			}
			if tryAuthRetry(err, provCfg, authRetried) {
				continue
			}
			if allowFailover {
				failed := &resolvedRouteTarget{model: matchedRouteModel, target: target, prov: provCfg}
				if next := g.tryFailover(err, failed, &excluded, route, config.RouteProtocolChat, "chat/completions", origModel); next != nil {
					provCfg = next.prov
					target = next.target
					respReq.Model = target.UpstreamModel
					metricLabels = buildMetricLabels(route, config.RouteProtocolChat, "chat/completions", target)
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
			g.runRouteToolHooks(r.Context(), parseResponsesToolCalls(rawResp, true), "ChatToResponses: failed to run tool hooks")
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			chatSSE := openai.ResponsesSSEToChatSSE(rawResp)
			if _, writeErr := w.Write(chatSSE); writeErr != nil {
				slog.Warn("Failed to write stream response", "error", writeErr)
			}
			w.(http.Flusher).Flush()
			logRecord(rawResp, "")
			return
		}

		var respResp openai.ResponsesResponse
		if err = json.Unmarshal(rawResp, &respResp); err != nil {
			http.Error(w, fmt.Sprintf("parse response: %v", err), http.StatusBadGateway)
			return
		}
		funcCalls, _ := extractFunctionCalls(respResp.Output)
		g.runRouteToolHooks(r.Context(), funcCallsToInfos(funcCalls), "ChatToResponses: failed to run tool hooks")

		chatResp, err := openai.ResponsesResponseToChatResponse(respResp, model)
		if err != nil {
			http.Error(w, fmt.Sprintf("convert response: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		respBody, _ := json.Marshal(chatResp)
		w.Write(respBody)
		logRecord(rawResp, "")
		return
	}
}
