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

func TestBuildToolHookSuggestionsForRoute(t *testing.T) {
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	records := []reqlog.Record{
		{
			Timestamp: now,
			Route:     "/openai",
			Response:  json.RawMessage(`{"output":[{"type":"function_call","call_id":"call-1","name":"web_search","arguments":"{\"q\":\"openai\"}"}]}`),
		},
		{
			Timestamp: now.Add(time.Minute),
			Route:     "/anthropic",
			Response:  json.RawMessage(`{"content":[{"type":"tool_use","id":"call-2","name":"filesystem__read_file","input":{"path":"/tmp/a"}}]}`),
		},
	}

	resp := buildToolHookSuggestionsForRoute(records, "/openai")
	if resp.RecentLogs != 1 {
		t.Fatalf("expected 1 filtered log, got %d", resp.RecentLogs)
	}
	if len(resp.Suggestions) != 1 {
		t.Fatalf("expected 1 filtered suggestion, got %d", len(resp.Suggestions))
	}
	if resp.Suggestions[0].Match != "web_search" {
		t.Fatalf("unexpected filtered suggestion: %#v", resp.Suggestions[0])
	}
}

func TestExtractToolCallsFromJSONSupportsResponsesEnvelope(t *testing.T) {
	raw := json.RawMessage(`{
		"response":{
			"output":[
				{"type":"function_call","call_id":"call-9","name":"web_search","arguments":"{\"q\":\"responses\"}"}
			]
		}
	}`)

	calls := extractToolCallsFromJSON(raw)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "web_search" || calls[0].Arguments != "{\"q\":\"responses\"}" {
		t.Fatalf("unexpected call: %#v", calls[0])
	}
}

func TestExtractToolCallsFromJSONSupportsResponsesSSEString(t *testing.T) {
	sse := `event: response.output_item.added
data: {"item":{"type":"function_call","call_id":"call-sse","name":"web_search"}}

event: response.completed
data: {"response":{"output":[{"type":"function_call","call_id":"call-sse","name":"web_search","arguments":"{\"q\":\"warden\"}"}]}}
`
	raw, err := json.Marshal(sse)
	if err != nil {
		t.Fatalf("marshal sse string: %v", err)
	}

	calls := extractToolCallsFromJSON(raw)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls from sse events, got %d", len(calls))
	}
	if calls[0].Name != "web_search" {
		t.Fatalf("unexpected first call: %#v", calls[0])
	}
	if calls[1].Arguments != "{\"q\":\"warden\"}" {
		t.Fatalf("unexpected completed-event args: %#v", calls[1])
	}
}

func TestBuildToolHookSuggestionsPrefersCompletedArgsFromSSE(t *testing.T) {
	sse := `event: response.output_item.added
data: {"item":{"type":"function_call","call_id":"call-sse","name":"web_search"}}

event: response.completed
data: {"response":{"output":[{"type":"function_call","call_id":"call-sse","name":"web_search","arguments":"{\"q\":\"warden\"}"}]}}
`
	raw, err := json.Marshal(sse)
	if err != nil {
		t.Fatalf("marshal sse string: %v", err)
	}

	resp := buildToolHookSuggestions([]reqlog.Record{{
		Timestamp: time.Date(2026, 3, 9, 12, 3, 0, 0, time.UTC),
		Route:     "/openai",
		Model:     "gpt-4.1-mini",
		Provider:  "primary",
		Response:  raw,
	}})
	if len(resp.Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(resp.Suggestions))
	}
	if resp.Suggestions[0].SampleArguments != "{\"q\":\"warden\"}" {
		t.Fatalf("expected completed args to win, got %q", resp.Suggestions[0].SampleArguments)
	}
}
