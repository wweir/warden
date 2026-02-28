package gateway

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/wweir/warden/config"
)

func TestSelector_Select(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"primary": {
				Name:     "primary",
				URL:      "http://primary.example.com",
				Protocol: "openai",
			},
			"secondary": {
				Name:     "secondary",
				URL:      "http://secondary.example.com",
				Protocol: "openai",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Prefix:    "/test",
				Providers: []string{"primary", "secondary"}, // primary first = highest precedence
			},
		},
	}
	route := cfg.Route["/test"]

	s := NewSelector(cfg)

	// no model specified -> first in Providers wins
	bu, err := s.Select(cfg, route, "")
	if err != nil {
		t.Fatalf("Select() = _, %v", err)
	}
	if bu.Name != "primary" {
		t.Errorf("Select(): want primary, got %s", bu.Name)
	}
}

func TestSelector_RecordOutcome(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"test": {
				Name:     "test",
				URL:      "http://test.example.com",
				Protocol: "openai",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Prefix:    "/test",
				Providers: []string{"test"},
			},
		},
	}

	s := NewSelector(cfg)

	// initial state should be zero
	states := map[string]*providerState{}
	s.mu.Lock()
	for name, st := range s.states {
		states[name] = &providerState{
			consecutiveFailures: st.consecutiveFailures,
			suppressUntil:       st.suppressUntil,
		}
	}
	s.mu.Unlock()

	wantSt := &providerState{}
	if got, want := states["test"], wantSt; got.consecutiveFailures != want.consecutiveFailures {
		t.Errorf("Initial: want %v, got %v", want.consecutiveFailures, got.consecutiveFailures)
	}
	if !states["test"].suppressUntil.IsZero() {
		t.Errorf("Initial: want suppressUntil.IsZero(), got %v", states["test"].suppressUntil)
	}

	// record 1 success
	s.RecordOutcome("test", nil, 100*time.Millisecond)

	// state should still be zero
	s.mu.Lock()
	gotSt := s.states["test"]
	s.mu.Unlock()

	if gotSt.consecutiveFailures != 0 {
		t.Errorf("After success: want 0, got %d", gotSt.consecutiveFailures)
	}
	if !gotSt.suppressUntil.IsZero() {
		t.Errorf("After success: want suppressUntil.IsZero(), got %v", gotSt.suppressUntil)
	}
}

func TestSelector_Suppress(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"test": {
				Name:     "test",
				URL:      "http://test.example.com",
				Protocol: "openai",
			},
		},
	}

	s := NewSelector(cfg)

	// record failure (429 Too Many Requests)
	s.RecordOutcome("test", &UpstreamError{Code: 429}, 100*time.Millisecond)

	// check suppression duration (30s)
	s.mu.Lock()
	gotSt := s.states["test"]
	duration := time.Until(gotSt.suppressUntil)
	s.mu.Unlock()

	if gotSt.consecutiveFailures != 1 {
		t.Errorf("After 1 failure: want 1, got %d", gotSt.consecutiveFailures)
	}
	if duration < 25*time.Second || duration > 35*time.Second {
		t.Errorf("After 1 failure: want ~30s, got %v", duration)
	}
}

func TestSelector_MaxFailures(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"test": {
				Name:     "test",
				URL:      "http://test.example.com",
				Protocol: "openai",
			},
		},
	}

	s := NewSelector(cfg)

	for i := 0; i < 10; i++ {
		s.RecordOutcome("test", &UpstreamError{Code: 500}, 100*time.Millisecond)
	}

	// should cap at maxConsecutiveFailures = 5
	s.mu.Lock()
	gotSt := s.states["test"]
	s.mu.Unlock()

	if gotSt.consecutiveFailures != 5 {
		t.Errorf("After 10 failures: want 5, got %d", gotSt.consecutiveFailures)
	}
}

func TestSelector_SuppressThenSuccess(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"test": {
				Name:     "test",
				URL:      "http://test.example.com",
				Protocol: "openai",
			},
		},
	}

	s := NewSelector(cfg)

	// suppress
	s.RecordOutcome("test", &UpstreamError{Code: 500}, 100*time.Millisecond)

	// then success
	s.RecordOutcome("test", nil, 50*time.Millisecond)

	s.mu.Lock()
	gotSt := s.states["test"]
	s.mu.Unlock()

	if gotSt.consecutiveFailures != 0 {
		t.Errorf("After success: want 0, got %d", gotSt.consecutiveFailures)
	}
	if !gotSt.suppressUntil.IsZero() {
		t.Errorf("After success: want suppressUntil.IsZero(), got %v", gotSt.suppressUntil)
	}
}

