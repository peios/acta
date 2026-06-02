package local

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/peios/acta/internal/httpx"
	"github.com/peios/acta/internal/passkey"
	"github.com/peios/acta/internal/session"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

// newThrottledProvider builds a real local provider backed by memstore, with a
// throttle guard whose sleeper is a no-op so backoff never actually blocks the
// test. The seeded account is alice / "correct horse".
func newThrottledProvider(t *testing.T, cfg ThrottleConfig) *Provider {
	t.Helper()
	ms := memstore.New()
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.CreateUser(context.Background(), store.NewUser{
		Username: "alice", Display: "Alice", PasswordHash: hash,
	}); err != nil {
		t.Fatal(err)
	}

	sessions := session.New(ms, session.Config{IdleTimeout: time.Hour, AbsoluteTimeout: time.Hour})
	passkeys, err := passkey.New(ms, passkey.Config{RPID: "localhost", RPOrigin: "http://localhost:8080", RPName: "Acta"})
	if err != nil {
		t.Fatal(err)
	}

	guard := NewGuard(cfg)
	guard.sleep = func(time.Duration) {} // don't really block on backoff
	return NewProvider(ms, sessions, passkeys, false, WithThrottle(guard))
}

func attemptLogin(p *Provider, ip, username, password string) string {
	form := url.Values{"username": {username}, "password": {password}}
	r := httptest.NewRequest(http.MethodPost, "/login/password", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = r.WithContext(httpx.WithRequestMeta(r.Context(), "req", ip))
	w := httptest.NewRecorder()
	p.handlePassword(w, r)
	return w.Result().Header.Get("Location")
}

func TestLoginThrottleBlocksAfterIPMax(t *testing.T) {
	p := newThrottledProvider(t, ThrottleConfig{
		Window: time.Minute, IPMax: 3, BackoffStep: time.Second, BackoffMax: 5 * time.Second,
	})

	const attacker = "203.0.113.50"
	for i := range 3 {
		loc := attemptLogin(p, attacker, "alice", "wrong")
		if !strings.Contains(loc, "err=invalid_credentials") {
			t.Fatalf("attempt %d: want invalid_credentials, got %q", i+1, loc)
		}
	}

	// The next attempt is refused before any password is verified.
	if loc := attemptLogin(p, attacker, "alice", "wrong"); !strings.Contains(loc, "err=too_many") {
		t.Fatalf("want too_many after IPMax failures, got %q", loc)
	}
	// Even the correct password is refused while the IP is blocked.
	if loc := attemptLogin(p, attacker, "alice", "correct horse"); !strings.Contains(loc, "err=too_many") {
		t.Fatalf("blocked IP must be refused regardless of password, got %q", loc)
	}
	// A different IP with the right password still gets in (no error redirect).
	if loc := attemptLogin(p, "198.51.100.7", "alice", "correct horse"); strings.Contains(loc, "err=") {
		t.Fatalf("a fresh IP with the correct password should succeed, got %q", loc)
	}
}

func TestLoginThrottleUnknownUserCountsTowardIPLimit(t *testing.T) {
	// Spraying random usernames from one IP must still trip the per-IP limit —
	// the throttle keys on IP, not just on known accounts.
	p := newThrottledProvider(t, ThrottleConfig{Window: time.Minute, IPMax: 2})

	const attacker = "203.0.113.99"
	_ = attemptLogin(p, attacker, "ghost1", "x")
	_ = attemptLogin(p, attacker, "ghost2", "x")
	if loc := attemptLogin(p, attacker, "ghost3", "x"); !strings.Contains(loc, "err=too_many") {
		t.Fatalf("want too_many after spraying unknown users, got %q", loc)
	}
}
