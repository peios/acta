package board_test

import (
	"context"
	"errors"
	"testing"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

// setup makes a workspace with the default lanes seeded and returns the board
// service plus the three status ids (To do, Doing, Done in order).
func setup(t *testing.T) (*board.Service, string, []store.Status) {
	t.Helper()
	ms := memstore.New()
	ctx := context.Background()
	ws, err := ms.CreateWorkspace(ctx, store.Workspace{Slug: "general", Name: "General"})
	if err != nil {
		t.Fatal(err)
	}
	svc := board.New(ms)
	if err := svc.SeedDefaults(ctx, ws.ID); err != nil {
		t.Fatal(err)
	}
	statuses, err := svc.Statuses(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != len(board.DefaultStatuses) {
		t.Fatalf("want %d seeded statuses, got %d", len(board.DefaultStatuses), len(statuses))
	}
	return svc, ws.ID, statuses
}

// laneTitles returns the titles in a lane, in position order.
func laneTitles(t *testing.T, svc *board.Service, wsID, statusID string) []string {
	t.Helper()
	items, err := svc.Items(context.Background(), wsID)
	if err != nil {
		t.Fatal(err)
	}
	// Items() returns the whole workspace; filter + rely on position order
	// being preserved per status by the store's ORDER BY position.
	var out []string
	for _, it := range items {
		if it.StatusID == statusID {
			out = append(out, it.Title)
		}
	}
	return out
}

func TestSeedDefaults(t *testing.T) {
	_, _, statuses := setup(t)
	for i, want := range board.DefaultStatuses {
		if statuses[i].Name != want || statuses[i].Position != i {
			t.Fatalf("status %d: want %q@%d, got %q@%d", i, want, i, statuses[i].Name, statuses[i].Position)
		}
	}
}

func TestCreateItemAppendsToLane(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	todo := st[0].ID

	a, _ := svc.CreateItem(ctx, wsID, todo, "A")
	b, _ := svc.CreateItem(ctx, wsID, todo, "B")
	if a.Position != 0 || b.Position != 1 {
		t.Fatalf("append positions: want 0,1 got %d,%d", a.Position, b.Position)
	}
	if got := laneTitles(t, svc, wsID, todo); len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("lane order: want [A B], got %v", got)
	}
}

func TestMoveItemWithinLane(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	todo := st[0].ID

	svc.CreateItem(ctx, wsID, todo, "A")
	svc.CreateItem(ctx, wsID, todo, "B")
	c, _ := svc.CreateItem(ctx, wsID, todo, "C")

	if err := svc.MoveItem(ctx, c.ID, todo, 0); err != nil {
		t.Fatal(err)
	}
	if got := laneTitles(t, svc, wsID, todo); len(got) != 3 || got[0] != "C" || got[1] != "A" || got[2] != "B" {
		t.Fatalf("after move-to-front: want [C A B], got %v", got)
	}
}

func TestMoveItemAcrossLanes(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	todo, doing := st[0].ID, st[1].ID

	a, _ := svc.CreateItem(ctx, wsID, todo, "A")
	svc.CreateItem(ctx, wsID, todo, "B")

	if err := svc.MoveItem(ctx, a.ID, doing, 0); err != nil {
		t.Fatal(err)
	}
	// Source lane re-densified to just [B] at position 0.
	src := laneTitles(t, svc, wsID, todo)
	if len(src) != 1 || src[0] != "B" {
		t.Fatalf("source lane: want [B], got %v", src)
	}
	// Destination lane now holds [A].
	dst := laneTitles(t, svc, wsID, doing)
	if len(dst) != 1 || dst[0] != "A" {
		t.Fatalf("dest lane: want [A], got %v", dst)
	}
	// And B's position is dense (0), not a stale 1.
	for _, it := range mustItems(t, svc, wsID) {
		if it.Title == "B" && it.Position != 0 {
			t.Fatalf("B should be re-densified to position 0, got %d", it.Position)
		}
	}
}

func TestDeleteStatusBlockedWhenNonEmpty(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	todo := st[0].ID

	svc.CreateItem(ctx, wsID, todo, "A")
	if err := svc.DeleteStatus(ctx, todo); !errors.Is(err, board.ErrStatusNotEmpty) {
		t.Fatalf("delete non-empty lane: want ErrStatusNotEmpty, got %v", err)
	}
	// Empty it, then deletion succeeds.
	items := mustItems(t, svc, wsID)
	if err := svc.DeleteItem(ctx, items[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteStatus(ctx, todo); err != nil {
		t.Fatalf("delete emptied lane: %v", err)
	}
}

func TestCreateItemRejectsForeignStatus(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()

	// A status id from a *different* workspace must be rejected.
	ms2 := memstore.New()
	other, _ := ms2.CreateWorkspace(ctx, store.Workspace{Slug: "other", Name: "Other"})
	otherSvc := board.New(ms2)
	otherSvc.SeedDefaults(ctx, other.ID)
	otherStatuses, _ := otherSvc.Statuses(ctx, other.ID)

	_ = st
	if _, err := svc.CreateItem(ctx, wsID, otherStatuses[0].ID, "X"); !errors.Is(err, store.ErrStatusNotFound) {
		t.Fatalf("foreign status (different store): want ErrStatusNotFound, got %v", err)
	}
}

func TestInvalidInput(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()

	if _, err := svc.CreateStatus(ctx, wsID, "   "); !errors.Is(err, board.ErrInvalidName) {
		t.Fatalf("blank status name: want ErrInvalidName, got %v", err)
	}
	if _, err := svc.CreateItem(ctx, wsID, st[0].ID, ""); !errors.Is(err, board.ErrInvalidTitle) {
		t.Fatalf("blank item title: want ErrInvalidTitle, got %v", err)
	}
}

func mustItems(t *testing.T, svc *board.Service, wsID string) []store.Item {
	t.Helper()
	items, err := svc.Items(context.Background(), wsID)
	if err != nil {
		t.Fatal(err)
	}
	return items
}
