package toolexec

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"

	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/mcp"
	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/toolhook"
)

// ToolResult contains the execution result for a single tool call.
type ToolResult struct {
	CallID  string
	Output  string
	IsError bool
}

// Execute executes injected tool calls and returns results.
func Execute(ctx context.Context, calls []protocol.ToolCallInfo, injectedTools []string, mcpClients map[string]*mcp.Client, mcpCfgs map[string]*config.MCPConfig, toolHooks []*config.HookRuleConfig, gatewayAddr string) (results []ToolResult, err error) {
	defer func() { deferlog.DebugError(err, "execute tool calls") }()

	for _, tc := range calls {
		isInjected := slices.Contains(injectedTools, tc.Name)
		mcpName, originalToolName := parseToolName(tc.Name)

		hooks := toolhook.MatchHooks(tc.Name, toolHooks)
		hctx := toolhook.CallContext{
			ToolName:  originalToolName,
			FullName:  tc.Name,
			MCPName:   mcpName,
			CallID:    tc.ID,
			Arguments: json.RawMessage(tc.Arguments),
		}

		if err := toolhook.RunPre(ctx, gatewayAddr, hooks, hctx); err != nil {
			slog.Warn("Tool call blocked by pre hook", "tool", tc.Name, "error", err)
			if isInjected {
				results = append(results, ToolResult{
					CallID:  tc.ID,
					Output:  "Error: tool call blocked by pre hook: " + err.Error(),
					IsError: true,
				})
			} else {
				slog.Warn("Blocked non-injected tool call is audit-only", "tool", tc.Name)
			}
			continue
		}

		if !isInjected {
			continue
		}

		slog.Debug("Executing injected tool", "tool", tc.Name)

		if mcpName == "" {
			slog.Warn("Invalid tool name format", "tool", tc.Name)
			results = append(results, ToolResult{
				CallID:  tc.ID,
				Output:  "Error: invalid tool name format: " + tc.Name,
				IsError: true,
			})
			continue
		}

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

		result, callErr := mcpClient.CallTool(ctx, originalToolName, json.RawMessage(tc.Arguments))

		hctx.Result = result
		hctx.IsError = callErr != nil
		go toolhook.RunPost(ctx, gatewayAddr, hooks, hctx)

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

// parseToolName parses "mcp_name__tool_name" and falls back to raw name for non-MCP tools.
func parseToolName(name string) (mcpName string, toolName string) {
	parts := strings.SplitN(name, "__", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", name
}
