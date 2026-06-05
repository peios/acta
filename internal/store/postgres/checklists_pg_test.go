package postgres

import (
	"context"
	"testing"

	"github.com/peios/acta/internal/store"
)

// TestPGChecklists exercises the status-checklist SQL against real Postgres: the
// bigserial fact ids, the case-insensitive unique title, the FactsByStatus join,
// the SetItemFact upsert/delete, SetItemPending, and the cascades on DeleteFact —
// none of which the memstore can validate.
func TestPGChecklists(t *testing.T) {
	pg := openTestDB(t)
	ctx := context.Background()

	ws, err := pg.CreateWorkspace(ctx, store.Workspace{Slug: "ck", Name: "Checklists"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteWorkspace(ctx, ws.ID) })
	bd, err := pg.CreateBoard(ctx, store.Board{WorkspaceID: ws.ID, Name: "Tasks", Slug: "tasks", Position: 0})
	if err != nil {
		t.Fatal(err)
	}
	st, err := pg.CreateStatus(ctx, store.Status{WorkspaceID: ws.ID, BoardID: bd.ID, Name: "Release Ready"})
	if err != nil {
		t.Fatal(err)
	}
	it, err := pg.CreateItem(ctx, store.Item{WorkspaceID: ws.ID, StatusID: st.ID, Title: "Ship"})
	if err != nil {
		t.Fatal(err)
	}
	ada, err := pg.CreateUser(ctx, store.NewUser{Username: "ck_ada", Display: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteUser(ctx, ada.ID) })

	// Facts get distinct bigserial ids; the title is unique case-insensitively.
	f1, err := pg.CreateFact(ctx, ws.ID, "Provium tests")
	if err != nil {
		t.Fatal(err)
	}
	f2, err := pg.CreateFact(ctx, ws.ID, "Learn docs")
	if err != nil {
		t.Fatal(err)
	}
	if f1.ID == 0 || f1.ID == f2.ID {
		t.Fatalf("expected distinct nonzero ids, got %d and %d", f1.ID, f2.ID)
	}
	if _, err := pg.CreateFact(ctx, ws.ID, "PROVIUM TESTS"); err != store.ErrFactTitleTaken {
		t.Fatalf("dup err = %v, want ErrFactTitleTaken", err)
	}

	// Gate the status with both facts, in order.
	if err := pg.SetStatusFacts(ctx, st.ID, []int64{f1.ID, f2.ID}); err != nil {
		t.Fatal(err)
	}
	gating, err := pg.FactsByStatus(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gating) != 2 || gating[0].ID != f1.ID || gating[1].ID != f2.ID {
		t.Fatalf("FactsByStatus = %+v, want [f1 f2] in order", gating)
	}

	// Tick f1 (upsert), then re-tick (ON CONFLICT refresh), then check the set.
	if err := pg.SetItemFact(ctx, it.ID, f1.ID, true, ada.ID); err != nil {
		t.Fatal(err)
	}
	if err := pg.SetItemFact(ctx, it.ID, f1.ID, true, ada.ID); err != nil {
		t.Fatalf("re-tick should upsert, got %v", err)
	}
	ticks, err := pg.TicksByItem(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ticks) != 1 || ticks[0].FactID != f1.ID || ticks[0].CheckedBy != ada.ID {
		t.Fatalf("ticks = %+v, want one f1 tick by ada", ticks)
	}
	// Untick deletes the row.
	if err := pg.SetItemFact(ctx, it.ID, f1.ID, false, ada.ID); err != nil {
		t.Fatal(err)
	}
	if ticks, _ := pg.TicksByItem(ctx, it.ID); len(ticks) != 0 {
		t.Fatalf("untick should delete, got %d", len(ticks))
	}

	// Pending transition round-trips on the item.
	if err := pg.SetItemPending(ctx, it.ID, st.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := pg.ItemByID(ctx, it.ID)
	if got.PendingStatusID != st.ID {
		t.Fatalf("pending = %q, want %s", got.PendingStatusID, st.ID)
	}
	if err := pg.SetItemPending(ctx, it.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := pg.ItemByID(ctx, it.ID); got.PendingStatusID != "" {
		t.Fatalf("pending should clear, got %q", got.PendingStatusID)
	}

	// Tick f2, then delete the fact — the tick and the gate row cascade away.
	if err := pg.SetItemFact(ctx, it.ID, f2.ID, true, ada.ID); err != nil {
		t.Fatal(err)
	}
	if err := pg.DeleteFact(ctx, f2.ID); err != nil {
		t.Fatal(err)
	}
	gating, _ = pg.FactsByStatus(ctx, st.ID)
	if len(gating) != 1 || gating[0].ID != f1.ID {
		t.Fatalf("after delete, gating = %+v, want just f1", gating)
	}
	if ticks, _ := pg.TicksByItem(ctx, it.ID); len(ticks) != 0 {
		t.Fatalf("deleting a fact should cascade its ticks, got %d", len(ticks))
	}
}
