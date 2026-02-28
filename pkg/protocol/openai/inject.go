package openai

import "encoding/json"

// ToolDef is a protocol-agnostic tool definition.
// The gateway converts mcp.Tool to ToolDef before calling Inject.
type ToolDef struct {
	Name        string
	Description string
	InputSchema any
}

// Inject injects tools into a Chat Completions request.
// Returns the list of injected tool names.
func Inject(req *ChatCompletionRequest, tools []ToolDef) []string {
	var injectedTools []string
	for _, t := range tools {
		req.Tools = append(req.Tools, Tool{
			Type: "function",
			Function: Function{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
		injectedTools = append(injectedTools, t.Name)
	}
	return injectedTools
}

// InjectResponsesTools injects tools into a Responses API request.
// Returns the list of injected tool names.
func InjectResponsesTools(req *ResponsesRequest, tools []ToolDef) []string {
	var injectedTools []string
	for _, t := range tools {
		toolDef := ResponsesFunctionTool{
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
