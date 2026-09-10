package integration

import (
	"acta2/internal/auth"
	"acta2/internal/config"
	"acta2/internal/httpapi"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrustedProxySeparatesClientRateLimits(t *testing.T) {
	f := database(t)
	service, _, err := auth.New(t.Context(), f.store)
	must(t, err)
	cfg, err := config.Parse("http://localhost:8081", "")
	must(t, err)
	cfg.TrustedProxies, err = config.ParseTrustedProxies("10.0.0.2")
	must(t, err)
	handler := httpapi.New(service, nil, nil, nil, cfg)
	request := func(peer, forwarded string, want int) {
		t.Helper()
		r := httptest.NewRequest("POST", "http://localhost:8081/api/setup/unlock", strings.NewReader(`{"code":"wrong"}`))
		r.RemoteAddr = peer
		r.Header.Set("Origin", "http://localhost:8081")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", forwarded)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s via %s: %d want %d: %s", forwarded, peer, w.Code, want, w.Body.String())
		}
	}
	for i := 0; i < 10; i++ {
		request("10.0.0.2:123", fmt.Sprintf("203.0.113.%d,198.51.100.1", i+1), 422)
	}
	request("10.0.0.2:123", "203.0.113.88,198.51.100.1", 429)
	request("10.0.0.2:123", "198.51.100.2", 422)
	// An untrusted direct caller cannot evade its own limit with changing headers.
	for i := 0; i < 10; i++ {
		request("192.0.2.8:123", fmt.Sprintf("203.0.113.%d", i+1), 422)
	}
	request("192.0.2.8:123", "203.0.113.88", 429)
}