func TestSelector_AllSuppressed(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"a": {
				Name:     "a",
				URL:      "http://a.example.com",
				Protocol: "openai",
			},
			"b": {
				Name:     "b",
				URL:      "http://b.example.com",
				Protocol: "openai",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Prefix:    "/test",
				Providers: []string{"a", "b"},
			},
		},
	}
	route := cfg.Route["/test"]

	s := NewSelector(cfg)

	// suppress both
	s.RecordOutcome("a", &UpstreamError{Code: 500}, 100*time.Millisecond)
	s.RecordOutcome("b", &UpstreamError{Code: 500}, 100*time.Millisecond)

	// manually set suppressUntil times to ensure one expires earlier
	s.mu.Lock()
	s.states["a"].suppressUntil = time.Now().Add(30 * time.Second)
	s.states["b"].suppressUntil = time.Now().Add(60 * time.Second)
	s.mu.Unlock()

	// select should pick a (earlier suppressUntil)
	bu, err := s.Select(cfg, route, "")
	if err != nil {
		t.Fatalf("Select() = _, %v", err)
	}
	if bu.Name != "a" {
		t.Errorf("All suppressed: want a, got %s", bu.Name)
	}
}

func TestSelector_ModelFilter(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				Name:     "openai",
				URL:      "http://openai.example.com",
				Protocol: "openai",
			},
			"anthropic": {
				Name:     "anthropic",
				URL:      "http://anthropic.example.com",
				Protocol: "anthropic",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Prefix:    "/test",
				Providers: []string{"openai", "anthropic"}, // openai first
			},
		},
	}
	route := cfg.Route["/test"]

	s := NewSelector(cfg)

	// set available models
	s.mu.Lock()
	s.states["openai"].availableModels = map[string]bool{"gpt-4o": true, "gpt-4o-mini": true}
	s.states["anthropic"].availableModels = map[string]bool{"claude-3-opus": true, "claude-3-sonnet": true}
	s.mu.Unlock()

	// request claude-3-opus -> should skip openai (doesn't have it), select anthropic
	bu, err := s.Select(cfg, route, "claude-3-opus")
	if err != nil {
		t.Fatalf("Select() = _, %v", err)
	}
	if bu.Name != "anthropic" {
		t.Errorf("Select(claude-3-opus): want anthropic, got %s", bu.Name)
	}

	// request gpt-4o -> should select openai (has it, first in list)
	bu, err = s.Select(cfg, route, "gpt-4o")
	if err != nil {
		t.Fatalf("Select() = _, %v", err)
	}
	if bu.Name != "openai" {
		t.Errorf("Select(gpt-4o): want openai, got %s", bu.Name)
	}
}

func TestSelector_ModelFilterNilDegradation(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"known": {
				Name:     "known",
				URL:      "http://known.example.com",
				Protocol: "openai",
			},
			"unknown": {
				Name:     "unknown",
				URL:      "http://unknown.example.com",
				Protocol: "openai",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Prefix:    "/test",
				Providers: []string{"unknown", "known"}, // unknown first = higher precedence
			},
		},
	}
	route := cfg.Route["/test"]

	s := NewSelector(cfg)

	// known has models set, unknown has nil (fetch failed)
	s.mu.Lock()
	s.states["known"].availableModels = map[string]bool{"gpt-4o": true}
	// s.states["unknown"].availableModels remains nil
	s.mu.Unlock()

	// request gpt-4o -> unknown (nil, not filtered) is first in list, should be selected
	bu, err := s.Select(cfg, route, "gpt-4o")
	if err != nil {
		t.Fatalf("Select() = _, %v", err)
	}
	if bu.Name != "unknown" {
		t.Errorf("Select(gpt-4o) with nil degradation: want unknown, got %s", bu.Name)
	}
}

