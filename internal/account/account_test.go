package account_test

import (
	"context"
	"errors"
	"testing"

	"github.com/peios/acta/internal/account"
	"github.com/peios/acta/internal/authn/local"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

func seedHuman(t *testing.T, ms *memstore.Store, username string) store.User {
	t.Helper()
	hash, err := local.HashPassword("s3cret-passw0rd")
	if err != nil {
		t.Fatal(err)
	}
	u, err := ms.CreateUser(context.Background(), store.NewUser{
		Username: username, Display: username, PasswordHash: hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestCreateValidatesAndHashes(t *testing.T) {
	ms := memstore.New()
	svc := account.New(ms)
	ctx := context.Background()

	u, err := svc.Create(ctx, "Jordan", "Jordan Doe", "s3cret-passw0rd")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.Username != "jordan" {
		t.Fatalf("username not normalized: %q", u.Username)
	}
	if u.PasswordHash == "" || u.PasswordHash == "s3cret-passw0rd" {
		t.Fatalf("password not hashed: %q", u.PasswordHash)
	}

	if _, err := svc.Create(ctx, "bad/name", "", "s3cret-passw0rd"); !errors.Is(err, account.ErrInvalidUsername) {
		t.Fatalf("slash username: want ErrInvalidUsername, got %v", err)
	}
	if _, err := svc.Create(ctx, "shorty", "", "short"); !errors.Is(err, account.ErrWeakPassword) {
		t.Fatalf("short password: want ErrWeakPassword, got %v", err)
	}
	if _, err := svc.Create(ctx, "jordan", "", "s3cret-passw0rd"); !errors.Is(err, store.ErrUsernameTaken) {
		t.Fatalf("duplicate: want ErrUsernameTaken, got %v", err)
	}
}

func TestDisableEnableAndLastActiveGuard(t *testing.T) {
	ms := memstore.New()
	svc := account.New(ms)
	ctx := context.Background()
	alice := seedHuman(t, ms, "alice")
	bob := seedHuman(t, ms, "bob")

	// Two active humans: disabling one is allowed.
	if err := svc.Disable(ctx, bob.ID); err != nil {
		t.Fatalf("disable bob: %v", err)
	}
	if got, _ := ms.UserByID(ctx, bob.ID); got.DisabledAt == nil {
		t.Fatal("bob should be disabled")
	}

	// Only alice remains active — she can't be disabled (lockout guard).
	if err := svc.Disable(ctx, alice.ID); !errors.Is(err, account.ErrLastActiveUser) {
		t.Fatalf("disable last active: want ErrLastActiveUser, got %v", err)
	}

	// Re-enabling bob restores access.
	if err := svc.Enable(ctx, bob.ID); err != nil {
		t.Fatalf("enable bob: %v", err)
	}
	if got, _ := ms.UserByID(ctx, bob.ID); got.DisabledAt != nil {
		t.Fatal("bob should be active again")
	}
}

func TestDisableAgentRejected(t *testing.T) {
	ms := memstore.New()
	svc := account.New(ms)
	ctx := context.Background()
	owner := seedHuman(t, ms, "owner")
	ag, err := ms.CreateUser(ctx, store.NewUser{
		Username: "owner/bot", Display: "bot", AgentOfID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Disable(ctx, ag.ID); !errors.Is(err, account.ErrNotHuman) {
		t.Fatalf("disable agent: want ErrNotHuman, got %v", err)
	}
}
