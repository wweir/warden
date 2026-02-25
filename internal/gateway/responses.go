package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
	"github.com/wweir/warden/pkg/openai"
	"github.com/wweir/warden/pkg/sse"

	"github.com/sower-proxy/deferlog/v2"
)

// handleResponses handles Responses API requests (POST /*/responses).
func (g *Gateway) handleResponses(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	var err error
	defer func() { deferlog.DebugError(err, "handle responses", "route", route.Prefix) }()

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

	availableTools, _ := g.collectTools(r.Context(), route)

	var excluded []string
	provCfg, err := g.selector.Select(g.cfg, route, peek.Model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var steps []reqlog.Step
	buildStep := func(iteration int, toolCalls []reqlog.ToolCallEntry, toolResults []reqlog.ToolResultEntry, llmReq, llmResp []byte) {
		steps = append(steps, reqlog.Step{
			Iteration:   iteration,
			ToolCalls:   toolCalls,
			ToolResults: toolResults,
			LLMRequest:  llmReq,
			LLMResponse: llmResp,
		})
	}

	logRecord := func(respBody []byte, errMsg string) {
		rec := reqlog.Record{
			Timestamp:  startTime,
			RequestID:  reqID,
			Route:      route.Prefix,
			Endpoint:   "responses",
			Model:      peek.Model,
			Stream:     peek.Stream,
			Provider:   provCfg.Name,
			DurationMs: time.Since(startTime).Milliseconds(),
			Error:      errMsg,
			Request:    rawReqBody,
			Response:   respBody,
			Steps:      steps,
		}
		// extract completed response from SSE events for readability,
		// keep original SSE data in RawResponse for debugging
		if peek.Stream && len(respBody) > 0 && errMsg == "" {
			events := sse.ParseEvents(respBody)
			if cr := openai.ExtractCompletedResponse(events); cr != nil {
				if assembled, err := json.Marshal(cr); err == nil {
					rec.RawResponse = respBody
					rec.Response = assembled
				}
			}
		}
		g.recordAndBroadcast(rec)
	}

	// no tools to inject: passthrough raw bytes with failover
	if len(availableTools) == 0 {
		// inject system prompt into raw body for passthrough
		rawReqBody = injectSystemPromptResponsesRaw(rawReqBody, route, peek.Model)

		for {
			slog.Info("Request received", "method", r.Method, "path", r.URL.Path,
				"provider", provCfg.Name, "model", peek.Model)

			provReqBody := resolveModelRaw(rawReqBody, provCfg, peek.Model)
			endpoint := protocolEndpoint(provCfg.Protocol, true)

			if peek.Stream {
				respBody, streamErr := sendRequest(provCfg, endpoint, provReqBody)
				if streamErr != nil {
					g.selector.RecordOutcome(provCfg.Name, streamErr, time.Since(startTime))
					if next := g.tryFailover(streamErr, provCfg.Name, &excluded, route, peek.Model); next != nil {
						provCfg = next
						continue
					}
					logRecord(respBody, streamErr.Error())
					http.Error(w, streamErr.Error(), http.StatusBadGateway)
					return
				}
				g.selector.RecordOutcome(provCfg.Name, nil, time.Since(startTime))
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")
				w.Write(respBody)
				w.(http.Flusher).Flush()
				logRecord(respBody, "")
				return
			}

			respBody, err := sendRequest(provCfg, endpoint, provReqBody)
			if err != nil {
				g.selector.RecordOutcome(provCfg.Name, err, time.Since(startTime))
				if next := g.tryFailover(err, provCfg.Name, &excluded, route, peek.Model); next != nil {
					provCfg = next
					continue
				}
				logRecord(nil, err.Error())
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			g.selector.RecordOutcome(provCfg.Name, nil, time.Since(startTime))
			w.Header().Set("Content-Type", "application/json")
			w.Write(respBody)
			logRecord(respBody, "")
			return
		}
	}

	// tools need injection: full decode required
	var req openai.ResponsesRequest
	if err = json.Unmarshal(rawReqBody, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req.Input = injectSystemPromptResponses(req.Input, route, peek.Model)

	injectedTools := InjectResponsesTools(&req, availableTools)

	// resolve model alias for the selected provider
	origModel := req.Model
	req.Model = provCfg.ResolveModel(req.Model)

	if req.Stream {
		for {
			slog.Info("Request received", "method", r.Method, "path", r.URL.Path,
				"provider", provCfg.Name, "model", origModel)

			firstResp, firstErr := sendResponsesRawRequest(provCfg, req)
			if firstErr != nil {
				g.selector.RecordOutcome(provCfg.Name, firstErr, time.Since(startTime))
				if next := g.tryFailover(firstErr, provCfg.Name, &excluded, route, origModel); next != nil {
					provCfg = next
					req.Model = provCfg.ResolveModel(origModel)
					continue
				}
				logRecord(nil, firstErr.Error())
				http.Error(w, firstErr.Error(), http.StatusBadGateway)
				return
			}
			g.selector.RecordOutcome(provCfg.Name, nil, time.Since(startTime))

			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			respBody, streamErr := g.handleResponsesStreamWithFirstResponse(w, r, provCfg, req, injectedTools, buildStep, firstResp)
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
			"provider", provCfg.Name, "model", origModel)

		resp, respBody, err := g.forwardResponsesRequest(provCfg, req)
		if err != nil {
			g.selector.RecordOutcome(provCfg.Name, err, time.Since(startTime))
			if next := g.tryFailover(err, provCfg.Name, &excluded, route, origModel); next != nil {
				provCfg = next
				req.Model = provCfg.ResolveModel(origModel)
				continue
			}
			logRecord(nil, err.Error())
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		g.selector.RecordOutcome(provCfg.Name, nil, time.Since(startTime))

		// no function_call in output: passthrough raw upstream response
		if !hasInjectedFunctionCalls(resp.Output, injectedTools) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(respBody)
			logRecord(respBody, "")
			return
		}

		resp = g.handleResponsesToolCalls(r, provCfg, req, resp, injectedTools, buildStep)

		w.Header().Set("Content-Type", "application/json")
		finalBody, _ := json.Marshal(resp)
		w.Write(finalBody)
		logRecord(finalBody, "")
		return
	}
}

// handleResponsesToolCalls processes tool calls in a Responses API response.
func (g *Gateway) handleResponsesToolCalls(r *http.Request, provCfg *config.ProviderConfig, req openai.ResponsesRequest, resp openai.ResponsesResponse, injectedTools []string, buildStep func(int, []reqlog.ToolCallEntry, []reqlog.ToolResultEntry, []byte, []byte)) openai.ResponsesResponse {
	for i := range maxToolCallIterations {
		funcCalls, _ := extractFunctionCalls(resp.Output)
		injectedCalls, clientCalls := splitFuncCalls(funcCalls, injectedTools)

		if len(injectedCalls) == 0 {
			break
		}

		slog.Debug("Responses: executing injected function calls", "iteration", i+1,
			"injected", len(injectedCalls), "client", len(clientCalls))

		callInfos := funcCallsToInfos(injectedCalls)
		results, err := Execute(r.Context(), callInfos, injectedTools, g.mcpClients, g.cfg.MCP, g.cfg.Addr)
		if err != nil {
			slog.Error("Responses: failed to execute tools", "error", err)
			break
		}

		// record step
		toolCallEntries := toToolCallEntries(callInfos)
		toolResultEntries := toToolResultEntries(results)

		newInput, err := buildResponsesInput(req.Input, resp.Output, results)
		if err != nil {
			slog.Error("Responses: failed to build input", "error", err)
			break
		}

		req.Input = newInput
		removePreviousResponseID(&req)

		llmReqBody, _ := json.Marshal(req)

		// mixed tool call: interrupt and let client handle its tools
		if len(clientCalls) > 0 {
			newResp, llmRespBody, err := g.forwardResponsesRequest(provCfg, req)
			if err != nil {
				slog.Error("Responses: failed to forward after mixed tool execution", "error", err)
				buildStep(i+1, toolCallEntries, toolResultEntries, llmReqBody, nil)
			} else {
				resp = newResp
				buildStep(i+1, toolCallEntries, toolResultEntries, llmReqBody, llmRespBody)
			}
			break
		}

		newResp, llmRespBody, err := g.forwardResponsesRequest(provCfg, req)
		if err != nil {
			slog.Error("Responses: failed to forward after tool execution", "error", err, "iteration", i+1)
			buildStep(i+1, toolCallEntries, toolResultEntries, llmReqBody, nil)
			break
		}
		resp = newResp
		buildStep(i+1, toolCallEntries, toolResultEntries, llmReqBody, llmRespBody)
	}

	// filter out injected function_call items from final output
	resp.Output = openai.FilterResponsesOutput(resp.Output, injectedTools)

	return resp
}

// handleResponsesStream handles streaming Responses API with tool interception.
func (g *Gateway) handleResponsesStream(w http.ResponseWriter, r *http.Request, provCfg *config.ProviderConfig, req openai.ResponsesRequest, injectedTools []string, buildStep func(int, []reqlog.ToolCallEntry, []reqlog.ToolResultEntry, []byte, []byte)) ([]byte, error) {
	defer func() { deferlog.DebugError(nil, "handle responses stream") }()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	if len(injectedTools) == 0 {
		return g.pipeResponsesStream(w, provCfg, req)
	}

	rawBody, err := sendResponsesRawRequest(provCfg, req)
	if err != nil {
		return nil, err
	}
	return g.processResponsesStreamToolCalls(w, r, provCfg, req, injectedTools, buildStep, rawBody)
}

// handleResponsesStreamWithFirstResponse processes a streaming Responses API response
// with tool interception, using a pre-fetched first response (for failover support).
func (g *Gateway) handleResponsesStreamWithFirstResponse(w http.ResponseWriter, r *http.Request, provCfg *config.ProviderConfig, req openai.ResponsesRequest, injectedTools []string, buildStep func(int, []reqlog.ToolCallEntry, []reqlog.ToolResultEntry, []byte, []byte), firstResp []byte) ([]byte, error) {
	defer func() { deferlog.DebugError(nil, "handle responses stream with first response") }()

	if len(injectedTools) == 0 {
		w.Write(firstResp)
		w.(http.Flusher).Flush()
		return firstResp, nil
	}

	return g.processResponsesStreamToolCalls(w, r, provCfg, req, injectedTools, buildStep, firstResp)
}

// processResponsesStreamToolCalls handles the tool call interception loop for streaming
// Responses API, starting from an already-fetched raw SSE response body.
func (g *Gateway) processResponsesStreamToolCalls(w http.ResponseWriter, r *http.Request, provCfg *config.ProviderConfig, req openai.ResponsesRequest, injectedTools []string, buildStep func(int, []reqlog.ToolCallEntry, []reqlog.ToolResultEntry, []byte, []byte), rawBody []byte) ([]byte, error) {
	parser := newStreamParser(provCfg.Protocol, true)

	for i := range maxToolCallIterations {
		// on subsequent iterations, fetch new response from upstream
		if i > 0 {
			var err error
			rawBody, err = sendResponsesRawRequest(provCfg, req)
			if err != nil {
				return nil, err
			}
		}

		events := sse.ParseEvents(rawBody)
		sseInfos, hasInjected, err := parser.Parse(events, injectedTools)
		if err != nil || !hasInjected {
			filteredEvents := parser.Filter(events, injectedTools)
			replayData := sse.ReplayEvents(filteredEvents)
			w.Write(replayData)
			w.(http.Flusher).Flush()
			return replayData, nil
		}

		injectedInfos, clientInfos := splitInfos(sseInfos, injectedTools)
		slog.Debug("Responses stream: executing injected tool calls", "iteration", i+1)

		results, err := Execute(r.Context(), injectedInfos, injectedTools, g.mcpClients, g.cfg.MCP, g.cfg.Addr)
		if err != nil {
			slog.Error("Responses stream: failed to execute tools", "error", err)
			w.Write(rawBody)
			w.(http.Flusher).Flush()
			return rawBody, nil
		}

		// record step
		toolCallEntries := toToolCallEntries(injectedInfos)
		toolResultEntries := toToolResultEntries(results)

		llmReqBody, _ := json.Marshal(req)
		buildStep(i+1, toolCallEntries, toolResultEntries, llmReqBody, rawBody)

		completedResp := openai.ExtractCompletedResponse(events)
		if completedResp == nil {
			w.Write(rawBody)
			w.(http.Flusher).Flush()
			return rawBody, nil
		}

		newInput, err := buildResponsesInput(req.Input, completedResp.Output, results)
		if err != nil {
			return nil, err
		}

		req.Input = newInput
		removePreviousResponseID(&req)

		if len(clientInfos) > 0 {
			return g.pipeResponsesStream(w, provCfg, req)
		}
	}

	return g.pipeResponsesStream(w, provCfg, req)
}

// --- responses upstream communication ---

// forwardResponsesRequest sends a non-streaming Responses API request upstream.
// Returns both parsed response and raw body bytes for passthrough optimization.
func (g *Gateway) forwardResponsesRequest(provCfg *config.ProviderConfig, req openai.ResponsesRequest) (openai.ResponsesResponse, []byte, error) {
	var resp openai.ResponsesResponse

	reqBody, err := json.Marshal(req)
	if err != nil {
		return resp, nil, fmt.Errorf("marshal request: %w", err)
	}

	body, err := sendRequest(provCfg, protocolEndpoint(provCfg.Protocol, true), reqBody)
	if err != nil {
		return resp, nil, err
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return resp, nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return resp, body, nil
}

func sendResponsesRawRequest(provCfg *config.ProviderConfig, req openai.ResponsesRequest) ([]byte, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	return sendRequest(provCfg, protocolEndpoint(provCfg.Protocol, true), reqBody)
}

func (g *Gateway) pipeResponsesStream(w http.ResponseWriter, provCfg *config.ProviderConfig, req openai.ResponsesRequest) ([]byte, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	return pipeRawStream(w, provCfg, protocolEndpoint(provCfg.Protocol, true), reqBody)
}

// --- responses helpers ---

// hasInjectedFunctionCalls checks if output contains function_call items matching injected tools.
func hasInjectedFunctionCalls(output []json.RawMessage, injectedTools []string) bool {
	for _, raw := range output {
		var peek struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &peek) == nil && peek.Type == "function_call" && slices.Contains(injectedTools, peek.Name) {
			return true
		}
	}
	return false
}

// extractFunctionCalls extracts function_call items from output, separating them from other items.
func extractFunctionCalls(output []json.RawMessage) (funcCalls []openai.FunctionCallItem, others []json.RawMessage) {
	for _, raw := range output {
		var peek struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &peek); err != nil || peek.Type != "function_call" {
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

// splitFuncCalls separates function calls into injected and client-originated.
func splitFuncCalls(calls []openai.FunctionCallItem, injectedTools []string) (injected, client []openai.FunctionCallItem) {
	for _, fc := range calls {
		if slices.Contains(injectedTools, fc.Name) {
			injected = append(injected, fc)
		} else {
			client = append(client, fc)
		}
	}
	return
}

// funcCallsToInfos converts FunctionCallItem slice to sse.ToolCallInfo slice.
func funcCallsToInfos(calls []openai.FunctionCallItem) []sse.ToolCallInfo {
	infos := make([]sse.ToolCallInfo, len(calls))
	for i, fc := range calls {
		infos[i] = sse.ToolCallInfo{
			ID:        fc.CallID,
			Name:      fc.Name,
			Arguments: fc.Arguments,
		}
	}
	return infos
}

// buildResponsesInput builds new input for the next iteration:
// original input + output items (for reasoning etc.) + function_call_output items.
func buildResponsesInput(originalInput json.RawMessage, outputItems []json.RawMessage, results []ToolResult) (json.RawMessage, error) {
	var inputItems []json.RawMessage

	if len(originalInput) > 0 && originalInput[0] == '"' {
		// string input: wrap as message item
		var s string
		if err := json.Unmarshal(originalInput, &s); err != nil {
			return nil, err
		}
		msgItem := map[string]any{
			"type": "message",
			"role": "user",
			"content": []map[string]string{
				{"type": "input_text", "text": s},
			},
		}
		raw, err := json.Marshal(msgItem)
		if err != nil {
			return nil, err
		}
		inputItems = append(inputItems, raw)
	} else if len(originalInput) > 0 && originalInput[0] == '[' {
		// array input: parse items
		var items []json.RawMessage
		if err := json.Unmarshal(originalInput, &items); err != nil {
			return nil, err
		}
		inputItems = append(inputItems, items...)
	}

	inputItems = append(inputItems, outputItems...)

	for _, r := range results {
		output := openai.FunctionCallOutputItem{
			Type:   "function_call_output",
			CallID: r.CallID,
			Output: r.Output,
		}
		raw, err := json.Marshal(output)
		if err != nil {
			continue
		}
		inputItems = append(inputItems, raw)
	}

	return json.Marshal(inputItems)
}

// removePreviousResponseID removes previous_response_id from Extra for intermediate rounds.
func removePreviousResponseID(req *openai.ResponsesRequest) {
	if req.Extra != nil {
		delete(req.Extra, "previous_response_id")
	}
}