func TestSelector_Models(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				Name:     "openai",
				URL:      "http://openai.example.com",
				Protocol: "openai",
			},
			"anthropic": {
				Name:     "anthropic",
				URL:      "http://anthropic.example.com",
				Protocol: "anthropic",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Prefix:    "/test",
				Providers: []string{"openai", "anthropic"},
			},
		},
	}
	route := cfg.Route["/test"]

	s := NewSelector(cfg)

	// set rawModels with overlapping model IDs
	s.mu.Lock()
	s.states["openai"].rawModels = []json.RawMessage{
		json.RawMessage(`{"id":"gpt-4o","object":"model","created":1000}`),
		json.RawMessage(`{"id":"gpt-4o-mini","object":"model","created":1001}`),
	}
	s.states["anthropic"].rawModels = []json.RawMessage{
		json.RawMessage(`{"id":"claude-3-opus","object":"model","created":2000}`),
		json.RawMessage(`{"id":"gpt-4o","object":"model","created":3000}`), // duplicate
	}
	s.mu.Unlock()

	models := s.Models(cfg, route)

	// should have 3 unique models: gpt-4o, gpt-4o-mini, claude-3-opus
	if len(models) != 3 {
		t.Fatalf("Models(): want 3, got %d", len(models))
	}

	// verify all expected IDs and owned_by
	type modelEntry struct {
		ID      string `json:"id"`
		OwnedBy string `json:"owned_by"`
	}
	entries := make(map[string]modelEntry)
	for _, raw := range models {
		var e modelEntry
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatalf("Unmarshal model: %v", err)
		}
		entries[e.ID] = e
	}

	for _, want := range []string{"gpt-4o", "gpt-4o-mini", "claude-3-opus"} {
		if _, ok := entries[want]; !ok {
			t.Errorf("Models(): missing model %s", want)
		}
	}

	// gpt-4o should be owned by openai (first seen)
	if entries["gpt-4o"].OwnedBy != "openai" {
		t.Errorf("gpt-4o owned_by: want openai, got %s", entries["gpt-4o"].OwnedBy)
	}
	if entries["claude-3-opus"].OwnedBy != "anthropic" {
		t.Errorf("claude-3-opus owned_by: want anthropic, got %s", entries["claude-3-opus"].OwnedBy)
	}
}

func TestSelector_ModelsEmpty(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"test": {
				Name:     "test",
				URL:      "http://test.example.com",
				Protocol: "openai",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Prefix:    "/test",
				Providers: []string{"test"},
			},
		},
	}
	route := cfg.Route["/test"]

	s := NewSelector(cfg)

	// no rawModels set (fetch failed)
	models := s.Models(cfg, route)
	if len(models) != 0 {
		t.Errorf("Models(): want 0, got %d", len(models))
	}
}

func TestSelector_ModelAlias_Select(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"nvidia": {
				Name:     "nvidia",
				URL:      "http://nvidia.example.com",
				Protocol: "openai",
				ModelAliases: map[string]string{
					"kimi-k2": "moonshotai/kimi-k2",
				},
			},
			"openai": {
				Name:     "openai",
				URL:      "http://openai.example.com",
				Protocol: "openai",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Prefix:    "/test",
				Providers: []string{"openai", "nvidia"},
			},
		},
	}
	route := cfg.Route["/test"]

	s := NewSelector(cfg)

	// set available models
	s.mu.Lock()
	s.states["openai"].availableModels = map[string]bool{"gpt-4o": true}
	s.states["nvidia"].availableModels = map[string]bool{"moonshotai/kimi-k2": true}
	s.mu.Unlock()

	// request alias "kimi-k2" -> nvidia has the alias, should be selected
	bu, err := s.Select(cfg, route, "kimi-k2")
	if err != nil {
		t.Fatalf("Select(kimi-k2) = _, %v", err)
	}
	if bu.Name != "nvidia" {
		t.Errorf("Select(kimi-k2): want nvidia, got %s", bu.Name)
	}
}

func TestSelector_ModelAlias_Models(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"nvidia": {
				Name:     "nvidia",
				URL:      "http://nvidia.example.com",
				Protocol: "openai",
				ModelAliases: map[string]string{
					"kimi-k2": "moonshotai/kimi-k2",
				},
			},
		},
		Route: map[string]*config.RouteConfig{
			"/test": {
				Prefix:    "/test",
				Providers: []string{"nvidia"},
			},
		},
	}
	route := cfg.Route["/test"]

	s := NewSelector(cfg)

	s.mu.Lock()
	s.states["nvidia"].rawModels = []json.RawMessage{
		json.RawMessage(`{"id":"moonshotai/kimi-k2","object":"model"}`),
	}
	s.mu.Unlock()

	models := s.Models(cfg, route)

	// should have 2 models: real + alias
	if len(models) != 2 {
		t.Fatalf("Models(): want 2, got %d", len(models))
	}

	type modelEntry struct {
		ID      string `json:"id"`
		OwnedBy string `json:"owned_by"`
		Aliased string `json:"aliased"`
	}
	entries := make(map[string]modelEntry)
	for _, raw := range models {
		var e modelEntry
		json.Unmarshal(raw, &e)
		entries[e.ID] = e
	}

	// real model
	if _, ok := entries["moonshotai/kimi-k2"]; !ok {
		t.Error("Models(): missing real model moonshotai/kimi-k2")
	}
	if entries["moonshotai/kimi-k2"].OwnedBy != "nvidia" {
		t.Errorf("moonshotai/kimi-k2 owned_by: want nvidia, got %s", entries["moonshotai/kimi-k2"].OwnedBy)
	}

	// alias model
	if _, ok := entries["kimi-k2"]; !ok {
		t.Error("Models(): missing alias model kimi-k2")
	}
	if entries["kimi-k2"].OwnedBy != "nvidia" {
		t.Errorf("kimi-k2 owned_by: want nvidia, got %s", entries["kimi-k2"].OwnedBy)
	}
	if entries["kimi-k2"].Aliased != "moonshotai/kimi-k2" {
		t.Errorf("kimi-k2 aliased: want moonshotai/kimi-k2, got %s", entries["kimi-k2"].Aliased)
	}
}


