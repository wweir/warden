package gateway

import (
	"encoding/json"
	"slices"

	"github.com/wweir/warden/internal/toolexec"
	"github.com/wweir/warden/internal/mcp"
	"github.com/wweir/warden/internal/reqlog"
	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/protocol/openai"
)

// buildStep creates a reqlog.Step from tool call infos, results, and optional LLM request/response.
func buildStep(iteration int, calls []protocol.ToolCallInfo, results []toolexec.ToolResult, llmReq, llmResp json.RawMessage) reqlog.Step {
	return reqlog.Step{
		Iteration:   iteration,
		ToolCalls:   toToolCallEntries(calls),
		ToolResults: toToolResultEntries(results),
		LLMRequest:  llmReq,
		LLMResponse: llmResp,
	}
}

// toToolCallEntries converts protocol.ToolCallInfo slice to reqlog.ToolCallEntry slice.
func toToolCallEntries(calls []protocol.ToolCallInfo) []reqlog.ToolCallEntry {
	entries := make([]reqlog.ToolCallEntry, len(calls))
	for i, c := range calls {
		entries[i] = reqlog.ToolCallEntry{ID: c.ID, Name: c.Name, Arguments: c.Arguments}
	}
	return entries
}

// toToolResultEntries converts ToolResult slice to reqlog.ToolResultEntry slice.
func toToolResultEntries(results []toolexec.ToolResult) []reqlog.ToolResultEntry {
	entries := make([]reqlog.ToolResultEntry, len(results))
	for i, r := range results {
		entries[i] = reqlog.ToolResultEntry{CallID: r.CallID, Output: r.Output, IsError: r.IsError}
	}
	return entries
}

// namedItem is a constraint for types that have a GetName method.
type namedItem interface {
	GetName() string
}

// splitByInjected separates items into injected and client-originated using a generic constraint.
func splitByInjected[T namedItem](items []T, injectedTools []string) (injected, client []T) {
	for _, item := range items {
		if slices.Contains(injectedTools, item.GetName()) {
			injected = append(injected, item)
		} else {
			client = append(client, item)
		}
	}
	return
}

// splitCalls separates tool calls into injected and client-originated.
func splitCalls(calls []openai.ToolCall, injectedTools []string) (injected, client []openai.ToolCall) {
	return splitByInjected(calls, injectedTools)
}

// splitInfos separates ToolCallInfo into injected and client-originated.
func splitInfos(infos []protocol.ToolCallInfo, injectedTools []string) (injected, client []protocol.ToolCallInfo) {
	return splitByInjected(infos, injectedTools)
}

// toolCallsToInfos converts openai.ToolCall slice to protocol.ToolCallInfo slice.
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

// infosToToolCalls converts protocol.ToolCallInfo slice back to openai.ToolCall slice.
func infosToToolCalls(infos []protocol.ToolCallInfo) []openai.ToolCall {
	calls := make([]openai.ToolCall, len(infos))
	for i, info := range infos {
		calls[i] = openai.ToolCall{
			ID:   info.ID,
			Type: "function",
			Function: openai.FunctionCall{
				Name:      info.Name,
				Arguments: info.Arguments,
			},
		}
	}
	return calls
}

// toolResultsToMessages converts ToolResult slice to openai.Message slice (role: "tool").
func toolResultsToMessages(results []toolexec.ToolResult) []openai.Message {
	msgs := make([]openai.Message, len(results))
	for i, r := range results {
		msgs[i] = openai.Message{
			Role:       "tool",
			Content:    r.Output,
			ToolCallID: r.CallID,
		}
	}
	return msgs
}

// mcpToolsToToolDefs converts mcp.Tool slice to openai.ToolDef slice.
func mcpToolsToToolDefs(tools []mcp.Tool) []openai.ToolDef {
	defs := make([]openai.ToolDef, len(tools))
	for i, t := range tools {
		defs[i] = openai.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return defs
}

