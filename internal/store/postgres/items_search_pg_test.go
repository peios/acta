package postgres

import (
	"context"
	"testing"

	"github.com/peios/acta/internal/store"
)

// TestPGSearchItems exercises the ILIKE search against real Postgres — with the
// pg_trgm migration applied by openTestDB — covering the bits the memstore can't
// prove about the SQL: substring matching over title and description, case
// insensitivity, the LIKE-wildcard escape (so "%"/"_" stay literal),
// archived gating, title-before-body ordering, and workspace scoping.
func TestPGSearchItems(t *testing.T) {
	pg := openTestDB(t)
	ctx := context.Background()

	ws, err := pg.CreateWorkspace(ctx, store.Workspace{Slug: "search", Name: "Search"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteWorkspace(ctx, ws.ID) })
	other, err := pg.CreateWorkspace(ctx, store.Workspace{Slug: "other", Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteWorkspace(ctx, other.ID) })

	bd, err := pg.CreateBoard(ctx, store.Board{WorkspaceID: ws.ID, Name: "Tasks", Slug: "tasks"})
	if err != nil {
		t.Fatal(err)
	}
	todo, err := pg.CreateStatus(ctx, store.Status{WorkspaceID: ws.ID, BoardID: bd.ID, Name: "To do"})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(title, desc string) store.Item {
		it, err := pg.CreateItem(ctx, store.Item{WorkspaceID: ws.ID, StatusID: todo.ID, Title: title})
		if err != nil {
			t.Fatal(err)
		}
		if desc != "" {
			if err := pg.UpdateItemDescription(ctx, it.ID, desc); err != nil {
				t.Fatal(err)
			}
		}
		return it
	}

	titleHit := mk("loregd device-wiring", "")
	descHit := mk("unrelated card", "notes on the registry daemon")
	pct := mk("100% migrated", "")
	und := mk("a_b boundary", "")
	mk("axb other", "")
	zTitle := mk("zeta crown", "")
	zBody := mk("plain card", "has zeta inside the body")
	archived := mk("widget retired", "")
	if err := pg.ArchiveItem(ctx, archived.ID); err != nil {
		t.Fatal(err)
	}
	mk("widget live", "")

	// A matching title in another workspace must never leak in.
	ob, _ := pg.CreateBoard(ctx, store.Board{WorkspaceID: other.ID, Name: "Tasks", Slug: "tasks"})
	ot, _ := pg.CreateStatus(ctx, store.Status{WorkspaceID: other.ID, BoardID: ob.ID, Name: "To do"})
	if _, err := pg.CreateItem(ctx, store.Item{WorkspaceID: other.ID, StatusID: ot.ID, Title: "loregd elsewhere"}); err != nil {
		t.Fatal(err)
	}

	// Title + case-insensitivity + workspace scoping (only this ws's loregd).
	if got, err := pg.SearchItems(ctx, ws.ID, "", "LOREGD", false); err != nil || len(got) != 1 || got[0].ID != titleHit.ID {
		t.Fatalf("title/case/scoped search = %v, err %v", got, err)
	}
	// Description substring.
	if got, _ := pg.SearchItems(ctx, ws.ID, "", "registry", false); len(got) != 1 || got[0].ID != descHit.ID {
		t.Fatalf("description search wrong: %v", got)
	}
	// LIKE wildcards stay literal (the escape).
	if got, _ := pg.SearchItems(ctx, ws.ID, "", "%", false); len(got) != 1 || got[0].ID != pct.ID {
		t.Fatalf(`literal "%%" search wrong: %v`, got)
	}
	if got, _ := pg.SearchItems(ctx, ws.ID, "", "a_b", false); len(got) != 1 || got[0].ID != und.ID {
		t.Fatalf(`literal "a_b" search wrong: %v`, got)
	}
	// Title matches rank above body-only matches.
	if got, _ := pg.SearchItems(ctx, ws.ID, "", "zeta", false); len(got) != 2 || got[0].ID != zTitle.ID || got[1].ID != zBody.ID {
		t.Fatalf("title-before-body ordering wrong: %v", got)
	}
	// Archived gating.
	if got, _ := pg.SearchItems(ctx, ws.ID, "", "widget", false); len(got) != 1 {
		t.Fatalf("default search must skip archived: %v", got)
	}
	if got, _ := pg.SearchItems(ctx, ws.ID, "", "widget", true); len(got) != 2 {
		t.Fatalf("include-archived search: want 2, got %v", got)
	}
}
