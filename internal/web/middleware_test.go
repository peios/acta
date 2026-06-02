package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/peios/acta/internal/httpx"
)

func TestSecureHeaders(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	// prod (hsts=true): HSTS present alongside the always-on headers.
	rec := httptest.NewRecorder()
	secureHeaders(true)(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Error("want HSTS header when hsts=true")
	}
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}

	// dev (hsts=false): no HSTS over plain http.
	rec = httptest.NewRecorder()
	secureHeaders(false)(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("want no HSTS when hsts=false, got %q", got)
	}
}

func TestRecoverPanic(t *testing.T) {
	boom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(httpx.WithRequestMeta(req.Context(), "req1", "203.0.113.1"))
	rec := httptest.NewRecorder()

	// The panic must be contained and turned into a 500, not re-thrown.
	recoverPanic(boom).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
