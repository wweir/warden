package gateway

import (
	"context"
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

	if matchedRouteModel.PromptEnabled {
		if prompt := matchedRouteModel.SystemPrompt; prompt != "" {
			rawReqBody = openai.InjectSystemPromptRaw(rawReqBody, prompt)
		}
	}

	authRetried := map[string]bool{}
	allowFailover := explicitProvider == ""
	var failovers []reqlog.Failover

	for {
		logRequest(r, selectedProvider.Name, model)
		logParams := newInferenceLogParams(r, startTime, reqID, route.Prefix, "chat/completions", model, stream, rawReqBody, failovers, metricLabels, selectedProvider.Name)
		if stream {
			g.publishPendingInferenceLog(logParams)
		}

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
				if next := g.tryFailover(err, resolved, &excluded, route, config.RouteProtocolChat, "chat/completions", model, &failovers); next != nil {
					resolved = next
					selectedProvider = next.prov
					selectedTarget = next.target
					matchedRouteModel = next.model
					metricLabels = buildMetricLabels(route, config.RouteProtocolChat, "chat/completions", selectedTarget)
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
			g.recordInferenceLog(
				logParams,
				respBody,
				"",
				func(respBody []byte) ([]byte, []byte, error) {
					clientBody := convertStreamIfNeeded(selectedProvider.Protocol, respBody)
					assembled, err := openai.AssembleChatStream(clientBody)
					return assembled, clientBody, err
				},
			)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if _, writeErr := w.Write(respBody); writeErr != nil {
			slog.Warn("Failed to write response", "error", writeErr)
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

// tryFailover checks if an error is retryable and selects the next upstream target.
// Returns nil if no failover is possible.
func (g *Gateway) tryFailover(err error, failed *resolvedRouteTarget, excluded *[]string, route *config.RouteConfig, serviceProtocol, endpoint, requestedModel string, failovers *[]reqlog.Failover) *resolvedRouteTarget {
	if !sel.IsRetryableError(err) {
		return nil
	}
	*excluded = append(*excluded, failed.target.Key)
	g.selector.RecordFailover(failed.prov.Name)
	g.RecordFailoverMetric(buildMetricLabels(route, serviceProtocol, endpoint, failed.target))
	nextTarget, nextProv, selErr := g.selector.Select(g.cfg, serviceProtocol, failed.model, requestedModel, *excluded...)
	if selErr != nil {
		return nil
	}
	if failovers != nil {
		*failovers = append(*failovers, reqlog.Failover{
			FailedProvider:      failed.prov.Name,
			FailedProviderModel: failed.target.UpstreamModel,
			NextProvider:        nextProv.Name,
			NextProviderModel:   nextTarget.UpstreamModel,
			Error:               err.Error(),
		})
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
