package gateway

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/wweir/warden/internal/reqlog"
)

func TestBuildToolHookSuggestions(t *testing.T) {
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)

	records := []reqlog.Record{
		{
			Timestamp: now.Add(-2 * time.Minute),
			Route:     "/openai",
			Model:     "gpt-4o",
			Provider:  "primary",
			Response: json.RawMessage(`{
				"choices":[{"message":{"tool_calls":[
					{"id":"call-1","function":{"name":"fs__write_file","arguments":"{\"path\":\"/tmp/a.txt\"}"}}
				]}}]
			}`),
			Steps: []reqlog.Step{{
				ToolCalls: []reqlog.ToolCallEntry{{
					ID:        "call-1",
					Name:      "fs__write_file",
					Arguments: "{\"path\":\"/tmp/a.txt\"}",
				}},
			}},
		},
		{
			Timestamp: now.Add(-1 * time.Minute),
			Route:     "/openai",
			Model:     "gpt-4.1-mini",
			Provider:  "backup",
			Response: json.RawMessage(`{
				"output":[
					{"type":"function_call","call_id":"call-2","name":"web_search","arguments":"{\"q\":\"warden\"}"}
				]
			}`),
		},
		{
			Timestamp: now,
			Route:     "/anthropic",
			Model:     "claude-3-7-sonnet",
			Provider:  "anthropic",
			Response: json.RawMessage(`{
				"message":{
					"content":[
						{"type":"tool_use","id":"call-3","name":"filesystem__read_file","input":{"path":"/tmp/b.txt"}}
					]
				}
			}`),
		},
	}

	resp := buildToolHookSuggestions(records)
	if resp.RecentLogs != len(records) {
		t.Fatalf("expected recent_logs=%d, got %d", len(records), resp.RecentLogs)
	}
	if len(resp.Suggestions) != 3 {
		t.Fatalf("expected 3 suggestions, got %d", len(resp.Suggestions))
	}

	fsWrite := resp.Suggestions[0]
	if fsWrite.Match != "filesystem__read_file" {
		t.Fatalf("expected most recent suggestion first, got %q", fsWrite.Match)
	}

	var writeSuggestion toolHookSuggestion
	for _, suggestion := range resp.Suggestions {
		if suggestion.Match == "fs__write_file" {
			writeSuggestion = suggestion
			break
		}
	}
	if writeSuggestion.Count != 1 {
		t.Fatalf("expected deduped count=1 for fs__write_file, got %d", writeSuggestion.Count)
	}
	if writeSuggestion.MCPName != "fs" || writeSuggestion.BaseToolName != "write_file" {
		t.Fatalf("unexpected split name: mcp=%q base=%q", writeSuggestion.MCPName, writeSuggestion.BaseToolName)
	}
	if writeSuggestion.PrimaryRoute != "/openai" || writeSuggestion.PrimaryModel != "gpt-4o" {
		t.Fatalf("unexpected primary hint: route=%q model=%q", writeSuggestion.PrimaryRoute, writeSuggestion.PrimaryModel)
	}
	if len(writeSuggestion.Routes) != 1 || len(writeSuggestion.Routes[0].Providers) != 1 || writeSuggestion.Routes[0].Providers[0] != "primary" {
		t.Fatalf("unexpected route/provider hints: %#v", writeSuggestion.Routes)
	}

	var webSearch toolHookSuggestion
	for _, suggestion := range resp.Suggestions {
		if suggestion.Match == "web_search" {
			webSearch = suggestion
			break
		}
	}
	if webSearch.BaseToolName != "web_search" || webSearch.MCPName != "" {
		t.Fatalf("unexpected native tool split: %#v", webSearch)
	}
	if webSearch.SampleArguments != "{\"q\":\"warden\"}" {
		t.Fatalf("unexpected sample args: %q", webSearch.SampleArguments)
	}
}
