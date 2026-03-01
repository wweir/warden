package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/sower-proxy/deferlog/v2"
	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
	"github.com/wweir/warden/internal/toolexec"
	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/protocol/openai"
)

// handleResponses handles Responses API requests (POST /*/responses).
func (g *Gateway) handleResponses(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	var err error
	defer func() { deferlog.DebugError(err, "handle responses", "route", route.Prefix) }()

	// Set headers for metrics middleware
	w.Header().Set("X-Route", route.Prefix)
	w.Header().Set("X-Endpoint", "responses")

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

	availableTools, _ := g.collectTools(r.Context(), route)

	var excluded []string
	authRetried := map[string]bool{}
	provCfg, err := g.selector.Select(g.cfg, route, model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set provider header for metrics middleware
	w.Header().Set("X-Provider", provCfg.Name)

	var steps []reqlog.Step

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
			Steps:       steps,
		}
		// extract completed response object from SSE events for logging
		if stream && len(respBody) > 0 && errMsg == "" {
			events := protocol.ParseEvents(respBody)
			if cr := openai.ExtractCompletedResponse(events); cr != nil {
				if assembled, err := json.Marshal(cr); err == nil {
					rec.Response = assembled
					usage := ExtractTokenUsage(assembled)
					g.RecordTokenMetrics(provCfg.Name, model, usage, rec.DurationMs)
				}
			}
		} else if len(respBody) > 0 && errMsg == "" {
			usage := ExtractTokenUsage(respBody)
			g.RecordTokenMetrics(provCfg.Name, model, usage, rec.DurationMs)
		}
		g.recordAndBroadcast(rec)
	}

	// no tools to inject: passthrough raw bytes with failover
	if len(availableTools) == 0 {
		// inject system prompt into raw body for passthrough
		if prompt := route.SystemPrompts[model]; prompt != "" {
			rawReqBody = openai.InjectSystemPromptResponsesRaw(rawReqBody, prompt)
		}

		for {
			logRequest(r, provCfg.Name, model)

			provReqBody := prepareRawBody(rawReqBody, provCfg, model)
			endpoint := protocolEndpoint(provCfg.Protocol, true)

			respBody, latency, err := sendRequest(provCfg, endpoint, provReqBody)
			if err != nil {
				g.selector.RecordOutcome(provCfg.Name, err, latency)
				if tryAuthRetry(err, provCfg, authRetried) {
					continue
				}
				if next := g.tryFailover(err, provCfg.Name, &excluded, route, model); next != nil {
					provCfg = next
					continue
				}
				logRecord(nil, err.Error())
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			g.selector.RecordOutcome(provCfg.Name, nil, latency)

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

	// tools need injection: full decode required
	var req openai.ResponsesRequest
	if err = json.Unmarshal(rawReqBody, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req.Input = openai.InjectSystemPromptResponses(req.Input, route.SystemPrompts[model])

	injectedTools := openai.InjectResponsesTools(&req, mcpToolsToToolDefs(availableTools))

	// resolve model alias for the selected provider
	origModel := req.Model
	req.Model = provCfg.ResolveModel(req.Model)

	if req.Stream {
		for {
			logRequest(r, provCfg.Name, origModel)

			firstResp, firstErr := sendResponsesRawRequest(provCfg, req)
			if firstErr != nil {
				g.selector.RecordOutcomeWithSource(provCfg.Name, firstErr, time.Since(startTime), "pre_stream")
				g.RecordStreamErrorMetric(provCfg.Name, "pre_stream")
				if tryAuthRetry(firstErr, provCfg, authRetried) {
					continue
				}
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

			respBody, streamSteps, streamErr := g.handleResponsesStreamWithFirstResponse(w, r, provCfg, req, injectedTools, firstResp)
			steps = append(steps, streamSteps...)
			// Record stream outcome for provider health (no failover after first packet)
			if streamErr != nil {
				g.selector.RecordOutcomeWithSource(provCfg.Name, streamErr, time.Since(startTime), "in_stream")
				g.RecordStreamErrorMetric(provCfg.Name, "in_stream")
				logRecord(respBody, streamErr.Error())
			} else {
				logRecord(respBody, "")
			}
			return
		}
	}

	// non-stream path with tools and failover
	for {
		logRequest(r, provCfg.Name, origModel)

		resp, respBody, err := g.forwardResponsesRequest(provCfg, req)
		if err != nil {
			g.selector.RecordOutcome(provCfg.Name, err, time.Since(startTime))
			if tryAuthRetry(err, provCfg, authRetried) {
				continue
			}
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

		funcCalls, _ := extractFunctionCalls(resp.Output)
		// no injected function_call in output: passthrough raw upstream response
		if !hasInjectedFunctionCalls(resp.Output, injectedTools) {
			if len(funcCalls) > 0 {
				allInfos := funcCallsToInfos(funcCalls)
				if _, err := toolexec.Execute(r.Context(), allInfos, injectedTools, g.mcpClients, g.cfg.MCP, g.cfg.ToolHooks, g.cfg.Addr); err != nil {
					slog.Error("Responses: failed to run tool hooks", "error", err)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(respBody)
			logRecord(respBody, "")
			return
		}

		resp, toolSteps := g.handleResponsesToolCalls(r, provCfg, req, resp, injectedTools)
		steps = append(steps, toolSteps...)

		w.Header().Set("Content-Type", "application/json")
		finalBody, _ := json.Marshal(resp)
		w.Write(finalBody)
		logRecord(finalBody, "")
		return
	}
}

// handleResponsesToolCalls processes tool calls in a Responses API response.
// Returns the updated response and accumulated steps.
func (g *Gateway) handleResponsesToolCalls(r *http.Request, provCfg *config.ProviderConfig, req openai.ResponsesRequest, resp openai.ResponsesResponse, injectedTools []string) (openai.ResponsesResponse, []reqlog.Step) {
	var steps []reqlog.Step
	for i := range maxToolCallIterations {
		funcCalls, _ := extractFunctionCalls(resp.Output)
		injectedCalls, clientCalls := splitFuncCalls(funcCalls, injectedTools)
		allInfos := funcCallsToInfos(funcCalls)

		results, err := toolexec.Execute(r.Context(), allInfos, injectedTools, g.mcpClients, g.cfg.MCP, g.cfg.ToolHooks, g.cfg.Addr)
		if err != nil {
			slog.Error("Responses: failed to execute tools", "error", err)
			break
		}

		if len(injectedCalls) == 0 {
			break
		}

		slog.Debug("Responses: executing injected function calls", "iteration", i+1,
			"injected", len(injectedCalls), "client", len(clientCalls))

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
				steps = append(steps, buildStep(i+1, allInfos, results, llmReqBody, nil))
			} else {
				resp = newResp
				steps = append(steps, buildStep(i+1, allInfos, results, llmReqBody, llmRespBody))
			}
			break
		}

		newResp, llmRespBody, err := g.forwardResponsesRequest(provCfg, req)
		if err != nil {
			slog.Error("Responses: failed to forward after tool execution", "error", err, "iteration", i+1)
			steps = append(steps, buildStep(i+1, allInfos, results, llmReqBody, nil))
			break
		}
		resp = newResp
		steps = append(steps, buildStep(i+1, allInfos, results, llmReqBody, llmRespBody))
	}

	// filter out injected function_call items from final output
	resp.Output = openai.FilterResponsesOutput(resp.Output, injectedTools)

	return resp, steps
}

// handleResponsesStreamWithFirstResponse processes a streaming Responses API response
// with tool interception, using a pre-fetched first response (for failover support).
func (g *Gateway) handleResponsesStreamWithFirstResponse(w http.ResponseWriter, r *http.Request, provCfg *config.ProviderConfig, req openai.ResponsesRequest, injectedTools []string, firstResp []byte) ([]byte, []reqlog.Step, error) {
	defer func() { deferlog.DebugError(nil, "handle responses stream with first response") }()

	if len(injectedTools) == 0 {
		w.Write(firstResp)
		w.(http.Flusher).Flush()
		return firstResp, nil, nil
	}

	return g.processResponsesStreamToolCalls(w, r, provCfg, req, injectedTools, firstResp)
}

// processResponsesStreamToolCalls handles the tool call interception loop for streaming
// Responses API, starting from an already-fetched raw SSE response body.
func (g *Gateway) processResponsesStreamToolCalls(w http.ResponseWriter, r *http.Request, provCfg *config.ProviderConfig, req openai.ResponsesRequest, injectedTools []string, rawBody []byte) ([]byte, []reqlog.Step, error) {
	parser := newStreamParser(provCfg.Protocol, true)
	var steps []reqlog.Step

	for i := range maxToolCallIterations {
		if i > 0 {
			var err error
			rawBody, err = sendResponsesRawRequest(provCfg, req)
			if err != nil {
				return nil, steps, err
			}
		}

		events := protocol.ParseEvents(rawBody)
		sseInfos, hasInjected, err := parser.Parse(events, injectedTools)
		if err != nil {
			filteredEvents := parser.Filter(events, injectedTools)
			replayData := protocol.ReplayEvents(filteredEvents)
			w.Write(replayData)
			w.(http.Flusher).Flush()
			return replayData, steps, nil
		}

		results, execErr := toolexec.Execute(r.Context(), sseInfos, injectedTools, g.mcpClients, g.cfg.MCP, g.cfg.ToolHooks, g.cfg.Addr)
		if execErr != nil {
			slog.Error("Responses stream: failed to execute tools", "error", execErr)
		}

		if !hasInjected {
			filteredEvents := parser.Filter(events, injectedTools)
			replayData := protocol.ReplayEvents(filteredEvents)
			w.Write(replayData)
			w.(http.Flusher).Flush()
			return replayData, steps, nil
		}

		injectedInfos, clientInfos := splitInfos(sseInfos, injectedTools)
		slog.Debug("Responses stream: executing injected tool calls", "iteration", i+1)
		if execErr != nil {
			w.Write(rawBody)
			w.(http.Flusher).Flush()
			return rawBody, steps, nil
		}

		llmReqBody, _ := json.Marshal(req)
		steps = append(steps, buildStep(i+1, injectedInfos, results, llmReqBody, rawBody))

		completedResp := openai.ExtractCompletedResponse(events)
		if completedResp == nil {
			w.Write(rawBody)
			w.(http.Flusher).Flush()
			return rawBody, steps, nil
		}

		newInput, err := buildResponsesInput(req.Input, completedResp.Output, results)
		if err != nil {
			return nil, steps, err
		}

		req.Input = newInput
		removePreviousResponseID(&req)

		if len(clientInfos) > 0 {
			respBody, err := g.pipeResponsesStream(w, provCfg, req)
			return respBody, steps, err
		}
	}

	respBody, err := g.pipeResponsesStream(w, provCfg, req)
	return respBody, steps, err
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

	body, _, err := sendRequest(provCfg, protocolEndpoint(provCfg.Protocol, true), reqBody)
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
	body, _, err := sendRequest(provCfg, protocolEndpoint(provCfg.Protocol, true), reqBody)
	if err != nil {
		return nil, err
	}
	return body, nil
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
		if gjson.GetBytes(raw, "type").String() == "function_call" &&
			slices.Contains(injectedTools, gjson.GetBytes(raw, "name").String()) {
			return true
		}
	}
	return false
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

// buildResponsesInput builds new input for the next iteration:
// original input + output items (for reasoning etc.) + function_call_output items.
func buildResponsesInput(originalInput json.RawMessage, outputItems []json.RawMessage, results []toolexec.ToolResult) (json.RawMessage, error) {
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
