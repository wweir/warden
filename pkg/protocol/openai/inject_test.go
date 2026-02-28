package openai

import (
	"encoding/json"
	"testing"
)

func TestInject(t *testing.T) {
	tests := []struct {
		name          string
		tools         []ToolDef
		wantToolCount int
		wantNames     []string
	}{
		{
			name:          "empty tools",
			tools:         []ToolDef{},
			wantToolCount: 0,
			wantNames:     []string{},
		},
		{
			name: "single tool",
			tools: []ToolDef{
				{
					Name:        "test_tool",
					Description: "A test tool",
					InputSchema: map[string]any{"type": "object"},
				},
			},
			wantToolCount: 1,
			wantNames:     []string{"test_tool"},
		},
		{
			name: "multiple tools",
			tools: []ToolDef{
				{Name: "tool1", Description: "First tool", InputSchema: map[string]any{"type": "object"}},
				{Name: "tool2", Description: "Second tool", InputSchema: map[string]any{"type": "object"}},
				{Name: "tool3", Description: "Third tool", InputSchema: map[string]any{"type": "object"}},
			},
			wantToolCount: 3,
			wantNames:     []string{"tool1", "tool2", "tool3"},
		},
		{
			name: "tool with nil schema",
			tools: []ToolDef{
				{Name: "tool_nil", Description: "Tool with nil schema", InputSchema: nil},
			},
			wantToolCount: 1,
			wantNames:     []string{"tool_nil"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ChatCompletionRequest{Model: "gpt-4o"}
			got := Inject(req, tt.tools)

			if len(req.Tools) != tt.wantToolCount {
				t.Errorf("Inject() got %d tools, want %d", len(req.Tools), tt.wantToolCount)
			}

			if len(got) != len(tt.wantNames) {
				t.Errorf("Inject() returned %d names, want %d", len(got), len(tt.wantNames))
			}

			for i, name := range tt.wantNames {
				if i < len(got) && got[i] != name {
					t.Errorf("Inject() name[%d] = %q, want %q", i, got[i], name)
				}
			}

			// Verify tool structure
			for i, tool := range req.Tools {
				if tool.Type != "function" {
					t.Errorf("Inject() tool[%d].Type = %q, want 'function'", i, tool.Type)
				}
				if i < len(tt.tools) && tool.Function.Name != tt.tools[i].Name {
					t.Errorf("Inject() tool[%d].Function.Name = %q, want %q", i, tool.Function.Name, tt.tools[i].Name)
				}
			}
		})
	}
}

func TestInjectResponsesTools(t *testing.T) {
	tests := []struct {
		name          string
		tools         []ToolDef
		wantToolCount int
		wantNames     []string
	}{
		{
			name:          "empty tools",
			tools:         []ToolDef{},
			wantToolCount: 0,
			wantNames:     []string{},
		},
		{
			name: "single tool",
			tools: []ToolDef{
				{
					Name:        "test_tool",
					Description: "A test tool",
					InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
			wantToolCount: 1,
			wantNames:     []string{"test_tool"},
		},
		{
			name: "multiple tools",
			tools: []ToolDef{
				{Name: "tool1", Description: "First tool", InputSchema: map[string]any{"type": "object"}},
				{Name: "tool2", Description: "Second tool", InputSchema: map[string]any{"type": "object"}},
			},
			wantToolCount: 2,
			wantNames:     []string{"tool1", "tool2"},
		},
		{
			name: "tool with nil schema",
			tools: []ToolDef{
				{Name: "tool_nil", Description: "Tool with nil schema", InputSchema: nil},
			},
			wantToolCount: 1,
			wantNames:     []string{"tool_nil"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ResponsesRequest{Model: "gpt-4o", Input: json.RawMessage(`"test"`)}
			got := InjectResponsesTools(req, tt.tools)

			if len(req.Tools) != tt.wantToolCount {
				t.Errorf("InjectResponsesTools() got %d tools, want %d", len(req.Tools), tt.wantToolCount)
			}

			if len(got) != len(tt.wantNames) {
				t.Errorf("InjectResponsesTools() returned %d names, want %d", len(got), len(tt.wantNames))
			}

			for i, name := range tt.wantNames {
				if i < len(got) && got[i] != name {
					t.Errorf("InjectResponsesTools() name[%d] = %q, want %q", i, got[i], name)
				}
			}

			// Verify tools are valid JSON
			for i, raw := range req.Tools {
				var tool ResponsesFunctionTool
				if err := json.Unmarshal(raw, &tool); err != nil {
					t.Errorf("InjectResponsesTools() tool[%d] is not valid JSON: %v", i, err)
				}
				if tool.Type != "function" {
					t.Errorf("InjectResponsesTools() tool[%d].Type = %q, want 'function'", i, tool.Type)
				}
			}
		})
	}
}

func TestInjectResponsesTools_MarshalingError(t *testing.T) {
	// Test that marshaling errors are handled gracefully
	req := &ResponsesRequest{Model: "gpt-4o", Input: json.RawMessage(`"test"`)}

	tools := []ToolDef{
		{Name: "valid_tool", Description: "Valid tool", InputSchema: map[string]any{"type": "object"}},
	}

	got := InjectResponsesTools(req, tools)

	// Should still inject the valid tool
	if len(got) != 1 || got[0] != "valid_tool" {
		t.Errorf("InjectResponsesTools() = %v, want ['valid_tool']", got)
	}

	if len(req.Tools) != 1 {
		t.Errorf("InjectResponsesTools() injected %d tools, want 1", len(req.Tools))
	}
}
