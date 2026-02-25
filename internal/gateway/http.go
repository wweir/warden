package gateway

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/pkg/anthropic"
)

// setAuthHeaders injects authentication headers based on protocol type.
// Custom headers from provider config are applied last and override defaults.
func setAuthHeaders(h http.Header, provCfg *config.ProviderConfig) {
	h.Set("Content-Type", "application/json")
	apiKey := provCfg.GetAPIKey()
	if apiKey != "" {
		switch provCfg.Protocol {
		case "anthropic":
			h.Set("x-api-key", apiKey)
			h.Set("anthropic-version", "2023-06-01")
			// Many Anthropic proxies expose OpenAI-compatible /v1/models that
			// requires Bearer auth, so set both headers.
			h.Set("Authorization", "Bearer "+apiKey)
		default:
			h.Set("Authorization", "Bearer "+apiKey)
		}
	}
	for k, v := range provCfg.Headers {
		h.Set(k, v)
	}
}

// sendRequest sends a raw request body to the upstream endpoint and returns the raw response body.
func sendRequest(provCfg *config.ProviderConfig, endpoint string, body []byte) ([]byte, error) {
	httpReq, err := http.NewRequest(http.MethodPost, provCfg.URL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	setAuthHeaders(httpReq.Header, provCfg)

	client := provCfg.HTTPClient(provCfg.TimeoutDuration)
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	reader := resp.Body
	// Manually decompress gzip when the transport did not handle it
	// (e.g. upstream returns gzip without proper Content-Encoding header).
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gr, gzErr := gzip.NewReader(resp.Body)
		if gzErr != nil {
			return nil, fmt.Errorf("create gzip reader: %w", gzErr)
		}
		defer gr.Close()
		reader = gr
	}

	respBody, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	// Fallback: if body still looks like gzip (magic bytes 0x1f 0x8b),
	// decompress it. Some proxies omit Content-Encoding header.
	if len(respBody) >= 2 && respBody[0] == 0x1f && respBody[1] == 0x8b {
		gr, gzErr := gzip.NewReader(bytes.NewReader(respBody))
		if gzErr == nil {
			decompressed, readErr := io.ReadAll(gr)
			gr.Close()
			if readErr == nil {
				respBody = decompressed
			}
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &UpstreamError{Code: resp.StatusCode, Body: string(respBody)}
	}

	// detect HTML body on 200 (misconfigured proxy returning HTML instead of JSON)
	if trimmed := strings.TrimSpace(string(respBody)); len(trimmed) > 0 && (trimmed[0] == '<' || strings.HasPrefix(trimmed, "<!DOCTYPE")) {
		return nil, &UpstreamError{Code: resp.StatusCode, Body: trimmed}
	}

	return respBody, nil
}

// pipeRawStream sends a raw request body upstream and returns the response bytes
// after writing them to the client. For Anthropic protocol, the SSE response is
// converted to OpenAI Chat Completions SSE format before writing.
func pipeRawStream(w http.ResponseWriter, provCfg *config.ProviderConfig, endpoint string, body []byte) ([]byte, error) {
	rawBody, err := sendRequest(provCfg, endpoint, body)
	// Always write the response body to the client if it exists
	if rawBody != nil {
		clientBody := rawBody
		if provCfg.Protocol == "anthropic" {
			clientBody = anthropic.ConvertStreamToOpenAI(rawBody)
		}
		w.Write(clientBody)
		w.(http.Flusher).Flush()
	}
	// Always return the raw body even if there's an error
	return rawBody, err
}

// modelsResponse is the common format for GET /models across all protocols.
type modelsResponse struct {
	Data    []json.RawMessage `json:"data"`
	HasMore bool              `json:"has_more"`
	LastID  string            `json:"last_id"`
}

// fetchModels queries GET <base_url>/models to discover available model IDs.
// Handles Anthropic pagination (has_more + after_id).
// Returns both a model ID set (for Select filtering) and raw model objects
// (for aggregated /models endpoint). Returns nil on error (caller should
// treat nil as "unknown, don't filter").
func fetchModels(provCfg *config.ProviderConfig) (map[string]bool, []json.RawMessage, error) {
	client := provCfg.HTTPClient(30 * time.Second)
	models := make(map[string]bool)
	var rawModels []json.RawMessage
	afterID := ""

	for {
		url := provCfg.URL + protocolModelsEndpoint(provCfg.Protocol)
		if afterID != "" {
			url += "?after_id=" + afterID
		}

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("create models request: %w", err)
		}
		setAuthHeaders(req.Header, provCfg)

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
			msg := strings.TrimSpace(string(body))
			// discard non-JSON bodies (HTML error pages, etc.)
			if msg == "" || strings.HasPrefix(msg, "<") {
				msg = http.StatusText(resp.StatusCode)
			} else if len(msg) > 200 {
				msg = msg[:200] + "..."
			}
			return nil, nil, fmt.Errorf("fetch models: HTTP %d %s", resp.StatusCode, msg)
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

		if !result.HasMore {
			break
		}
		afterID = result.LastID
	}

	return models, rawModels, nil
}
