package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/peios/acta/internal/store"
)

// TestPGBoardViews exercises the board_views CRUD against real Postgres — the
// bits memstore can't prove: created_at/created_by round-tripping (incl. the
// NULL → "" path), position ordering, the unique(board_id, slug) backstop, the
// atomic reorder, and ErrBoardViewNotFound on a missing id.
func TestPGBoardViews(t *testing.T) {
	pg := openTestDB(t)
	ctx := context.Background()

	ws, err := pg.CreateWorkspace(ctx, store.Workspace{Slug: "views", Name: "Views"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteWorkspace(ctx, ws.ID) })
	bd, err := pg.CreateBoard(ctx, store.Board{WorkspaceID: ws.ID, Name: "Tasks", Slug: "tasks"})
	if err != nil {
		t.Fatal(err)
	}
	author, err := pg.CreateUser(ctx, store.NewUser{Username: "viewer", Display: "Viewer"})
	if err != nil {
		t.Fatal(err)
	}

	mk := func(slug, name, query string, pos int, by string) store.BoardView {
		v, err := pg.CreateBoardView(ctx, store.BoardView{
			WorkspaceID: ws.ID, BoardID: bd.ID, Slug: slug, Name: name,
			Icon: "filter", Query: query, Position: pos, CreatedBy: by,
		})
		if err != nil {
			t.Fatalf("create %s: %v", slug, err)
		}
		return v
	}

	// created_by round-trips both ways: a real user id, and "" (stored NULL).
	withBy := mk("all-items", "All items", "", 0, author.ID)
	if withBy.CreatedBy != author.ID || withBy.CreatedAt.IsZero() {
		t.Errorf("created_by/at round-trip: %+v", withBy)
	}
	mine := mk("my-items", "My items", "assignee=me", 1, "")
	if mine.CreatedBy != "" {
		t.Errorf("null created_by should read as empty, got %q", mine.CreatedBy)
	}
	rel := mk("milestones", "Milestones", "mode=milestone", 2, "")

	// Ordered by position.
	list, err := pg.BoardViewsByBoard(ctx, bd.ID)
	if err != nil || len(list) != 3 || list[0].Slug != "all-items" || list[2].Slug != "milestones" {
		t.Fatalf("list by position = %v (err %v)", list, err)
	}

	// Unique (board_id, slug) is enforced.
	if _, err := pg.CreateBoardView(ctx, store.BoardView{WorkspaceID: ws.ID, BoardID: bd.ID, Slug: "all-items", Name: "dup", Position: 9}); err == nil {
		t.Error("duplicate slug on a board should be rejected")
	}

	// Rename.
	if err := pg.RenameBoardView(ctx, withBy.ID, "Everything"); err != nil {
		t.Fatal(err)
	}
	if got, _ := pg.BoardViewByID(ctx, withBy.ID); got.Name != "Everything" {
		t.Errorf("rename: name = %q", got.Name)
	}

	// Update query (save-to-view).
	if err := pg.UpdateBoardViewQuery(ctx, withBy.ID, "assignee=me"); err != nil {
		t.Fatal(err)
	}
	if got, _ := pg.BoardViewByID(ctx, withBy.ID); got.Query != "assignee=me" {
		t.Errorf("update query = %q", got.Query)
	}
	if err := pg.UpdateBoardViewQuery(ctx, "nope", ""); !errors.Is(err, store.ErrBoardViewNotFound) {
		t.Errorf("update unknown = %v, want ErrBoardViewNotFound", err)
	}

	// Atomic reorder: reverse the strip.
	if err := pg.ReorderBoardViews(ctx, bd.ID, []string{rel.ID, mine.ID, withBy.ID}); err != nil {
		t.Fatal(err)
	}
	after, _ := pg.BoardViewsByBoard(ctx, bd.ID)
	if after[0].ID != rel.ID || after[2].ID != withBy.ID {
		t.Fatalf("reorder produced %v", after)
	}

	// Delete + not-found.
	if err := pg.DeleteBoardView(ctx, mine.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.BoardViewByID(ctx, mine.ID); !errors.Is(err, store.ErrBoardViewNotFound) {
		t.Errorf("deleted view lookup = %v, want ErrBoardViewNotFound", err)
	}
	if err := pg.DeleteBoardView(ctx, "nope"); !errors.Is(err, store.ErrBoardViewNotFound) {
		t.Errorf("delete unknown = %v, want ErrBoardViewNotFound", err)
	}
}
