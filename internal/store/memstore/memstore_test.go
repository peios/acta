package memstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestEventsOrderingAndLimit(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()

	// Explicit, strictly increasing timestamps keep the ordering assertion
	// deterministic regardless of clock resolution.
	base := time.Unix(1_700_000_000, 0)
	verbs := []string{"a", "b", "c", "d"}
	for i, v := range verbs {
		if _, err := ms.RecordEvent(ctx, store.Event{
			WorkspaceID: "ws", ItemID: "it", Verb: v,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ms.EventsByItem(ctx, "it", 0) // 0 → default cap, returns all
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(verbs) {
		t.Fatalf("want %d events, got %d", len(verbs), len(got))
	}
	// Newest-first: the last recorded verb is first.
	if got[0].Verb != "d" || got[len(got)-1].Verb != "a" {
		t.Fatalf("ordering wrong: %s … %s", got[0].Verb, got[len(got)-1].Verb)
	}
	// A non-nil empty data map comes back, never nil.
	if got[0].Data == nil {
		t.Fatal("event Data should be non-nil")
	}

	// limit truncates to the newest N.
	top2, err := ms.EventsByItem(ctx, "it", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(top2) != 2 || top2[0].Verb != "d" || top2[1].Verb != "c" {
		t.Fatalf("limit=2 wrong: %+v", top2)
	}

	// Item scoping: a different item sees nothing.
	if other, _ := ms.EventsByItem(ctx, "nope", 10); len(other) != 0 {
		t.Fatalf("want no events for unknown item, got %d", len(other))
	}
}
