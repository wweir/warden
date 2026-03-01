package toolexec

import (
	"context"
	"encoding/json"
	"log/slog"
	"path"
	"slices"
	"strings"

	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/mcp"
	"github.com/wweir/warden/pkg/mcphook"
	"github.com/wweir/warden/pkg/protocol"
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
		if !slices.Contains(injectedTools, tc.Name) {
			continue
		}

		slog.Debug("Executing injected tool", "tool", tc.Name)

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

		hooks := matchHooks(tc.Name, toolHooks)

		hctx := mcphook.HookContext{
			MCPName:   mcpName,
			ToolName:  originalToolName,
			CallID:    tc.ID,
			Arguments: json.RawMessage(tc.Arguments),
		}

		if err := mcphook.RunPre(ctx, mcpName, originalToolName, gatewayAddr, hooks, hctx); err != nil {
			slog.Warn("Tool call blocked by pre hook", "mcp", mcpName, "tool", originalToolName, "error", err)
			results = append(results, ToolResult{
				CallID:  tc.ID,
				Output:  "Error: tool call blocked by pre hook: " + err.Error(),
				IsError: true,
			})
			continue
		}

		result, callErr := mcpClient.CallTool(ctx, originalToolName, json.RawMessage(tc.Arguments))

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

// matchHooks returns all HookConfig entries whose Match pattern matches the tool name.
func matchHooks(toolName string, rules []*config.HookRuleConfig) []config.HookConfig {
	var hooks []config.HookConfig
	for _, rule := range rules {
		matched, err := path.Match(rule.Match, toolName)
		if err != nil {
			slog.Warn("Invalid hook rule match pattern", "pattern", rule.Match, "error", err)
			continue
		}
		if matched {
			hooks = append(hooks, rule.Hook)
		}
	}
	return hooks
}

// splitToolName splits "mcp_name__tool_name" into [mcpName, toolName].
func splitToolName(name string) []string {
	return strings.SplitN(name, "__", 2)
}
