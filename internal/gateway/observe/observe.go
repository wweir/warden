package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	proxypkg "github.com/wweir/warden/internal/gateway/proxy"
	requestctxpkg "github.com/wweir/warden/internal/gateway/requestctx"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
	tokenusagepkg "github.com/wweir/warden/internal/gateway/tokenusage"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/internal/reqlog"
	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/protocol/anthropic"
	"github.com/wweir/warden/pkg/protocol/openai"
	"github.com/wweir/warden/pkg/toolhook"
)

type StreamLogAssembler func(respBody []byte) (assembled []byte, fallback []byte, err error)

type InferenceLogParams struct {
	StartTime  time.Time
	RequestID  string
	Route      string
	Endpoint   string
	Model      string
	APIKey     string
	Stream     bool
	Provider   string
	UserAgent  string
	Request    []byte
	Failovers  []reqlog.Failover
	MetricTags telemetrypkg.Labels
}

func NewInferenceLogParams(r *http.Request, startTime time.Time, requestID, route, endpoint, model string, stream bool, requestBody []byte, failovers []reqlog.Failover, labels telemetrypkg.Labels, provider string) InferenceLogParams {
	return InferenceLogParams{
		StartTime:  startTime,
		RequestID:  requestID,
		Route:      route,
		Endpoint:   endpoint,
		Model:      model,
		APIKey:     requestctxpkg.APIKeyNameFromContext(r.Context()),
		Stream:     stream,
		Provider:   provider,
		UserAgent:  r.UserAgent(),
		Request:    requestBody,
		Failovers:  failovers,
		MetricTags: labels,
	}
}

func RecordInferenceLog(params InferenceLogParams, respBody []byte, errMsg string, assembleStream StreamLogAssembler, observation tokenusagepkg.Observation, recordTokens func(labels telemetrypkg.Labels, usage tokenusagepkg.Observation, durationMs int64), emit func(reqlog.Record)) {
	rec := reqlog.Record{
		Timestamp:   params.StartTime,
		RequestID:   params.RequestID,
		Route:       params.Route,
		Endpoint:    params.Endpoint,
		Model:       params.Model,
		APIKey:      params.APIKey,
		Stream:      params.Stream,
		Provider:    params.Provider,
		UserAgent:   params.UserAgent,
		DurationMs:  time.Since(params.StartTime).Milliseconds(),
		Error:       errMsg,
		Fingerprint: reqlog.BuildFingerprint(params.Request),
		Request:     params.Request,
		Response:    respBody,
		Failovers:   params.Failovers,
	}

	if len(respBody) > 0 && errMsg == "" {
		rec.TokenUsage = &reqlog.TokenUsage{
			PromptTokens:     observation.PromptTokens,
			CompletionTokens: observation.CompletionTokens,
			TotalTokens:      observation.TotalTokens,
			Source:           observation.SourceLabel(),
			Completeness:     observation.CompletenessLabel(),
		}

		if params.Stream && assembleStream != nil {
			assembled, fallback, err := assembleStream(respBody)
			if err == nil {
				rec.Response = assembled
			} else {
				if len(fallback) == 0 {
					fallback = respBody
				}
				rec.Response = proxypkg.MarshalRawStreamForLog(fallback)
			}
		}

		if recordTokens != nil {
			recordTokens(params.MetricTags, observation, rec.DurationMs)
		}
	}

	emit(rec)
}

func PublishPendingInferenceLog(params InferenceLogParams, publish func(reqlog.Record)) {
	publish(reqlog.Record{
		Timestamp:   params.StartTime,
		RequestID:   params.RequestID,
		Route:       params.Route,
		Endpoint:    params.Endpoint,
		Model:       params.Model,
		APIKey:      params.APIKey,
		Stream:      params.Stream,
		Pending:     true,
		Provider:    params.Provider,
		UserAgent:   params.UserAgent,
		Fingerprint: reqlog.BuildFingerprint(params.Request),
		Request:     params.Request,
		Failovers:   params.Failovers,
	})
}

func AssembleAnthropicStreamLog(respBody []byte) ([]byte, []byte, error) {
	assembled := anthropic.AssembleStream(respBody)
	if assembled == nil {
		return nil, respBody, fmt.Errorf("assemble anthropic stream")
	}
	return assembled, respBody, nil
}

func RunRouteToolHooks(ctx context.Context, gatewayAddr string, calls []protocol.ToolCallInfo, op string) {
	for _, call := range calls {
		mcpName, toolName := splitObservedToolName(call.Name)
		hooks := toolhook.MatchHooks(call.Name, requestctxpkg.RouteHooksFromContext(ctx))
		hctx := toolhook.CallContext{
			ToolName:  toolName,
			FullName:  call.Name,
			MCPName:   mcpName,
			CallID:    call.ID,
			Arguments: json.RawMessage(call.Arguments),
		}
		if err := toolhook.RunPre(ctx, gatewayAddr, hooks, hctx); err != nil {
			slog.Warn(op, "tool", call.Name, "error", err)
			continue
		}
		go toolhook.RunPost(postHookContext(ctx), gatewayAddr, hooks, hctx)
	}
}

func RunFirstChoiceToolHooks(ctx context.Context, gatewayAddr string, resp openai.ChatCompletionResponse, op string) {
	if len(resp.Choices) == 0 {
		return
	}
	RunRouteToolHooks(ctx, gatewayAddr, toolCallsToInfos(resp.Choices[0].Message.ToolCalls), op)
}

func ParseChatToolCalls(protocolType string, body []byte, stream bool) []protocol.ToolCallInfo {
	if stream {
		parser := upstreampkg.NewStreamParser(protocolType, false)
		infos, err := parser.Parse(protocol.ParseEvents(body))
		if err == nil {
			return infos
		}
		return nil
	}

	resp, err := upstreampkg.UnmarshalProtocolResponse(protocolType, body)
	if err != nil || len(resp.Choices) == 0 {
		return nil
	}
	return toolCallsToInfos(resp.Choices[0].Message.ToolCalls)
}

func ParseResponsesToolCalls(body []byte, stream bool) []protocol.ToolCallInfo {
	if stream {
		parser := upstreampkg.NewStreamParser("openai", true)
		infos, err := parser.Parse(protocol.ParseEvents(body))
		if err == nil {
			return infos
		}
		return nil
	}

	var resp openai.ResponsesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	funcCalls, _ := ExtractFunctionCalls(resp.Output)
	return FuncCallsToInfos(funcCalls)
}

func ExtractFunctionCalls(output []json.RawMessage) (funcCalls []openai.FunctionCallItem, others []json.RawMessage) {
	for _, raw := range output {
		if gjsonType(raw) != "function_call" {
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

func FuncCallsToInfos(calls []openai.FunctionCallItem) []protocol.ToolCallInfo {
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

func toolCallsToInfos(calls []openai.ToolCall) []protocol.ToolCallInfo {
	infos := make([]protocol.ToolCallInfo, len(calls))
	for i, tc := range calls {
		infos[i] = protocol.ToolCallInfo{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		}
	}
	return infos
}

func splitObservedToolName(name string) (mcpName string, toolName string) {
	parts := strings.SplitN(name, "__", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", name
}

func postHookContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	// Post hooks are audit-only asynchronous work. Keep request-scoped values
	// such as route hook config, but do not let downstream cancellation kill the
	// goroutine before the hook's own timeout applies.
	return context.WithoutCancel(ctx)
}

func gjsonType(raw json.RawMessage) string {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if typ, ok := payload["type"].(string); ok {
		return typ
	}
	return ""
}
