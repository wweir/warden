package gateway

import (
	"encoding/json"

	"github.com/wweir/warden/internal/mcp"
	"github.com/wweir/warden/pkg/openai"
)

// Inject injects MCP tools into a Chat Completions request.
// Returns the list of injected tool names.
func Inject(req *openai.ChatCompletionRequest, tools []mcp.Tool) []string {
	var injectedTools []string
	for _, tool := range tools {
		req.Tools = append(req.Tools, openai.Tool{
			Type: "function",
			Function: openai.Function{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
		injectedTools = append(injectedTools, tool.Name)
	}
	return injectedTools
}

// InjectResponsesTools injects MCP tools into a Responses API request.
// Returns the list of injected tool names.
func InjectResponsesTools(req *openai.ResponsesRequest, tools []mcp.Tool) []string {
	var injectedTools []string
	for _, t := range tools {
		toolDef := openai.ResponsesFunctionTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
		}
		if t.InputSchema != nil {
			if b, err := json.Marshal(t.InputSchema); err == nil {
				toolDef.Parameters = b
			}
		}
		raw, err := json.Marshal(toolDef)
		if err != nil {
			continue
		}
		req.Tools = append(req.Tools, raw)
		injectedTools = append(injectedTools, t.Name)
	}
	return injectedTools
}
