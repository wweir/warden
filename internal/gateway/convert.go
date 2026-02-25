package gateway

import (
	"encoding/json"
	"slices"

	"github.com/wweir/warden/internal/reqlog"
	"github.com/wweir/warden/pkg/openai"
	"github.com/wweir/warden/pkg/sse"
)

// buildStep creates a reqlog.Step from tool call infos, results, and optional LLM request/response.
func buildStep(iteration int, calls []sse.ToolCallInfo, results []ToolResult, llmReq, llmResp json.RawMessage) reqlog.Step {
	return reqlog.Step{
		Iteration:   iteration,
		ToolCalls:   toToolCallEntries(calls),
		ToolResults: toToolResultEntries(results),
		LLMRequest:  llmReq,
		LLMResponse: llmResp,
	}
}

// toToolCallEntries converts sse.ToolCallInfo slice to reqlog.ToolCallEntry slice.
func toToolCallEntries(calls []sse.ToolCallInfo) []reqlog.ToolCallEntry {
	entries := make([]reqlog.ToolCallEntry, len(calls))
	for i, c := range calls {
		entries[i] = reqlog.ToolCallEntry{ID: c.ID, Name: c.Name, Arguments: c.Arguments}
	}
	return entries
}

// toToolResultEntries converts ToolResult slice to reqlog.ToolResultEntry slice.
func toToolResultEntries(results []ToolResult) []reqlog.ToolResultEntry {
	entries := make([]reqlog.ToolResultEntry, len(results))
	for i, r := range results {
		entries[i] = reqlog.ToolResultEntry{CallID: r.CallID, Output: r.Output, IsError: r.IsError}
	}
	return entries
}

// splitCalls separates tool calls into injected and client-originated.
func splitCalls(calls []openai.ToolCall, injectedTools []string) (injected, client []openai.ToolCall) {
	for _, tc := range calls {
		if slices.Contains(injectedTools, tc.Function.Name) {
			injected = append(injected, tc)
		} else {
			client = append(client, tc)
		}
	}
	return
}

// splitInfos separates ToolCallInfo into injected and client-originated.
func splitInfos(infos []sse.ToolCallInfo, injectedTools []string) (injected, client []sse.ToolCallInfo) {
	for _, info := range infos {
		if slices.Contains(injectedTools, info.Name) {
			injected = append(injected, info)
		} else {
			client = append(client, info)
		}
	}
	return
}

// toolCallsToInfos converts openai.ToolCall slice to sse.ToolCallInfo slice.
func toolCallsToInfos(calls []openai.ToolCall) []sse.ToolCallInfo {
	infos := make([]sse.ToolCallInfo, len(calls))
	for i, tc := range calls {
		infos[i] = sse.ToolCallInfo{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		}
	}
	return infos
}

// infosToToolCalls converts sse.ToolCallInfo slice back to openai.ToolCall slice.
func infosToToolCalls(infos []sse.ToolCallInfo) []openai.ToolCall {
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
func toolResultsToMessages(results []ToolResult) []openai.Message {
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

// filterToolCalls removes injected tool calls, keeping only client-originated ones.
func filterToolCalls(toolCalls []openai.ToolCall, injectedTools []string) []openai.ToolCall {
	var filtered []openai.ToolCall
	for _, tc := range toolCalls {
		if !slices.Contains(injectedTools, tc.Function.Name) {
			filtered = append(filtered, tc)
		}
	}
	return filtered
}
