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
	"github.com/wweir/warden/internal/toolexec"
	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/protocol/openai"
)

// handleChatCompletion handles Chat Completion requests.
func (g *Gateway) handleChatCompletion(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	var err error
	defer func() { deferlog.DebugError(err, "handle chat completion", "route", route.Prefix) }()

	// Set headers for metrics middleware
	w.Header().Set("X-Route", route.Prefix)
	w.Header().Set("X-Endpoint", "chat/completions")

	startTime := time.Now()
	reqID := reqlog.GenerateID()

	// read raw request body once
	rawReqBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.Body.Close()

	// lightweight parse: only extract fields needed for routing
	model := gjson.GetBytes(rawReqBody, "model").String()
	stream := gjson.GetBytes(rawReqBody, "stream").Bool()
	if !gjson.ValidBytes(rawReqBody) {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	w.Header().Set("X-Model", model)

	// collect enabled MCP tools
	availableTools, injectedTools := g.collectTools(r.Context(), route)

	// inject system prompt if configured for this model (protocol-independent)
	if prompt := route.SystemPrompts[model]; prompt != "" {
		rawReqBody = openai.InjectSystemPromptRaw(rawReqBody, prompt)
	}

	// select provider with failover on retryable errors
	var excluded []string
	authRetried := map[string]bool{}
	selectedProvider, err := g.selector.Select(g.cfg, route, model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set provider header for metrics middleware
	w.Header().Set("X-Provider", selectedProvider.Name)

	// helper to record a log entry
	var steps []reqlog.Step
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
			Steps:       steps,
		}
		// assemble streaming chunks into a single response object for logging
		if stream && len(respBody) > 0 && errMsg == "" {
			if assembled, err := openai.AssembleChatStream(respBody); err == nil {
				rec.Response = assembled
				// Extract token usage from assembled response for metrics
				usage := ExtractTokenUsage(assembled)
				g.RecordTokenMetrics(selectedProvider.Name, model, usage, rec.DurationMs)
			}
		} else if len(respBody) > 0 && errMsg == "" {
			// Extract token usage from non-stream response
			usage := ExtractTokenUsage(respBody)
			g.RecordTokenMetrics(selectedProvider.Name, model, usage, rec.DurationMs)
		}
		g.recordAndBroadcast(rec)
	}

	// no tools to inject: passthrough raw bytes with failover
	if len(availableTools) == 0 {
		for {
			logRequest(r, selectedProvider.Name, model)

			provReqBody := prepareRawBody(rawReqBody, selectedProvider, model)
			reqBody, err := marshalProtocolRaw(selectedProvider.Protocol, provReqBody)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			endpoint := protocolEndpoint(selectedProvider.Protocol, false)

			respBody, latency, err := sendRequest(selectedProvider, endpoint, reqBody, stream)
			if err != nil {
				g.selector.RecordOutcome(selectedProvider.Name, err, latency)
				if tryAuthRetry(err, selectedProvider, authRetried) {
					continue
				}
				if next := g.tryFailover(err, selectedProvider.Name, &excluded, route, model); next != nil {
					selectedProvider = next
					continue
				}
				logRecord(nil, err.Error())
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			g.selector.RecordOutcome(selectedProvider.Name, nil, latency)

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

	// tools need injection: full decode required
	var req openai.ChatCompletionRequest
	if err = json.Unmarshal(rawReqBody, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	injectedTools = openai.Inject(&req, mcpToolsToToolDefs(availableTools))

	// resolve model alias for the selected provider
	origModel := req.Model
	req.Model = selectedProvider.ResolveModel(req.Model)

	if req.Stream {
		for {
			logRequest(r, selectedProvider.Name, origModel)
			firstReqBody, err := marshalProtocolRequest(selectedProvider.Protocol, req)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			firstResp, latency, err := sendRequest(selectedProvider, protocolEndpoint(selectedProvider.Protocol, false), firstReqBody, true)
			if err != nil {
				g.selector.RecordOutcomeWithSource(selectedProvider.Name, err, latency, "pre_stream")
				g.RecordStreamErrorMetric(selectedProvider.Name, "pre_stream")
				if tryAuthRetry(err, selectedProvider, authRetried) {
					continue
				}
				if next := g.tryFailover(err, selectedProvider.Name, &excluded, route, origModel); next != nil {
					selectedProvider = next
					req.Model = selectedProvider.ResolveModel(origModel)
					continue
				}
				logRecord(nil, err.Error())
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			g.selector.RecordOutcome(selectedProvider.Name, nil, latency)

			// first request succeeded, write SSE response to client
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			if len(injectedTools) == 0 {
				clientBody := convertStreamIfNeeded(selectedProvider.Protocol, firstResp)
				if _, writeErr := w.Write(clientBody); writeErr != nil {
					slog.Warn("Failed to write stream response", "error", writeErr)
				}
				w.(http.Flusher).Flush()
				logRecord(firstResp, "")
				return
			}

			// parse SSE for tool call interception
			var respBody []byte
			var streamErr error
			respBody, steps, streamErr = g.processStreamToolCalls(w, r, selectedProvider, req, injectedTools, firstResp)
			// Record stream outcome for provider health (no failover after first packet)
			if streamErr != nil {
				g.selector.RecordOutcomeWithSource(selectedProvider.Name, streamErr, time.Since(startTime), "in_stream")
				g.RecordStreamErrorMetric(selectedProvider.Name, "in_stream")
				logRecord(respBody, streamErr.Error())
			} else {
				logRecord(respBody, "")
			}
			return
		}
	}

	// non-stream path with tools and failover
	for {
		logRequest(r, selectedProvider.Name, origModel)

		resp, respBody, latency, err := g.forwardNonStreamRequest(r.Context(), selectedProvider, req)
		if err != nil {
			g.selector.RecordOutcome(selectedProvider.Name, err, latency)
			if tryAuthRetry(err, selectedProvider, authRetried) {
				continue
			}
			if next := g.tryFailover(err, selectedProvider.Name, &excluded, route, origModel); next != nil {
				selectedProvider = next
				req.Model = selectedProvider.ResolveModel(origModel)
				continue
			}
			logRecord(nil, err.Error())
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		g.selector.RecordOutcome(selectedProvider.Name, nil, latency)

		// no tool calls in response: passthrough raw upstream response
		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
			w.Header().Set("Content-Type", "application/json")
			if _, writeErr := w.Write(respBody); writeErr != nil {
				slog.Warn("Failed to write response", "error", writeErr)
			}
			logRecord(respBody, "")
			return
		}

		resp = g.handleToolCalls(r.Context(), req, resp, injectedTools, selectedProvider, &steps)

		w.Header().Set("Content-Type", "application/json")
		finalBody, _ := json.Marshal(resp)
		if _, writeErr := w.Write(finalBody); writeErr != nil {
			slog.Warn("Failed to write response", "error", writeErr)
		}
		logRecord(finalBody, "")
		return
	}
}

// tryFailover checks if an error is retryable and selects the next provider.
// Returns nil if no failover is possible.
func (g *Gateway) tryFailover(err error, failedName string, excluded *[]string, route *config.RouteConfig, model string) *config.ProviderConfig {
	if !sel.IsRetryableError(err) {
		return nil
	}
	*excluded = append(*excluded, failedName)
	g.selector.RecordFailover(failedName)
	g.RecordFailoverMetric(failedName)
	next, selErr := g.selector.Select(g.cfg, route, model, *excluded...)
	if selErr != nil {
		return nil
	}
	slog.Warn("Provider failover", "failed", failedName, "next", next.Name, "error", err)
	return next
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

const maxToolCallIterations = 10

// handleToolCalls loops through tool call iterations until no injected tools remain.
func (g *Gateway) handleToolCalls(ctx context.Context, req openai.ChatCompletionRequest, resp openai.ChatCompletionResponse, injectedTools []string, provCfg *config.ProviderConfig, steps *[]reqlog.Step) openai.ChatCompletionResponse {
	messages := req.Messages

	for i := range maxToolCallIterations {
		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
			break
		}

		allCalls := resp.Choices[0].Message.ToolCalls
		injectedCalls, clientCalls := splitCalls(allCalls, injectedTools)
		allInfos := toolCallsToInfos(allCalls)

		results, err := toolexec.Execute(ctx, allInfos, injectedTools, g.mcpClients, g.cfg.MCP, g.cfg.ToolHooks, g.cfg.Addr)
		if err != nil {
			slog.Error("Failed to execute tools", "error", err)
			break
		}

		if len(injectedCalls) == 0 {
			break
		}

		slog.Debug("Executing injected tool calls", "iteration", i+1,
			"injected", len(injectedCalls), "client", len(clientCalls))

		messages = append(messages, resp.Choices[0].Message)
		messages = append(messages, toolResultsToMessages(results)...)

		newReq := openai.ChatCompletionRequest{
			Model: req.Model, Messages: messages, Tools: req.Tools,
			Extra: req.Extra,
		}

		llmReqBody, _ := json.Marshal(newReq)

		// mixed tool call: execute injected tools, then interrupt loop
		if len(clientCalls) > 0 {
			newResp, llmRespBody, _, err := g.forwardNonStreamRequest(ctx, provCfg, newReq)
			if err != nil {
				slog.Error("Failed to forward after mixed tool execution", "error", err)
				if steps != nil {
					*steps = append(*steps, buildStep(i+1, allInfos, results, llmReqBody, nil))
				}
			} else {
				resp = newResp
				if steps != nil {
					*steps = append(*steps, buildStep(i+1, allInfos, results, llmReqBody, llmRespBody))
				}
			}
			break
		}

		newResp, llmRespBody, _, err := g.forwardNonStreamRequest(ctx, provCfg, newReq)
		if err != nil {
			slog.Error("Failed to forward request after tool execution", "error", err, "iteration", i+1)
			if steps != nil {
				*steps = append(*steps, buildStep(i+1, allInfos, results, llmReqBody, nil))
			}
			break
		}
		resp = newResp
		if steps != nil {
			*steps = append(*steps, buildStep(i+1, allInfos, results, llmReqBody, llmRespBody))
		}
	}

	// filter out injected tool_calls from final response
	if len(resp.Choices) > 0 {
		_, resp.Choices[0].Message.ToolCalls = splitCalls(resp.Choices[0].Message.ToolCalls, injectedTools)
		if len(resp.Choices[0].Message.ToolCalls) == 0 && resp.Choices[0].FinishReason == "tool_calls" {
			resp.Choices[0].FinishReason = "stop"
		}
	}

	return resp
}

// processStreamToolCalls handles the tool call interception loop for streaming,
// starting from an already-fetched raw SSE response body.
func (g *Gateway) processStreamToolCalls(w http.ResponseWriter, r *http.Request, provCfg *config.ProviderConfig, req openai.ChatCompletionRequest, injectedTools []string, rawBody []byte) ([]byte, []reqlog.Step, error) {
	parser := newStreamParser(provCfg.Protocol, false)
	messages := req.Messages
	var steps []reqlog.Step

	for i := range maxToolCallIterations {
		// on subsequent iterations, fetch new response from upstream
		if i > 0 {
			var err error
			rawBody, err = sendUpstreamChatRaw(provCfg, req)
			if err != nil {
				return nil, steps, err
			}
		}

		events := protocol.ParseEvents(rawBody)
		sseInfos, hasInjected, err := parser.Parse(events, injectedTools)
		if err != nil {
			return nil, steps, err
		}

		// no injected tool calls: replay buffered SSE to client
		if !hasInjected {
			if len(sseInfos) > 0 {
				if _, err := toolexec.Execute(r.Context(), sseInfos, injectedTools, g.mcpClients, g.cfg.MCP, g.cfg.ToolHooks, g.cfg.Addr); err != nil {
					slog.Error("Stream: failed to run tool hooks", "error", err)
				}
			}
			if _, writeErr := w.Write(convertStreamIfNeeded(provCfg.Protocol, rawBody)); writeErr != nil {
				slog.Warn("Failed to write stream response", "error", writeErr)
			}
			w.(http.Flusher).Flush()
			return rawBody, steps, nil
		}

		injectedInfos, clientInfos := splitInfos(sseInfos, injectedTools)
		slog.Debug("Stream: executing injected tool calls", "iteration", i+1, "tool_calls", len(injectedInfos))

		results, err := toolexec.Execute(r.Context(), sseInfos, injectedTools, g.mcpClients, g.cfg.MCP, g.cfg.ToolHooks, g.cfg.Addr)
		if err != nil {
			slog.Error("Stream: failed to execute tools", "error", err)
			if _, writeErr := w.Write(convertStreamIfNeeded(provCfg.Protocol, rawBody)); writeErr != nil {
				slog.Warn("Failed to write stream response", "error", writeErr)
			}
			w.(http.Flusher).Flush()
			return rawBody, steps, nil
		}

		llmReqBody, _ := json.Marshal(req)
		steps = append(steps, buildStep(i+1, injectedInfos, results, llmReqBody, rawBody))

		// construct assistant message with all tool calls
		assistantMsg := openai.Message{Role: "assistant", ToolCalls: infosToToolCalls(sseInfos)}
		messages = append(messages, assistantMsg)
		messages = append(messages, toolResultsToMessages(results)...)
		req.Messages = messages

		// mixed tool call: execute injected, pipe final stream for client tools
		if len(clientInfos) > 0 {
			respBody, err := g.pipeChatStream(w, provCfg, req)
			return respBody, steps, err
		}
	}

	respBody, err := g.pipeChatStream(w, provCfg, req)
	return respBody, steps, err
}

// --- upstream communication ---

// forwardNonStreamRequest sends a non-streaming chat completion request upstream.
// Returns parsed response, raw body bytes, and first-token latency for passthrough optimization.
func (g *Gateway) forwardNonStreamRequest(_ context.Context, provCfg *config.ProviderConfig, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, []byte, time.Duration, error) {
	var resp openai.ChatCompletionResponse

	reqBody, err := marshalProtocolRequest(provCfg.Protocol, req)
	if err != nil {
		return resp, nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	body, latency, err := sendRequest(provCfg, protocolEndpoint(provCfg.Protocol, false), reqBody, false)
	if err != nil {
		return resp, nil, latency, err
	}

	resp, err = unmarshalProtocolResponse(provCfg.Protocol, body)
	if err != nil {
		return resp, nil, latency, fmt.Errorf("unmarshal response: %w", err)
	}

	return resp, body, latency, nil
}

// pipeChatStream sends a streaming chat request upstream and pipes the raw SSE response to the client.
func (g *Gateway) pipeChatStream(w http.ResponseWriter, provCfg *config.ProviderConfig, req openai.ChatCompletionRequest) ([]byte, error) {
	reqBody, err := marshalProtocolRequest(provCfg.Protocol, req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	return pipeRawStream(w, provCfg, protocolEndpoint(provCfg.Protocol, false), reqBody)
}

// sendUpstreamChatRaw sends a chat completion HTTP request and returns the raw response body.
func sendUpstreamChatRaw(provCfg *config.ProviderConfig, req openai.ChatCompletionRequest) ([]byte, error) {
	reqBody, err := marshalProtocolRequest(provCfg.Protocol, req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	body, _, err := sendRequest(provCfg, protocolEndpoint(provCfg.Protocol, false), reqBody, true)
	return body, err
}