func TestProviderState_SlidingWindow(t *testing.T) {
	st := &providerState{}

	// Record 5 successes
	for i := 0; i < 5; i++ {
		st.recordOutcome(true, 100, "")
	}

	total, success, failure, avg := st.windowStats()
	if total != 5 {
		t.Errorf("windowStats total = %d, want 5", total)
	}
	if success != 5 {
		t.Errorf("windowStats success = %d, want 5", success)
	}
	if failure != 0 {
		t.Errorf("windowStats failure = %d, want 0", failure)
	}
	if avg != 100.0 {
		t.Errorf("windowStats avg = %f, want 100.0", avg)
	}

	// Record 3 failures
	for i := 0; i < 3; i++ {
		st.recordOutcome(false, 200, "pre_stream")
	}

	total, success, failure, avg = st.windowStats()
	if total != 8 {
		t.Errorf("windowStats total = %d, want 8", total)
	}
	if success != 5 {
		t.Errorf("windowStats success = %d, want 5", success)
	}
	if failure != 3 {
		t.Errorf("windowStats failure = %d, want 3", failure)
	}
	// Average: (5*100 + 3*200) / 8 = 137.5
	if avg != 137.5 {
		t.Errorf("windowStats avg = %f, want 137.5", avg)
	}
}

func TestProviderState_SlidingWindow_RingBuffer(t *testing.T) {
	st := &providerState{}

	// Record more than outcomeWindowSize (1000) outcomes
	for i := 0; i < 1005; i++ {
		st.recordOutcome(i%2 == 0, int64(i), "") // alternating success/failure
	}

	total, success, failure, _ := st.windowStats()
	if total != 1000 {
		t.Errorf("windowStats total = %d, want 1000 (ring buffer limit)", total)
	}
	if success != 500 {
		t.Errorf("windowStats success = %d, want 500", success)
	}
	if failure != 500 {
		t.Errorf("windowStats failure = %d, want 500", failure)
	}
}

func TestSelector_SuppressReasons(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"test": {
				Name:     "test",
				URL:      "http://test.example.com",
				Protocol: "openai",
			},
		},
	}

	s := NewSelector(cfg)

	// record 3 failures with different errors
	s.RecordOutcome("test", &UpstreamError{Code: 500, Body: "internal server error"}, 100*time.Millisecond)
	s.RecordOutcome("test", &UpstreamError{Code: 429, Body: "rate limited"}, 100*time.Millisecond)
	s.RecordOutcome("test", &UpstreamError{Code: 502, Body: "bad gateway"}, 100*time.Millisecond)

	statuses := s.ProviderStatuses()
	if len(statuses) != 1 {
		t.Fatalf("ProviderStatuses: want 1, got %d", len(statuses))
	}

	reasons := statuses[0].SuppressReasons
	if len(reasons) != 3 {
		t.Fatalf("SuppressReasons: want 3, got %d", len(reasons))
	}
	if reasons[0].Reason != "HTTP 500: internal server error" {
		t.Errorf("SuppressReasons[0]: want 'HTTP 500: internal server error', got %q", reasons[0].Reason)
	}
	if reasons[1].Reason != "HTTP 429: rate limited" {
		t.Errorf("SuppressReasons[1]: want 'HTTP 429: rate limited', got %q", reasons[1].Reason)
	}

	// success clears consecutive failures but reasons persist
	s.RecordOutcome("test", nil, 50*time.Millisecond)
	statuses = s.ProviderStatuses()
	if statuses[0].ConsecutiveFailures != 0 {
		t.Errorf("After success: want 0 failures, got %d", statuses[0].ConsecutiveFailures)
	}
	if len(statuses[0].SuppressReasons) != 3 {
		t.Errorf("After success: reasons should persist, want 3, got %d", len(statuses[0].SuppressReasons))
	}
}

func TestSelector_SuppressReasons_MaxLimit(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"test": {
				Name:     "test",
				URL:      "http://test.example.com",
				Protocol: "openai",
			},
		},
	}

	s := NewSelector(cfg)

	// record 25 failures (exceeds maxSuppressReasons=20)
	for i := 0; i < 25; i++ {
		s.RecordOutcome("test", &UpstreamError{Code: 500, Body: "error"}, 100*time.Millisecond)
	}

	statuses := s.ProviderStatuses()
	reasons := statuses[0].SuppressReasons
	if len(reasons) != 20 {
		t.Errorf("SuppressReasons: want 20 (max), got %d", len(reasons))
	}
}
