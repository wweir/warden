package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
	"github.com/wweir/warden/pkg/openai"
	"github.com/wweir/warden/pkg/sse"

	"github.com/sower-proxy/deferlog/v2"
)

// handleChatCompletion handles Chat Completion requests.
func (g *Gateway) handleChatCompletion(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	var err error
	defer func() { deferlog.DebugError(err, "handle chat completion", "route", route.Prefix) }()

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
	var peek struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err = json.Unmarshal(rawReqBody, &peek); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// collect enabled MCP tools
	availableTools, injectedTools := g.collectTools(r.Context(), route)

	// inject system prompt if configured for this model (protocol-independent)
	rawReqBody = injectSystemPromptRaw(rawReqBody, route, peek.Model)

	// select provider with failover on retryable errors
	var excluded []string
	selectedProvider, err := g.selector.Select(g.cfg, route, peek.Model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// helper to record a log entry
	var steps []reqlog.Step
	logRecord := func(respBody []byte, errMsg string) {
		rec := reqlog.Record{
			Timestamp:  startTime,
			RequestID:  reqID,
			Route:      route.Prefix,
			Endpoint:   "chat/completions",
			Model:      peek.Model,
			Stream:     peek.Stream,
			Provider:   selectedProvider.Name,
			DurationMs: time.Since(startTime).Milliseconds(),
			Error:      errMsg,
			Request:    rawReqBody,
			Response:   respBody,
			Steps:      steps,
		}
		// assemble streaming chunks into a single response for readability,
		// keep original SSE data in RawResponse for debugging
		if peek.Stream && len(respBody) > 0 && errMsg == "" {
			if assembled, err := openai.AssembleChatStream(respBody); err == nil {
				rec.RawResponse = respBody
				rec.Response = assembled
			}
		}
		g.recordAndBroadcast(rec)
	}

	// no tools to inject: passthrough raw bytes with failover
	if len(availableTools) == 0 {
		for {
			slog.Info("Request received", "method", r.Method, "path", r.URL.Path,
				"provider", selectedProvider.Name, "model", peek.Model)

			provReqBody := resolveModelRaw(rawReqBody, selectedProvider, peek.Model)
			reqBody, err := marshalProtocolRaw(selectedProvider.Protocol, provReqBody)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			endpoint := protocolEndpoint(selectedProvider.Protocol, false)

			if peek.Stream {
				respBody, streamErr := sendRequest(selectedProvider, endpoint, reqBody)
				if streamErr != nil {
					g.selector.RecordOutcome(selectedProvider.Name, streamErr, time.Since(startTime))
					if next := g.tryFailover(streamErr, selectedProvider.Name, &excluded, route, peek.Model); next != nil {
						selectedProvider = next
						continue
					}
					logRecord(respBody, streamErr.Error())
					http.Error(w, streamErr.Error(), http.StatusBadGateway)
					return
				}
				g.selector.RecordOutcome(selectedProvider.Name, nil, time.Since(startTime))
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")
				clientBody := convertStreamIfNeeded(selectedProvider.Protocol, respBody)
				w.Write(clientBody)
				w.(http.Flusher).Flush()
				logRecord(respBody, "")
				return
			}

			respBody, err := sendRequest(selectedProvider, endpoint, reqBody)
			if err != nil {
				g.selector.RecordOutcome(selectedProvider.Name, err, time.Since(startTime))
				if next := g.tryFailover(err, selectedProvider.Name, &excluded, route, peek.Model); next != nil {
					selectedProvider = next
					continue
				}
				logRecord(nil, err.Error())
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			g.selector.RecordOutcome(selectedProvider.Name, nil, time.Since(startTime))
			w.Header().Set("Content-Type", "application/json")
			w.Write(respBody)
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

	injectedTools = Inject(&req, availableTools)

	// resolve model alias for the selected provider
	origModel := req.Model
	req.Model = selectedProvider.ResolveModel(req.Model)

	if req.Stream {
		for {
			slog.Info("Request received", "method", r.Method, "path", r.URL.Path,
				"provider", selectedProvider.Name, "model", origModel)

			// try first upstream request to detect retryable errors before writing to client
			firstReqBody, err := marshalProtocolRequest(selectedProvider.Protocol, req)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			firstResp, err := sendRequest(selectedProvider, protocolEndpoint(selectedProvider.Protocol, false), firstReqBody)
			if err != nil {
				g.selector.RecordOutcome(selectedProvider.Name, err, time.Since(startTime))
				if next := g.tryFailover(err, selectedProvider.Name, &excluded, route, origModel); next != nil {
					selectedProvider = next
					req.Model = selectedProvider.ResolveModel(origModel)
					continue
				}
				logRecord(nil, err.Error())
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			g.selector.RecordOutcome(selectedProvider.Name, nil, time.Since(startTime))

			// first request succeeded, write SSE response to client
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			if len(injectedTools) == 0 {
				clientBody := convertStreamIfNeeded(selectedProvider.Protocol, firstResp)
				w.Write(clientBody)
				w.(http.Flusher).Flush()
				logRecord(firstResp, "")
				return
			}

			// parse SSE for tool call interception
			var respBody []byte
			var streamErr error
			respBody, steps, streamErr = g.handleStreamWithFirstResponse(w, r, selectedProvider, req, injectedTools, firstResp)
			if streamErr != nil {
				logRecord(respBody, streamErr.Error())
			} else {
				logRecord(respBody, "")
			}
			return
		}
	}

	// non-stream path with tools and failover
	for {
		slog.Info("Request received", "method", r.Method, "path", r.URL.Path,
			"provider", selectedProvider.Name, "model", origModel)

		resp, respBody, err := g.forwardNonStreamRequest(r.Context(), selectedProvider, req)
		if err != nil {
			g.selector.RecordOutcome(selectedProvider.Name, err, time.Since(startTime))
			if next := g.tryFailover(err, selectedProvider.Name, &excluded, route, origModel); next != nil {
				selectedProvider = next
				req.Model = selectedProvider.ResolveModel(origModel)
				continue
			}
			logRecord(nil, err.Error())
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		g.selector.RecordOutcome(selectedProvider.Name, nil, time.Since(startTime))

		// no tool calls in response: passthrough raw upstream response
		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.Write(respBody)
			logRecord(respBody, "")
			return
		}

		resp = g.handleToolCalls(r.Context(), req, resp, injectedTools, selectedProvider, &steps)

		w.Header().Set("Content-Type", "application/json")
		finalBody, _ := json.Marshal(resp)
		w.Write(finalBody)
		logRecord(finalBody, "")
		return
	}
}

// tryFailover checks if an error is retryable and selects the next provider.
// Returns nil if no failover is possible.
func (g *Gateway) tryFailover(err error, failedName string, excluded *[]string, route *config.RouteConfig, model string) *config.ProviderConfig {
	ue, ok := err.(*UpstreamError)
	if !ok || !ue.IsRetryable() {
		return nil
	}
	*excluded = append(*excluded, failedName)
	next, selErr := g.selector.Select(g.cfg, route, model, *excluded...)
	if selErr != nil {
		return nil
	}
	slog.Warn("Provider failover", "failed", failedName, "next", next.Name, "error", err)
	return next
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

		if len(injectedCalls) == 0 {
			break
		}

		slog.Debug("Executing injected tool calls", "iteration", i+1,
			"injected", len(injectedCalls), "client", len(clientCalls))

		callInfos := toolCallsToInfos(injectedCalls)
		results, err := Execute(ctx, callInfos, injectedTools, g.mcpClients, g.cfg.MCP, g.cfg.Addr)
		if err != nil {
			slog.Error("Failed to execute tools", "error", err)
			break
		}

		messages = append(messages, resp.Choices[0].Message)
		messages = append(messages, toolResultsToMessages(results)...)

		newReq := openai.ChatCompletionRequest{
			Model: req.Model, Messages: messages, Tools: req.Tools,
			Extra: req.Extra,
		}

		llmReqBody, _ := json.Marshal(newReq)

		// mixed tool call: execute injected tools, then interrupt loop
		if len(clientCalls) > 0 {
			newResp, llmRespBody, err := g.forwardNonStreamRequest(ctx, provCfg, newReq)
			if err != nil {
				slog.Error("Failed to forward after mixed tool execution", "error", err)
				if steps != nil {
					*steps = append(*steps, buildStep(i+1, callInfos, results, llmReqBody, nil))
				}
			} else {
				resp = newResp
				if steps != nil {
					*steps = append(*steps, buildStep(i+1, callInfos, results, llmReqBody, llmRespBody))
				}
			}
			break
		}

		newResp, llmRespBody, err := g.forwardNonStreamRequest(ctx, provCfg, newReq)
		if err != nil {
			slog.Error("Failed to forward request after tool execution", "error", err, "iteration", i+1)
			if steps != nil {
				*steps = append(*steps, buildStep(i+1, callInfos, results, llmReqBody, nil))
			}
			break
		}
		resp = newResp
		if steps != nil {
			*steps = append(*steps, buildStep(i+1, callInfos, results, llmReqBody, llmRespBody))
		}
	}

	// filter out injected tool_calls from final response
	if len(resp.Choices) > 0 {
		resp.Choices[0].Message.ToolCalls = filterToolCalls(resp.Choices[0].Message.ToolCalls, injectedTools)
		if len(resp.Choices[0].Message.ToolCalls) == 0 && resp.Choices[0].FinishReason == "tool_calls" {
			resp.Choices[0].FinishReason = "stop"
		}
	}

	return resp
}

// handleStream handles streaming responses with tool call interception.
func (g *Gateway) handleStream(w http.ResponseWriter, r *http.Request, provCfg *config.ProviderConfig, req openai.ChatCompletionRequest, injectedTools []string) ([]byte, []reqlog.Step, error) {
	defer func() { deferlog.DebugError(nil, "handle chat stream") }()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	if len(injectedTools) == 0 {
		respBody, err := g.pipeChatStream(w, provCfg, req)
		return respBody, nil, err
	}

	rawBody, err := sendUpstreamChatRaw(provCfg, req)
	if err != nil {
		return nil, nil, err
	}
	return g.processStreamToolCalls(w, r, provCfg, req, injectedTools, rawBody)
}

// handleStreamWithFirstResponse processes a streaming response with tool interception,
// using a pre-fetched first response (for failover support).
func (g *Gateway) handleStreamWithFirstResponse(w http.ResponseWriter, r *http.Request, provCfg *config.ProviderConfig, req openai.ChatCompletionRequest, injectedTools []string, firstResp []byte) ([]byte, []reqlog.Step, error) {
	defer func() { deferlog.DebugError(nil, "handle chat stream with first response") }()

	return g.processStreamToolCalls(w, r, provCfg, req, injectedTools, firstResp)
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

		events := sse.ParseEvents(rawBody)
		sseInfos, hasInjected, err := parser.Parse(events, injectedTools)
		if err != nil {
			return nil, steps, err
		}

		// no injected tool calls: replay buffered SSE to client
		if !hasInjected {
			w.Write(convertStreamIfNeeded(provCfg.Protocol, rawBody))
			w.(http.Flusher).Flush()
			return rawBody, steps, nil
		}

		injectedInfos, clientInfos := splitInfos(sseInfos, injectedTools)
		slog.Debug("Stream: executing injected tool calls", "iteration", i+1, "tool_calls", len(injectedInfos))

		results, err := Execute(r.Context(), injectedInfos, injectedTools, g.mcpClients, g.cfg.MCP, g.cfg.Addr)
		if err != nil {
			slog.Error("Stream: failed to execute tools", "error", err)
			w.Write(convertStreamIfNeeded(provCfg.Protocol, rawBody))
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
// Returns both parsed response and raw body bytes for passthrough optimization.
func (g *Gateway) forwardNonStreamRequest(_ context.Context, provCfg *config.ProviderConfig, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, []byte, error) {
	var resp openai.ChatCompletionResponse

	reqBody, err := marshalProtocolRequest(provCfg.Protocol, req)
	if err != nil {
		return resp, nil, fmt.Errorf("marshal request: %w", err)
	}

	body, err := sendRequest(provCfg, protocolEndpoint(provCfg.Protocol, false), reqBody)
	if err != nil {
		return resp, nil, err
	}

	resp, err = unmarshalProtocolResponse(provCfg.Protocol, body)
	if err != nil {
		return resp, nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return resp, body, nil
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
	return sendRequest(provCfg, protocolEndpoint(provCfg.Protocol, false), reqBody)
}
