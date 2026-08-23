package postgres

import (
	"context"
	"testing"

	"github.com/peios/acta/internal/store"
)

// TestPGReleases exercises the release SQL against real Postgres: the
// case-insensitive unique name, the many-to-many item_releases join (including a
// single item belonging to two releases — the backport substrate), the ship
// state machine, the workspace link map, the per-release counts, and the cascades
// when a release or an item is deleted — none of which the memstore can validate.
func TestPGReleases(t *testing.T) {
	pg := openTestDB(t)
	ctx := context.Background()

	ws, err := pg.CreateWorkspace(ctx, store.Workspace{Slug: "rel", Name: "Releases"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteWorkspace(ctx, ws.ID) })
	bd, err := pg.CreateBoard(ctx, store.Board{WorkspaceID: ws.ID, Name: "Tasks", Slug: "tasks", Position: 0})
	if err != nil {
		t.Fatal(err)
	}
	todo, err := pg.CreateStatus(ctx, store.Status{WorkspaceID: ws.ID, BoardID: bd.ID, Name: "To do"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := pg.CreateStatus(ctx, store.Status{WorkspaceID: ws.ID, BoardID: bd.ID, Name: "Done", Position: 1})
	if err != nil {
		t.Fatal(err)
	}
	itA, err := pg.CreateItem(ctx, store.Item{WorkspaceID: ws.ID, StatusID: todo.ID, Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	itB, err := pg.CreateItem(ctx, store.Item{WorkspaceID: ws.ID, StatusID: done.ID, Title: "B"})
	if err != nil {
		t.Fatal(err)
	}

	// Distinct releases; the name is unique within the workspace, case-insensitively.
	r1, err := pg.CreateRelease(ctx, store.Release{WorkspaceID: ws.ID, Name: "v0.27.0", Position: 0})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := pg.CreateRelease(ctx, store.Release{WorkspaceID: ws.ID, Name: "v0.28.0", Position: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pg.CreateRelease(ctx, store.Release{WorkspaceID: ws.ID, Name: "V0.27.0"}); err != store.ErrReleaseNameTaken {
		t.Fatalf("dup name err = %v, want ErrReleaseNameTaken", err)
	}
	if list, _ := pg.ReleasesByWorkspace(ctx, ws.ID); len(list) != 2 || list[0].ID != r1.ID || list[1].ID != r2.ID {
		t.Fatalf("ReleasesByWorkspace = %+v, want [r1 r2] by position", list)
	}

	// Membership: A in r1; B in r1 too. ItemsByRelease(r1) returns both.
	if err := pg.SetItemRelease(ctx, itA.ID, r1.ID); err != nil {
		t.Fatal(err)
	}
	if err := pg.SetItemRelease(ctx, itB.ID, r1.ID); err != nil {
		t.Fatal(err)
	}
	if items, _ := pg.ItemsByRelease(ctx, r1.ID); len(items) != 2 {
		t.Fatalf("ItemsByRelease(r1) = %d, want 2", len(items))
	}

	// Backport substrate: the join is many-to-many, so A can also belong to r2.
	// (The UI's SetItemRelease replaces, so reach past it to prove the schema.)
	if _, err := pg.pool.Exec(ctx,
		`INSERT INTO item_releases (item_id, release_id) VALUES ($1, $2)`, itA.ID, r2.ID); err != nil {
		t.Fatalf("second membership insert: %v", err)
	}
	if rels, _ := pg.ReleasesByItem(ctx, itA.ID); len(rels) != 2 {
		t.Fatalf("ReleasesByItem(A) = %d, want 2 (in two releases)", len(rels))
	}

	// Workspace link map: A -> {r1, r2}, B -> {r1}.
	links, err := pg.ReleaseLinksByWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links[itA.ID]) != 2 || len(links[itB.ID]) != 1 {
		t.Fatalf("links = %+v, want A:2 B:1", links)
	}

	// Per-release counts, broken down by size: r1 has 2 items, one of them (B) in
	// the Done lane.
	counts, err := pg.ReleaseSizeCounts(ctx, ws.ID, []string{done.ID})
	if err != nil {
		t.Fatal(err)
	}
	var total, doneCount int
	for _, c := range counts[r1.ID] {
		total, doneCount = total+c.Total, doneCount+c.Done
	}
	if total != 2 || doneCount != 1 {
		t.Fatalf("counts[r1] = %d/%d, want 1/2", doneCount, total)
	}

	// Status machine: shipping stamps shipped_at; moving to planned clears it.
	if err := pg.SetReleaseStatus(ctx, r1.ID, "shipped"); err != nil {
		t.Fatal(err)
	}
	if got, _ := pg.ReleaseByID(ctx, r1.ID); got.Status != "shipped" || got.ShippedAt == nil {
		t.Fatalf("after ship: %q / %v", got.Status, got.ShippedAt)
	}
	if err := pg.SetReleaseStatus(ctx, r1.ID, "planned"); err != nil {
		t.Fatal(err)
	}
	if got, _ := pg.ReleaseByID(ctx, r1.ID); got.Status != "planned" || got.ShippedAt != nil {
		t.Fatalf("after re-plan: %q / %v", got.Status, got.ShippedAt)
	}

	// Deleting an item cascades its memberships: A drops out of both releases.
	if err := pg.DeleteItem(ctx, itA.ID); err != nil {
		t.Fatal(err)
	}
	if rels, _ := pg.ReleasesByItem(ctx, itA.ID); len(rels) != 0 {
		t.Fatalf("deleting an item should cascade its memberships, got %d", len(rels))
	}

	// Deleting a release cascades the join but leaves the items.
	if err := pg.DeleteRelease(ctx, r1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.ReleaseByID(ctx, r1.ID); err != store.ErrReleaseNotFound {
		t.Fatalf("deleted release still resolves: %v", err)
	}
	if rels, _ := pg.ReleasesByItem(ctx, itB.ID); len(rels) != 0 {
		t.Fatalf("B still tagged with the deleted release (%d)", len(rels))
	}
	if _, err := pg.ItemByID(ctx, itB.ID); err != nil {
		t.Fatalf("item B should survive release deletion: %v", err)
	}
}
