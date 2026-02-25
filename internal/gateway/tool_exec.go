package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/mcp"
	"github.com/wweir/warden/pkg/mcphook"
	"github.com/wweir/warden/pkg/sse"

	"github.com/sower-proxy/deferlog/v2"
)

// ToolResult contains the execution result for a single tool call.
type ToolResult struct {
	CallID  string // matches ToolCallInfo.ID
	Output  string // tool execution result
	IsError bool   // whether the output is an error message
}

// Execute executes injected tool calls and returns results.
// Only calls whose Name appears in injectedTools are executed.
// mcpCfgs is used to look up per-tool disabled flags and hook configurations.
// gatewayAddr is the gateway's own listening address, used for ai-type hooks.
func Execute(ctx context.Context, calls []sse.ToolCallInfo, injectedTools []string, mcpClients map[string]*mcp.Client, mcpCfgs map[string]*config.MCPConfig, gatewayAddr string) (results []ToolResult, err error) {
	defer func() { deferlog.DebugError(err, "execute tool calls") }()

	for _, tc := range calls {
		if !slices.Contains(injectedTools, tc.Name) {
			continue
		}

		slog.Debug("Executing injected tool", "tool", tc.Name)

		// split "mcp_name__tool_name" into [mcpName, toolName]
		parts := splitToolName(tc.Name)
		if len(parts) != 2 {
			slog.Warn("Invalid tool name format", "tool", tc.Name)
			results = append(results, ToolResult{
				CallID:  tc.ID,
				Output:  "Error: invalid tool name format: " + tc.Name,
				IsError: true,
			})
			continue
		}
		mcpName, originalToolName := parts[0], parts[1]

		// check disabled flag (second line of defense after collectTools filtering)
		if mcpCfg, ok := mcpCfgs[mcpName]; ok {
			if toolCfg, ok := mcpCfg.Tools[originalToolName]; ok && toolCfg.Disabled {
				slog.Warn("Tool is disabled", "mcp", mcpName, "tool", originalToolName)
				results = append(results, ToolResult{
					CallID:  tc.ID,
					Output:  "Error: tool is disabled: " + originalToolName,
					IsError: true,
				})
				continue
			}
		}

		mcpClient, exists := mcpClients[mcpName]
		if !exists {
			slog.Warn("MCP client not found", "mcp_name", mcpName, "tool", tc.Name)
			results = append(results, ToolResult{
				CallID:  tc.ID,
				Output:  "Error: MCP client not found: " + mcpName,
				IsError: true,
			})
			continue
		}

		// collect hooks for this tool
		var hooks []config.HookConfig
		if mcpCfg, ok := mcpCfgs[mcpName]; ok {
			if toolCfg, ok := mcpCfg.Tools[originalToolName]; ok {
				hooks = toolCfg.Hooks
			}
		}

		hctx := mcphook.HookContext{
			MCPName:   mcpName,
			ToolName:  originalToolName,
			CallID:    tc.ID,
			Arguments: json.RawMessage(tc.Arguments),
		}

		// run pre hooks; any failure blocks the tool call
		if err := mcphook.RunPre(ctx, mcpName, originalToolName, gatewayAddr, hooks, hctx); err != nil {
			slog.Warn("Tool call blocked by pre hook", "mcp", mcpName, "tool", originalToolName, "error", err)
			results = append(results, ToolResult{
				CallID:  tc.ID,
				Output:  "Error: tool call blocked by pre hook: " + err.Error(),
				IsError: true,
			})
			continue
		}

		// call MCP tool with original name
		result, callErr := mcpClient.CallTool(ctx, originalToolName, json.RawMessage(tc.Arguments))

		// run post hooks asynchronously (audit only, does not block)
		hctx.Result = result
		hctx.IsError = callErr != nil
		go mcphook.RunPost(ctx, mcpName, originalToolName, gatewayAddr, hooks, hctx)

		if callErr != nil {
			slog.Error("Failed to call tool", "tool", tc.Name, "error", callErr)
			output := result
			if output == "" {
				output = "Error: " + callErr.Error()
			}
			results = append(results, ToolResult{
				CallID:  tc.ID,
				Output:  output,
				IsError: true,
			})
			continue
		}

		results = append(results, ToolResult{
			CallID: tc.ID,
			Output: result,
		})
	}

	return results, nil
}

// splitToolName splits "mcp_name__tool_name" into [mcpName, toolName]
func splitToolName(name string) []string {
	return strings.SplitN(name, "__", 2)
}
