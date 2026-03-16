package gateway

import (
	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/protocol/openai"
)

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
