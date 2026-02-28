package gateway

import (
	"testing"

	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/protocol/openai"
)

func TestSplitByInjected_ToolCall(t *testing.T) {
	calls := []openai.ToolCall{
		{ID: "1", Function: openai.FunctionCall{Name: "tool_a"}},
		{ID: "2", Function: openai.FunctionCall{Name: "tool_b"}},
		{ID: "3", Function: openai.FunctionCall{Name: "injected_a"}},
		{ID: "4", Function: openai.FunctionCall{Name: "tool_c"}},
	}
	injected := []string{"injected_a"}

	injectedCalls, clientCalls := splitByInjected(calls, injected)

	if len(injectedCalls) != 1 {
		t.Errorf("injectedCalls len = %d, want 1", len(injectedCalls))
	}
	if len(clientCalls) != 3 {
		t.Errorf("clientCalls len = %d, want 3", len(clientCalls))
	}

	if injectedCalls[0].GetName() != "injected_a" {
		t.Errorf("injectedCalls[0].Name = %s, want injected_a", injectedCalls[0].GetName())
	}

	expectedClient := []string{"tool_a", "tool_b", "tool_c"}
	for i, tc := range clientCalls {
		if tc.GetName() != expectedClient[i] {
			t.Errorf("clientCalls[%d].Name = %s, want %s", i, tc.GetName(), expectedClient[i])
		}
	}
}

func TestSplitByInjected_ToolCallInfo(t *testing.T) {
	infos := []protocol.ToolCallInfo{
		{ID: "1", Name: "tool_a"},
		{ID: "2", Name: "injected_b"},
		{ID: "3", Name: "tool_c"},
	}
	injected := []string{"injected_b"}

	injectedInfos, clientInfos := splitByInjected(infos, injected)

	if len(injectedInfos) != 1 {
		t.Errorf("injectedInfos len = %d, want 1", len(injectedInfos))
	}
	if len(clientInfos) != 2 {
		t.Errorf("clientInfos len = %d, want 2", len(clientInfos))
	}

	if injectedInfos[0].GetName() != "injected_b" {
		t.Errorf("injectedInfos[0].Name = %s, want injected_b", injectedInfos[0].GetName())
	}
}

func TestSplitCalls_BackwardCompatible(t *testing.T) {
	calls := []openai.ToolCall{
		{ID: "1", Function: openai.FunctionCall{Name: "injected_x"}},
		{ID: "2", Function: openai.FunctionCall{Name: "client_y"}},
	}
	injected := []string{"injected_x"}

	injectedCalls, clientCalls := splitCalls(calls, injected)

	if len(injectedCalls) != 1 || injectedCalls[0].Function.Name != "injected_x" {
		t.Error("splitCalls failed for injected")
	}
	if len(clientCalls) != 1 || clientCalls[0].Function.Name != "client_y" {
		t.Error("splitCalls failed for client")
	}
}

func TestSplitInfos_BackwardCompatible(t *testing.T) {
	infos := []protocol.ToolCallInfo{
		{ID: "1", Name: "injected_z"},
		{ID: "2", Name: "client_w"},
	}
	injected := []string{"injected_z"}

	injectedInfos, clientInfos := splitInfos(infos, injected)

	if len(injectedInfos) != 1 || injectedInfos[0].Name != "injected_z" {
		t.Error("splitInfos failed for injected")
	}
	if len(clientInfos) != 1 || clientInfos[0].Name != "client_w" {
		t.Error("splitInfos failed for client")
	}
}
