package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthLiveness(t *testing.T) {
	rec := httptest.NewRecorder()
	HealthHandler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d, want 200", rec.Code)
	}
}

func TestHealthReadiness(t *testing.T) {
	rec := httptest.NewRecorder()
	HealthHandler(func(context.Context) error { return nil }).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz (dependencies up) = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	HealthHandler(func(context.Context) error { return errors.New("db down") }).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz (dependency down) = %d, want 503", rec.Code)
	}
}
