package selector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/pkg/protocol/anthropic"
)

const modelsEndpointPath = "/models"

// RefreshModels queries GET /models for all providers in parallel.
// When a provider has static models configured, they are set immediately as a
// baseline, and a remote fetch is still attempted to discover additional models.
func (s *Selector) RefreshModels(ctx context.Context, cfg *config.ConfigStruct) {
	var wg sync.WaitGroup
	for name, provCfg := range cfg.Provider {
		s.seedConfiguredModels(name, provCfg)

		wg.Add(1)
		go func(name string, provCfg *config.ProviderConfig) {
			defer wg.Done()

			fetched, fetchedRaw, err := FetchModels(ctx, provCfg)
			if err != nil {
				if len(provCfg.Models) == 0 {
					slog.Warn("Models discovery failed, model filter disabled for this provider; set 'models' in config to suppress",
						"provider", name, "error", err)
				} else {
					slog.Warn("Models discovery failed, using configured models only",
						"provider", name, "error", err)
				}
				return
			}

			s.mu.Lock()
			defer s.mu.Unlock()

			st, ok := s.states[name]
			if !ok {
				return
			}
			if len(provCfg.Models) > 0 {
				for id := range fetched {
					if st.availableModels[id] {
						continue
					}
					st.availableModels[id] = true
					st.rawModels = append(st.rawModels, buildModelEntryRaw(id, name))
				}
				slog.Info("Models merged from config and upstream", "provider", name, "count", len(st.availableModels))
				return
			}
			st.availableModels = fetched
			st.rawModels = fetchedRaw
			slog.Info("Models discovered from upstream", "provider", name, "count", len(fetched))
		}(name, provCfg)
	}
	wg.Wait()
}

func (s *Selector) seedConfiguredModels(name string, provCfg *config.ProviderConfig) {
	if len(provCfg.Models) == 0 {
		return
	}

	models := make(map[string]bool, len(provCfg.Models))
	rawModels := make([]json.RawMessage, 0, len(provCfg.Models))
	for _, id := range provCfg.Models {
		models[id] = true
		rawModels = append(rawModels, buildModelEntryRaw(id, name))
	}

	s.mu.Lock()
	if st, ok := s.states[name]; ok {
		st.availableModels = models
		st.rawModels = rawModels
	}
	s.mu.Unlock()
	slog.Info("Models loaded from config", "provider", name, "count", len(models))
}

// Models returns the public model list exposed by the route.
func (s *Selector) Models(route *config.RouteConfig) []json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]bool)
	result := make([]json.RawMessage, 0, len(route.PublicModels()))

	for _, modelName := range route.PublicModels() {
		compiled := route.MatchModel(modelName)
		if compiled == nil || seen[modelName] {
			continue
		}
		seen[modelName] = true

		entry := map[string]any{
			"id":       modelName,
			"object":   "model",
			"owned_by": route.Prefix,
		}
		if !compiled.Wildcard && len(compiled.Upstreams) > 0 && compiled.Upstreams[0].RenameModel {
			entry["aliased"] = compiled.Upstreams[0].UpstreamModel
		}
		result = append(result, mustMarshal(entry))
	}

	for _, wildcard := range route.CompiledWildcardModels() {
		for _, upstream := range wildcard.Upstreams {
			st := s.states[upstream.Provider]
			if st == nil {
				continue
			}
			for _, raw := range st.rawModels {
				entry, id := decodeModelEntry(raw)
				if id == "" || seen[id] {
					continue
				}
				matched, err := path.Match(wildcard.Pattern, id)
				if err != nil || !matched {
					continue
				}
				seen[id] = true
				entry["owned_by"] = mustMarshal(route.Prefix)
				out, _ := json.Marshal(entry)
				result = append(result, out)
			}
		}
	}

	return result
}

func decodeModelEntry(raw json.RawMessage) (map[string]json.RawMessage, string) {
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, ""
	}
	var id string
	if idRaw, ok := entry["id"]; ok {
		_ = json.Unmarshal(idRaw, &id)
	}
	return entry, id
}

func buildModelEntryRaw(id, owner string) json.RawMessage {
	return mustMarshal(map[string]string{
		"id":       id,
		"object":   "model",
		"owned_by": owner,
	})
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// SetAuthHeaders injects authentication headers based on protocol type.
func SetAuthHeaders(ctx context.Context, h http.Header, provCfg *config.ProviderConfig) {
	h.Set("Content-Type", "application/json")
	apiKey := provCfg.GetAPIKey(ctx)
	if apiKey != "" {
		switch provCfg.Protocol {
		case "anthropic":
			anthropic.SetAuthHeaders(h, apiKey)
		default:
			h.Set("Authorization", "Bearer "+apiKey)
		}
	}
	for k, v := range provCfg.Headers {
		h.Set(k, v)
	}
}

type modelsResponse struct {
	Data    []json.RawMessage `json:"data"`
	HasMore bool              `json:"has_more"`
	LastID  string            `json:"last_id"`
}

// FetchModels queries GET <base_url>/models to discover available model IDs.
func FetchModels(ctx context.Context, provCfg *config.ProviderConfig) (map[string]bool, []json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client := provCfg.HTTPClient(30 * time.Second)
	models := make(map[string]bool)
	var rawModels []json.RawMessage
	afterID := ""
	seenCursors := map[string]bool{}

	for {
		url := provCfg.URL + modelsEndpointPath
		if afterID != "" {
			url += "?after_id=" + afterID
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("create models request: %w", err)
		}
		SetAuthHeaders(ctx, req.Header, provCfg)

		resp, err := client.Do(req)
		if err != nil {
			return nil, nil, fmt.Errorf("fetch models: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("read models response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("fetch models: %s", formatModelsFetchHTTPError(resp.StatusCode, body))
		}

		ct := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") && len(body) > 0 && body[0] != '{' && body[0] != '[' {
			return nil, nil, fmt.Errorf("unexpected response Content-Type %q, not JSON", ct)
		}

		var result modelsResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, nil, fmt.Errorf("parse models response: %w", err)
		}
		for _, raw := range result.Data {
			var entry struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &entry); err == nil {
				models[entry.ID] = true
			}
			rawModels = append(rawModels, raw)
		}
		nextAfterID, err := nextModelsCursor(afterID, result, seenCursors)
		if err != nil {
			return nil, nil, err
		}
		if nextAfterID == "" {
			break
		}
		afterID = nextAfterID
	}

	return models, rawModels, nil
}

func nextModelsCursor(current string, result modelsResponse, seen map[string]bool) (string, error) {
	if !result.HasMore {
		return "", nil
	}

	next := strings.TrimSpace(result.LastID)
	if next == "" {
		return "", fmt.Errorf("fetch models: upstream pagination returned has_more=true with empty last_id")
	}
	if next == current {
		return "", fmt.Errorf("fetch models: upstream pagination did not advance cursor %q", next)
	}
	if seen[next] {
		return "", fmt.Errorf("fetch models: upstream pagination repeated cursor %q", next)
	}

	seen[next] = true
	return next, nil
}
