package apitoken

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

func newUser(t *testing.T, ms *memstore.Store) store.User {
	t.Helper()
	u, err := ms.CreateUser(context.Background(), store.NewUser{Username: "jack", Display: "Jack"})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestMintAuthenticateRoundtrip(t *testing.T) {
	ms := memstore.New()
	u := newUser(t, ms)
	svc := New(ms)

	plain, tok, err := svc.Mint(context.Background(), u.ID, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, Prefix) {
		t.Fatalf("token missing prefix: %q", plain)
	}
	if tok.Name != "laptop" {
		t.Fatalf("name not stored: %q", tok.Name)
	}
	// The stored display prefix is non-secret: it shares the prefix but is far
	// shorter than the full token.
	if !strings.HasPrefix(tok.Prefix, Prefix) || len(tok.Prefix) >= len(plain) {
		t.Fatalf("display prefix wrong: %q (full %q)", tok.Prefix, plain)
	}

	p, err := svc.Authenticate(context.Background(), plain)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if p.ID != u.ID || p.Username != "jack" {
		t.Fatalf("authenticated as wrong principal: %+v", p)
	}
}

func TestAuthenticateRejectsBadTokens(t *testing.T) {
	ms := memstore.New()
	svc := New(ms)
	bad := []string{"", "nope", "Bearer x", Prefix + "deadbeef", "acta_patX"}
	for _, tok := range bad {
		if _, err := svc.Authenticate(context.Background(), tok); err != ErrInvalidToken {
			t.Fatalf("Authenticate(%q): want ErrInvalidToken, got %v", tok, err)
		}
	}
}

func TestRevokeInvalidatesToken(t *testing.T) {
	ms := memstore.New()
	u := newUser(t, ms)
	svc := New(ms)

	plain, tok, _ := svc.Mint(context.Background(), u.ID, "")
	if err := svc.Revoke(context.Background(), tok.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(context.Background(), plain); err != ErrInvalidToken {
		t.Fatalf("revoked token still authenticates: %v", err)
	}
}

func TestRevokeScopedToOwner(t *testing.T) {
	ms := memstore.New()
	u := newUser(t, ms)
	svc := New(ms)
	_, tok, _ := svc.Mint(context.Background(), u.ID, "")

	// A different user can't revoke it.
	if err := svc.Revoke(context.Background(), tok.ID, "someone-else"); err != store.ErrAPITokenNotFound {
		t.Fatalf("cross-user revoke: want ErrAPITokenNotFound, got %v", err)
	}
}

func TestAuthenticateTouchesLastUsed(t *testing.T) {
	ms := memstore.New()
	u := newUser(t, ms)
	svc := New(ms)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	plain, _, _ := svc.Mint(context.Background(), u.ID, "")
	if _, err := svc.Authenticate(context.Background(), plain); err != nil {
		t.Fatal(err)
	}
	got, _ := ms.APITokensByUserID(context.Background(), u.ID)
	if len(got) != 1 || got[0].LastUsedAt == nil {
		t.Fatalf("expected last_used set after auth, got %+v", got)
	}
}
