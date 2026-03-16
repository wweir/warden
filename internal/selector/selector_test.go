package selector

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/wweir/warden/config"
)

func mustValidateConfig(t *testing.T, cfg *config.ConfigStruct) *config.RouteConfig {
	t.Helper()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	return cfg.Route["/test"]
}

func TestSelector_SelectExactModel(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"primary":   {URL: "http://primary.example.com", Protocol: "openai"},
			"secondary": {URL: "http://secondary.example.com", Protocol: "openai"},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Protocol: config.RouteProtocolChat,
				Models: map[string]*config.RouteModelConfig{
					"gpt-4o": {
						Upstreams: []*config.RouteUpstreamConfig{
							{Provider: "primary", Model: "gpt-4o"},
							{Provider: "secondary", Model: "gpt-4.1"},
						},
					},
				},
			},
		},
	}
	route := mustValidateConfig(t, cfg)
	matched := route.MatchModel("gpt-4o")
	if matched == nil {
		t.Fatal("MatchModel(gpt-4o) = nil")
	}

	s := NewSelector(cfg)
	target, prov, err := s.Select(cfg, route, config.RouteProtocolChat, matched, "gpt-4o")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if prov.Name != "primary" {
		t.Fatalf("Select() provider = %s, want primary", prov.Name)
	}
	if target.UpstreamModel != "gpt-4o" {
		t.Fatalf("Select() upstream_model = %s, want gpt-4o", target.UpstreamModel)
	}
}

func TestSelector_SelectWildcardModel(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"first":  {URL: "http://first.example.com", Protocol: "openai"},
			"second": {URL: "http://second.example.com", Protocol: "openai"},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Protocol: config.RouteProtocolResponses,
				Models: map[string]*config.RouteModelConfig{
					"gpt-*": {
						Providers: []string{"first", "second"},
					},
				},
			},
		},
	}
	route := mustValidateConfig(t, cfg)
	matched := route.MatchModel("gpt-4.1")
	if matched == nil || !matched.Wildcard {
		t.Fatal("MatchModel(gpt-4.1) should match wildcard model")
	}

	s := NewSelector(cfg)
	s.mu.Lock()
	s.states["first"].availableModels = map[string]bool{"gpt-4o": true}
	s.states["second"].availableModels = map[string]bool{"gpt-4.1": true}
	s.mu.Unlock()

	target, prov, err := s.Select(cfg, route, config.RouteProtocolResponses, matched, "gpt-4.1")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if prov.Name != "second" {
		t.Fatalf("Select() provider = %s, want second", prov.Name)
	}
	if target.UpstreamModel != "gpt-4.1" {
		t.Fatalf("Select() upstream_model = %s, want gpt-4.1", target.UpstreamModel)
	}
}

func TestSelector_SelectSkipsManualSuppress(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"primary":   {URL: "http://primary.example.com", Protocol: "openai"},
			"secondary": {URL: "http://secondary.example.com", Protocol: "openai"},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Protocol: config.RouteProtocolChat,
				Models: map[string]*config.RouteModelConfig{
					"gpt-4o": {
						Upstreams: []*config.RouteUpstreamConfig{
							{Provider: "primary", Model: "gpt-4o"},
							{Provider: "secondary", Model: "gpt-4o"},
						},
					},
				},
			},
		},
	}
	route := mustValidateConfig(t, cfg)
	matched := route.MatchModel("gpt-4o")
	s := NewSelector(cfg)
	if ok := s.SetManualSuppress("primary", true); !ok {
		t.Fatal("SetManualSuppress(primary, true) = false")
	}

	target, prov, err := s.Select(cfg, route, config.RouteProtocolChat, matched, "gpt-4o")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if prov.Name != "secondary" || target.ProviderName != "secondary" {
		t.Fatalf("Select() should choose secondary, got provider=%s target=%s", prov.Name, target.ProviderName)
	}
}

