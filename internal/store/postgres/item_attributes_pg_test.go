package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/peios/acta/internal/store"
)

// TestPGItemAttributes covers the priority/type/size/due columns: defaults on a
// fresh item, the four setters (including create-time values and clearing back to
// unset), and ErrItemNotFound on an unknown id.
func TestPGItemAttributes(t *testing.T) {
	pg := openTestDB(t)
	ctx := context.Background()

	ws, err := pg.CreateWorkspace(ctx, store.Workspace{Slug: "attrs", Name: "Attrs"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteWorkspace(ctx, ws.ID) })
	bd, err := pg.CreateBoard(ctx, store.Board{WorkspaceID: ws.ID, Name: "Tasks", Slug: "tasks"})
	if err != nil {
		t.Fatal(err)
	}
	todo, err := pg.CreateStatus(ctx, store.Status{WorkspaceID: ws.ID, BoardID: bd.ID, Name: "To do"})
	if err != nil {
		t.Fatal(err)
	}

	// Fresh item: every attribute defaults to unset.
	it, err := pg.CreateItem(ctx, store.Item{WorkspaceID: ws.ID, StatusID: todo.ID, Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if it.Priority != 0 || it.Type != 0 || it.Size != 0 || it.DueDate != nil {
		t.Fatalf("fresh item not unset: %+v", it)
	}

	// Create-time values round-trip through the INSERT.
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	it2, err := pg.CreateItem(ctx, store.Item{
		WorkspaceID: ws.ID, StatusID: todo.ID, Title: "B",
		Priority: 4, Type: 2, Size: 3, DueDate: &due,
	})
	if err != nil {
		t.Fatal(err)
	}
	if it2.Priority != 4 || it2.Type != 2 || it2.Size != 3 || it2.DueDate == nil || !it2.DueDate.Equal(due) {
		t.Fatalf("create-time attrs not persisted: %+v", it2)
	}

	// Setters update each column independently.
	if err := pg.SetItemPriority(ctx, it.ID, 3); err != nil {
		t.Fatal(err)
	}
	if err := pg.SetItemType(ctx, it.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := pg.SetItemSize(ctx, it.ID, 5); err != nil {
		t.Fatal(err)
	}
	if err := pg.SetItemDue(ctx, it.ID, &due); err != nil {
		t.Fatal(err)
	}
	got, err := pg.ItemByID(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Priority != 3 || got.Type != 1 || got.Size != 5 || got.DueDate == nil || !got.DueDate.Equal(due) {
		t.Fatalf("setters not reflected: %+v", got)
	}

	// Clearing back to unset: 0 for the enums, nil for the date.
	if err := pg.SetItemPriority(ctx, it.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := pg.SetItemDue(ctx, it.ID, nil); err != nil {
		t.Fatal(err)
	}
	got, err = pg.ItemByID(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Priority != 0 || got.DueDate != nil {
		t.Fatalf("clear failed: priority=%d due=%v", got.Priority, got.DueDate)
	}

	// Unknown id is a not-found on every setter.
	for _, call := range []func() error{
		func() error { return pg.SetItemPriority(ctx, "nope", 1) },
		func() error { return pg.SetItemType(ctx, "nope", 1) },
		func() error { return pg.SetItemSize(ctx, "nope", 1) },
		func() error { return pg.SetItemDue(ctx, "nope", &due) },
	} {
		if err := call(); !errors.Is(err, store.ErrItemNotFound) {
			t.Errorf("unknown id: want ErrItemNotFound, got %v", err)
		}
	}
}
