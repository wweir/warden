package gateway

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/internal/reqlog"
)

type toolHookSuggestionResponse struct {
	RecentLogs  int                  `json:"recent_logs"`
	Suggestions []toolHookSuggestion `json:"suggestions"`
}

type toolHookSuggestion struct {
	Match           string                    `json:"match"`
	ToolName        string                    `json:"tool_name"`
	BaseToolName    string                    `json:"base_tool_name"`
	MCPName         string                    `json:"mcp_name,omitempty"`
	Count           int                       `json:"count"`
	LastSeen        time.Time                 `json:"last_seen"`
	SampleArguments string                    `json:"sample_arguments,omitempty"`
	PrimaryRoute    string                    `json:"primary_route,omitempty"`
	PrimaryModel    string                    `json:"primary_model,omitempty"`
	Routes          []toolHookSuggestionRoute `json:"routes,omitempty"`
}

type toolHookSuggestionRoute struct {
	Route        string    `json:"route"`
	Count        int       `json:"count"`
	LastSeen     time.Time `json:"last_seen"`
	PrimaryModel string    `json:"primary_model,omitempty"`
	Models       []string  `json:"models,omitempty"`
	Providers    []string  `json:"providers,omitempty"`
}

type toolCallObservation struct {
	ID        string
	Name      string
	Arguments string
}

type toolSuggestionAgg struct {
	Match           string
	ToolName        string
	BaseToolName    string
	MCPName         string
	Count           int
	LastSeen        time.Time
	SampleArguments string
	Routes          map[string]*toolSuggestionRouteAgg
}

type toolSuggestionRouteAgg struct {
	Route          string
	Count          int
	LastSeen       time.Time
	ModelCounts    map[string]int
	ProviderCounts map[string]int
}

func buildToolHookSuggestions(records []reqlog.Record) toolHookSuggestionResponse {
	aggs := map[string]*toolSuggestionAgg{}

	for _, rec := range records {
		for _, call := range extractToolCallObservations(rec) {
			agg, ok := aggs[call.Name]
			if !ok {
				mcpName, baseToolName := splitToolName(call.Name)
				agg = &toolSuggestionAgg{
					Match:        call.Name,
					ToolName:     call.Name,
					BaseToolName: baseToolName,
					MCPName:      mcpName,
					Routes:       map[string]*toolSuggestionRouteAgg{},
				}
				aggs[call.Name] = agg
			}

			agg.Count++
			if rec.Timestamp.After(agg.LastSeen) {
				agg.LastSeen = rec.Timestamp
			}
			if agg.SampleArguments == "" && call.Arguments != "" {
				agg.SampleArguments = call.Arguments
			}

			if rec.Route == "" {
				continue
			}
			routeAgg, ok := agg.Routes[rec.Route]
			if !ok {
				routeAgg = &toolSuggestionRouteAgg{
					Route:          rec.Route,
					ModelCounts:    map[string]int{},
					ProviderCounts: map[string]int{},
				}
				agg.Routes[rec.Route] = routeAgg
			}
			routeAgg.Count++
			if rec.Timestamp.After(routeAgg.LastSeen) {
				routeAgg.LastSeen = rec.Timestamp
			}
			if rec.Model != "" {
				routeAgg.ModelCounts[rec.Model]++
			}
			if rec.Provider != "" {
				routeAgg.ProviderCounts[rec.Provider]++
			}
		}
	}

	suggestions := make([]toolHookSuggestion, 0, len(aggs))
	for _, agg := range aggs {
		routes := make([]toolHookSuggestionRoute, 0, len(agg.Routes))
		for _, routeAgg := range agg.Routes {
			primaryModel, models := topKeys(routeAgg.ModelCounts)
			_, providers := topKeys(routeAgg.ProviderCounts)
			routes = append(routes, toolHookSuggestionRoute{
				Route:        routeAgg.Route,
				Count:        routeAgg.Count,
				LastSeen:     routeAgg.LastSeen,
				PrimaryModel: primaryModel,
				Models:       models,
				Providers:    providers,
			})
		}
		slices.SortFunc(routes, func(a, b toolHookSuggestionRoute) int {
			if a.Count != b.Count {
				return b.Count - a.Count
			}
			if !a.LastSeen.Equal(b.LastSeen) {
				if a.LastSeen.After(b.LastSeen) {
					return -1
				}
				return 1
			}
			return strings.Compare(a.Route, b.Route)
		})

		suggestion := toolHookSuggestion{
			Match:           agg.Match,
			ToolName:        agg.ToolName,
			BaseToolName:    agg.BaseToolName,
			MCPName:         agg.MCPName,
			Count:           agg.Count,
			LastSeen:        agg.LastSeen,
			SampleArguments: agg.SampleArguments,
			Routes:          routes,
		}
		if len(routes) > 0 {
			suggestion.PrimaryRoute = routes[0].Route
			suggestion.PrimaryModel = routes[0].PrimaryModel
		}
		suggestions = append(suggestions, suggestion)
	}

	slices.SortFunc(suggestions, func(a, b toolHookSuggestion) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		if !a.LastSeen.Equal(b.LastSeen) {
			if a.LastSeen.After(b.LastSeen) {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Match, b.Match)
	})

	return toolHookSuggestionResponse{
		RecentLogs:  len(records),
		Suggestions: suggestions,
	}
}

