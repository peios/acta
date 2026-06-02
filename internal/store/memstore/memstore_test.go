package memstore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

func TestSetUserPassword(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	u, err := ms.CreateUser(ctx, store.NewUser{Username: "jack", Display: "Jack", PasswordHash: "old"})
	if err != nil {
		t.Fatal(err)
	}

	if err := ms.SetUserPassword(ctx, u.ID, "new"); err != nil {
		t.Fatalf("SetUserPassword: %v", err)
	}
	got, err := ms.UserByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordHash != "new" {
		t.Errorf("password hash = %q, want %q", got.PasswordHash, "new")
	}
}

func TestSetUserPasswordUnknownUser(t *testing.T) {
	ms := memstore.New()
	if err := ms.SetUserPassword(context.Background(), "nope", "x"); !errors.Is(err, store.ErrUserNotFound) {
		t.Errorf("want ErrUserNotFound, got %v", err)
	}
}
