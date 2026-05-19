package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wweir/warden/config"
)

func TestHandleProviderProbeAccess(t *testing.T) {
	// Mock a server that responds like OpenAI /models
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("request path: %s", r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	h := &Handler{}
	req := httptest.NewRequest("POST", "/api/providers/probe-access", strings.NewReader(`{
		"url": "`+server.URL+`/v1",
		"api_key": "test-key",
		"headers": {},
		"proxy": ""
	}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleProviderProbeAccess(w, req, nil)

	t.Logf("status: %d", w.Code)
	t.Logf("body: %s", w.Body.String())

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleProviderProbeAccessUsesStoredSecretForRedactedAPIKey(t *testing.T) {
	const realAPIKey = "real-provider-key"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+realAPIKey {
			http.Error(w, `{"error":"bad key"}`, http.StatusInternalServerError)
			return
		}
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"glm-5"}]}`))
	}))
	defer server.Close()

	h := &Handler{
		cfg: &config.ConfigStruct{
			Provider: map[string]*config.ProviderConfig{
				"cloud": {
					APIKey: config.SecretString(realAPIKey),
				},
			},
		},
	}

	for _, baseURL := range []string{server.URL, server.URL + "/v1"} {
		req := httptest.NewRequest("POST", "/api/providers/probe-access", strings.NewReader(`{
			"name": "cloud",
			"url": "`+baseURL+`",
			"api_key": "`+RedactedPlaceholder+`",
			"headers": {},
			"proxy": ""
		}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleProviderProbeAccess(w, req, nil)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
		}
		var payload struct {
			Formats []AccessModeProbeResult `json:"formats"`
		}
		if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(payload.Formats) == 0 || !payload.Formats[0].Available {
			t.Fatalf("openai probe for %s unavailable: %+v", baseURL, payload.Formats)
		}
		if payload.Formats[0].Error != "" {
			t.Fatalf("openai probe for %s kept stale error: %+v", baseURL, payload.Formats[0])
		}
		if got, want := payload.Formats[0].ResolvedURL, server.URL+"/v1"; got != want {
			t.Fatalf("resolved_url = %q, want %q", got, want)
		}
	}
}

func TestHandleProviderProbeAccessDetectsAnthropic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-3"}]}`))
		case "/v1/messages":
			// Return 400 to indicate the endpoint exists but rejects the probe payload.
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	h := &Handler{}
	req := httptest.NewRequest("POST", "/api/providers/probe-access", strings.NewReader(`{
		"url": "`+server.URL+`/v1",
		"api_key": "test-key",
		"headers": {},
		"proxy": ""
	}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleProviderProbeAccess(w, req, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var payload struct {
		Formats []AccessModeProbeResult `json:"formats"`
	}
	if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var openAI, anthropic *AccessModeProbeResult
	for i := range payload.Formats {
		switch payload.Formats[i].Mode {
		case "openai":
			openAI = &payload.Formats[i]
		case "anthropic":
			anthropic = &payload.Formats[i]
		}
	}
	if openAI == nil || !openAI.Available {
		t.Fatalf("openai probe unavailable: %+v", payload.Formats)
	}
	if anthropic == nil || !anthropic.Available {
		t.Fatalf("anthropic probe should be available (400 means endpoint exists): %+v", payload.Formats)
	}
	if anthropic.ResolvedURL != server.URL+"/v1" {
		t.Fatalf("anthropic resolved_url = %q, want %q", anthropic.ResolvedURL, server.URL+"/v1")
	}
}

func TestHandleProviderProbeAccessDoesNotTreatOpenAIModelsAsAnthropic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"glm-5"}]}`))
	}))
	defer server.Close()

	h := &Handler{}
	req := httptest.NewRequest("POST", "/api/providers/probe-access", strings.NewReader(`{
		"url": "`+server.URL+`/v1",
		"api_key": "test-key",
		"headers": {},
		"proxy": ""
	}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleProviderProbeAccess(w, req, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var payload struct {
		Formats []AccessModeProbeResult `json:"formats"`
	}
	if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var openAI, anthropic *AccessModeProbeResult
	for i := range payload.Formats {
		switch payload.Formats[i].Mode {
		case "openai":
			openAI = &payload.Formats[i]
		case "anthropic":
			anthropic = &payload.Formats[i]
		}
	}
	if openAI == nil || !openAI.Available {
		t.Fatalf("openai probe unavailable: %+v", payload.Formats)
	}
	if anthropic == nil {
		t.Fatalf("anthropic probe result missing: %+v", payload.Formats)
	}
	if anthropic.Available {
		t.Fatalf("anthropic probe used OpenAI /models as proof: %+v", anthropic)
	}
}
