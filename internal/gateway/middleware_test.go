package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecoveryMiddlewareReturnsJSONError(t *testing.T) {
	handler := (&RecoveryMiddleware{}).Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
