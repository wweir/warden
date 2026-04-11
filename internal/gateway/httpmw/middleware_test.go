package httpmw

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wweir/warden/config"
)

func TestRecoveryReturnsJSONError(t *testing.T) {
	handler := (&Recovery{}).Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"error":"Internal server error"`) {
		t.Fatalf("body = %q, want JSON error payload", body)
	}
}

func TestAPIKeyAuthBypassesWhenNoKeysConfigured(t *testing.T) {
	handler := (&APIKeyAuth{Cfg: &config.ConfigStruct{}}).Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestAPIKeyAuthRejectsMissingOrInvalidKey(t *testing.T) {
	cfg := &config.ConfigStruct{
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				APIKeys: map[string]config.SecretString{
					"client": "valid-token",
				},
			},
		},
	}
	handler := (&APIKeyAuth{Cfg: cfg}).Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	}))

	for _, tc := range []struct {
		name   string
		header http.Header
	}{
		{name: "missing"},
		{name: "invalid bearer", header: http.Header{"Authorization": []string{"Bearer nope"}}},
		{name: "invalid x-api-key", header: http.Header{"X-Api-Key": []string{"nope"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", nil)
			req.Header = tc.header.Clone()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}

			var body map[string]map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if body["error"]["message"] != "invalid api key" {
				t.Fatalf("error message = %q", body["error"]["message"])
			}
		})
	}
}

func TestAPIKeyAuthBypassesRouteWithoutKeys(t *testing.T) {
	cfg := &config.ConfigStruct{
		Route: map[string]*config.RouteConfig{
			"/public": {Protocol: config.RouteProtocolChat},
		},
	}
	handler := (&APIKeyAuth{Cfg: cfg}).Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/public/chat/completions", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestAPIKeyAuthUsesLongestMatchedRoute(t *testing.T) {
	cfg := &config.ConfigStruct{
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				APIKeys: map[string]config.SecretString{
					"outer": "outer-token",
				},
			},
			"/openai/internal": {
				Protocol: config.RouteProtocolChat,
				APIKeys: map[string]config.SecretString{
					"inner": "inner-token",
				},
			},
		},
	}
	handler := (&APIKeyAuth{Cfg: cfg}).Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/openai/internal/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer inner-token")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}