func extractToolCallObservations(rec reqlog.Record) []toolCallObservation {
	seen := map[string]struct{}{}
	observations := make([]toolCallObservation, 0)
	add := func(call toolCallObservation) {
		if call.Name == "" {
			return
		}
		key := call.Name + "\x00" + call.Arguments
		if call.ID != "" {
			key = "id\x00" + call.ID
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		observations = append(observations, call)
	}

	for _, step := range rec.Steps {
		for _, call := range step.ToolCalls {
			add(toolCallObservation{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			})
		}
	}
	for _, call := range extractToolCallsFromJSON(rec.Response) {
		add(call)
	}

	return observations
}

func extractToolCallsFromJSON(raw json.RawMessage) []toolCallObservation {
	if len(raw) == 0 || !gjson.ValidBytes(raw) {
		return nil
	}

	result := gjson.ParseBytes(raw)
	calls := make([]toolCallObservation, 0)

	for _, choice := range result.Get("choices").Array() {
		for _, call := range choice.Get("message.tool_calls").Array() {
			calls = append(calls, toolCallObservation{
				ID:        call.Get("id").String(),
				Name:      call.Get("function.name").String(),
				Arguments: call.Get("function.arguments").String(),
			})
		}
		calls = append(calls, extractAnthropicToolUses(choice.Get("message.content"))...)
	}

	for _, item := range result.Get("output").Array() {
		if item.Get("type").String() != "function_call" {
			continue
		}
		calls = append(calls, toolCallObservation{
			ID:        item.Get("call_id").String(),
			Name:      item.Get("name").String(),
			Arguments: item.Get("arguments").String(),
		})
	}

	for _, item := range result.Get("content").Array() {
		if item.Get("type").String() != "tool_use" {
			continue
		}
		calls = append(calls, toolCallObservation{
			ID:        item.Get("id").String(),
			Name:      item.Get("name").String(),
			Arguments: item.Get("input").Raw,
		})
	}
	calls = append(calls, extractAnthropicToolUses(result.Get("message.content"))...)

	return calls
}

func extractAnthropicToolUses(content gjson.Result) []toolCallObservation {
	if !content.Exists() || !content.IsArray() {
		return nil
	}

	calls := make([]toolCallObservation, 0)
	for _, item := range content.Array() {
		if item.Get("type").String() != "tool_use" {
			continue
		}
		calls = append(calls, toolCallObservation{
			ID:        item.Get("id").String(),
			Name:      item.Get("name").String(),
			Arguments: item.Get("input").Raw,
		})
	}
	return calls
}

func splitToolName(fullName string) (string, string) {
	parts := strings.SplitN(fullName, "__", 2)
	if len(parts) != 2 {
		return "", fullName
	}
	return parts[0], parts[1]
}

func topKeys(counts map[string]int) (string, []string) {
	type item struct {
		Name  string
		Count int
	}

	items := make([]item, 0, len(counts))
	for name, count := range counts {
		if name == "" {
			continue
		}
		items = append(items, item{Name: name, Count: count})
	}
	slices.SortFunc(items, func(a, b item) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Name, b.Name)
	})

	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Name)
	}
	primary := ""
	if len(keys) > 0 {
		primary = keys[0]
	}
	return primary, keys
}
