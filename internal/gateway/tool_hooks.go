package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/protocol/openai"
	"github.com/wweir/warden/pkg/toolhook"
)

func (g *Gateway) runRouteToolHooks(ctx context.Context, calls []protocol.ToolCallInfo, op string) {
	for _, call := range calls {
		mcpName, toolName := splitObservedToolName(call.Name)
		hooks := toolhook.MatchHooks(call.Name, routeHooksFromContext(ctx))
		hctx := toolhook.CallContext{
			ToolName:  toolName,
			FullName:  call.Name,
			MCPName:   mcpName,
			CallID:    call.ID,
			Arguments: json.RawMessage(call.Arguments),
		}
		if err := toolhook.RunPre(ctx, g.cfg.Addr, hooks, hctx); err != nil {
			slog.Warn(op, "tool", call.Name, "error", err)
			continue
		}
		go toolhook.RunPost(ctx, g.cfg.Addr, hooks, hctx)
	}
}

func splitObservedToolName(name string) (mcpName string, toolName string) {
	parts := strings.SplitN(name, "__", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", name
}

func parseChatToolCalls(protocolType string, body []byte, stream bool) []protocol.ToolCallInfo {
	if stream {
		parser := newStreamParser(protocolType, false)
		infos, err := parser.Parse(protocol.ParseEvents(body))
		if err == nil {
			return infos
		}
		return nil
	}

	resp, err := unmarshalProtocolResponse(protocolType, body)
	if err != nil || len(resp.Choices) == 0 {
		return nil
	}
	return toolCallsToInfos(resp.Choices[0].Message.ToolCalls)
}

func parseResponsesToolCalls(body []byte, stream bool) []protocol.ToolCallInfo {
	if stream {
		parser := newStreamParser("openai", true)
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
	funcCalls, _ := extractFunctionCalls(resp.Output)
	return funcCallsToInfos(funcCalls)
}
