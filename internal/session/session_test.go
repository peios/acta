package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

// fixture builds a manager over a fresh memstore with one user, and returns a
// pointer to the clock the manager reads so tests can advance time.
func fixture(t *testing.T) (*Manager, *memstore.Store, identity.Principal, *time.Time) {
	t.Helper()
	ms := memstore.New()
	u, err := ms.CreateUser(context.Background(), store.NewUser{Username: "jack", Display: "Jack"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	m := New(ms, Config{
		IdleTimeout:     time.Hour,
		AbsoluteTimeout: 24 * time.Hour,
		RefreshInterval: time.Minute,
	})
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	return m, ms, identity.Principal{ID: u.ID, Username: u.Username, Display: u.Display}, &now
}

// establish runs Establish and returns a request carrying the new cookie.
func establish(t *testing.T, m *Manager, p identity.Principal) *http.Request {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := m.Establish(context.Background(), rec, p); err != nil {
		t.Fatalf("establish: %v", err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	return req
}

func TestEstablishAndCurrent(t *testing.T) {
	m, _, p, _ := fixture(t)
	req := establish(t, m, p)

	got, err := m.Current(context.Background(), req)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if got == nil || got.ID != p.ID {
		t.Fatalf("expected principal %s, got %+v", p.ID, got)
	}
}

func TestCurrentNoCookie(t *testing.T) {
	m, _, _, _ := fixture(t)
	req := httptest.NewRequest("GET", "/", nil)
	got, err := m.Current(context.Background(), req)
	if err != nil || got != nil {
		t.Fatalf("expected (nil,nil), got (%v,%v)", got, err)
	}
}

func TestIdleExpiry(t *testing.T) {
	m, ms, p, now := fixture(t)
	req := establish(t, m, p)

	*now = now.Add(time.Hour + time.Minute) // past idle timeout
	got, err := m.Current(context.Background(), req)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if got != nil {
		t.Fatal("expected idle-expired session to be nil")
	}
	if ms.SessionCount() != 0 {
		t.Fatal("expected idle-expired session to be deleted")
	}
}

func TestAbsoluteExpiry(t *testing.T) {
	m, ms, p, now := fixture(t)
	req := establish(t, m, p)

	// Stay active (within idle) but cross the absolute ceiling by touching
	// the session every 30 minutes for over 24 hours.
	for range 50 {
		*now = now.Add(30 * time.Minute)
		_, _ = m.Current(context.Background(), req)
	}
	got, err := m.Current(context.Background(), req)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if got != nil {
		t.Fatal("expected absolute-expired session to be nil even while active")
	}
	if ms.SessionCount() != 0 {
		t.Fatal("expected absolute-expired session to be deleted")
	}
}

func TestDestroy(t *testing.T) {
	m, ms, p, _ := fixture(t)
	req := establish(t, m, p)

	rec := httptest.NewRecorder()
	if err := m.Destroy(context.Background(), rec, req); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if ms.SessionCount() != 0 {
		t.Fatal("expected session to be removed server-side on destroy")
	}
	// Cookie is cleared (MaxAge < 0).
	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == "acta_session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("expected destroy to clear the session cookie")
	}
}