func TestSelector_Models(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"primary": {URL: "http://primary.example.com", Protocol: "openai"},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Protocol: config.RouteProtocolChat,
				Models: map[string]*config.RouteModelConfig{
					"gpt-4o": {
						Upstreams: []*config.RouteUpstreamConfig{
							{Provider: "primary", Model: "gpt-4.1"},
						},
					},
					"gpt-*": {
						Providers: []string{"primary"},
					},
				},
			},
		},
	}
	route := mustValidateConfig(t, cfg)
	s := NewSelector(cfg)

	s.mu.Lock()
	s.states["primary"].rawModels = []json.RawMessage{
		json.RawMessage(`{"id":"gpt-4.1","object":"model"}`),
		json.RawMessage(`{"id":"gpt-4.1-mini","object":"model"}`),
	}
	s.mu.Unlock()

	models := s.Models(cfg, route)
	if len(models) != 3 {
		t.Fatalf("Models() len = %d, want 3", len(models))
	}

	type modelEntry struct {
		ID      string `json:"id"`
		OwnedBy string `json:"owned_by"`
		Aliased string `json:"aliased"`
	}
	entries := map[string]modelEntry{}
	for _, raw := range models {
		var entry modelEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		entries[entry.ID] = entry
	}

	if entries["gpt-4o"].Aliased != "gpt-4.1" {
		t.Fatalf("gpt-4o aliased = %q, want gpt-4.1", entries["gpt-4o"].Aliased)
	}
	if entries["gpt-4.1"].OwnedBy != "/test" {
		t.Fatalf("gpt-4.1 owned_by = %q, want /test", entries["gpt-4.1"].OwnedBy)
	}
	if entries["gpt-4.1-mini"].OwnedBy != "/test" {
		t.Fatalf("gpt-4.1-mini owned_by = %q, want /test", entries["gpt-4.1-mini"].OwnedBy)
	}
}

func TestSelector_RecordOutcome(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"test": {Name: "test", URL: "http://test.example.com", Protocol: "openai"},
		},
	}

	s := NewSelector(cfg)
	s.RecordOutcome("test", &UpstreamError{Code: 500}, 100*time.Millisecond)
	s.RecordOutcome("test", nil, 50*time.Millisecond)

	status := s.ProviderDetail("test")
	if status == nil {
		t.Fatal("ProviderDetail(test) = nil")
	}
	if status.ConsecutiveFailures != 0 {
		t.Fatalf("ConsecutiveFailures = %d, want 0", status.ConsecutiveFailures)
	}
	if len(status.SuppressReasons) != 1 {
		t.Fatalf("SuppressReasons len = %d, want 1", len(status.SuppressReasons))
	}
}

func TestSelector_RecordOutcomeWithSource_ErrorCounters(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"test": {Name: "test", URL: "http://test.example.com", Protocol: "openai"},
		},
	}

	s := NewSelector(cfg)
	s.RecordOutcomeWithSource("test", &UpstreamError{Code: 500}, 100*time.Millisecond, "pre_stream")
	s.RecordOutcomeWithSource("test", &UpstreamError{Code: 500}, 100*time.Millisecond, "in_stream")

	status := s.ProviderDetail("test")
	if status == nil {
		t.Fatal("ProviderDetail(test) = nil")
	}
	if status.PreStreamErrors != 1 {
		t.Fatalf("PreStreamErrors = %d, want 1", status.PreStreamErrors)
	}
	if status.InStreamErrors != 1 {
		t.Fatalf("InStreamErrors = %d, want 1", status.InStreamErrors)
	}
}

func TestSelector_RecordFailover(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"test": {Name: "test", URL: "http://test.example.com", Protocol: "openai"},
		},
	}

	s := NewSelector(cfg)
	s.RecordFailover("test")
	s.RecordFailover("test")

	status := s.ProviderDetail("test")
	if status == nil {
		t.Fatal("ProviderDetail(test) = nil")
	}
	if status.FailoverCount != 2 {
		t.Fatalf("FailoverCount = %d, want 2", status.FailoverCount)
	}
}
