package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/peios/acta/internal/store"
)

// openTestDB connects to the Postgres named by ACTA_TEST_DATABASE_URL and runs
// migrations, skipping the test entirely when no such database is configured.
// The mcp_config migration and its jsonb/conflict behaviour have no pure-Go
// equivalent, so this is the only place they're exercised against real SQL.
func openTestDB(t *testing.T) *Postgres {
	t.Helper()
	url := os.Getenv("ACTA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set ACTA_TEST_DATABASE_URL to run Postgres-backed tests")
	}
	ctx := context.Background()
	pg, err := Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pg.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(pg.Close)
	return pg
}

func TestPGAppSetting(t *testing.T) {
	pg := openTestDB(t)
	ctx := context.Background()

	if v, err := pg.AppSetting(ctx, "mcp.guide"); err != nil || v != "" {
		t.Fatalf("absent setting: %q %v", v, err)
	}
	if err := pg.SetAppSetting(ctx, "mcp.guide", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := pg.SetAppSetting(ctx, "mcp.guide", "world"); err != nil {
		t.Fatal(err) // upsert
	}
	if v, _ := pg.AppSetting(ctx, "mcp.guide"); v != "world" {
		t.Fatalf("want upserted value, got %q", v)
	}
}

func TestPGMCPPromptCRUD(t *testing.T) {
	pg := openTestDB(t)
	ctx := context.Background()

	created, err := pg.CreateMCPPrompt(ctx, store.MCPPrompt{
		Name:        "pgtest_standup",
		Title:       "Standup",
		Description: "desc",
		Body:        "Hi {{workspace}}",
		Arguments:   []store.MCPPromptArg{{Name: "workspace", Description: "board", Required: true}},
		Position:    3,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("create did not assign an id")
	}

	// jsonb arguments round-trip.
	got, err := pg.MCPPromptByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Arguments) != 1 || got.Arguments[0].Name != "workspace" || !got.Arguments[0].Required {
		t.Fatalf("arguments didn't round-trip: %+v", got.Arguments)
	}

	// Duplicate name maps to the typed conflict error.
	_, err = pg.CreateMCPPrompt(ctx, store.MCPPrompt{Name: "pgtest_standup", Body: "x"})
	if !errors.Is(err, store.ErrMCPPromptNameTaken) {
		t.Fatalf("want ErrMCPPromptNameTaken, got %v", err)
	}

	// Update replaces mutable fields.
	got.Title = "Renamed"
	got.Arguments = nil
	if err := pg.UpdateMCPPrompt(ctx, got); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := pg.MCPPromptByID(ctx, created.ID)
	if reloaded.Title != "Renamed" || len(reloaded.Arguments) != 0 {
		t.Fatalf("update didn't persist: %+v", reloaded)
	}

	// List is ordered; delete removes it.
	if list, err := pg.ListMCPPrompts(ctx); err != nil || len(list) == 0 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
	if err := pg.DeleteMCPPrompt(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if err := pg.DeleteMCPPrompt(ctx, created.ID); !errors.Is(err, store.ErrMCPPromptNotFound) {
		t.Fatalf("want not-found on second delete, got %v", err)
	}
}

func TestPGEvents(t *testing.T) {
	pg := openTestDB(t)
	ctx := context.Background()

	rec, err := pg.RecordEvent(ctx, store.Event{
		WorkspaceID: "ws1",
		ItemID:      "it1",
		ItemTitle:   "Wire the log",
		ActorID:     "u1",
		ActorName:   "Ada",
		Verb:        store.EventItemStatusChange,
		Data:        map[string]string{"from": "To do", "to": "Doing"},
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if rec.ID == "" || rec.CreatedAt.IsZero() {
		t.Fatalf("record didn't populate id/created_at: %+v", rec)
	}

	// A nil-data event must round-trip as {} rather than failing or yielding null.
	if _, err := pg.RecordEvent(ctx, store.Event{
		WorkspaceID: "ws1", ItemID: "it1", Verb: store.EventItemArchived,
	}); err != nil {
		t.Fatalf("record nil-data: %v", err)
	}

	byItem, err := pg.EventsByItem(ctx, "it1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(byItem) != 2 {
		t.Fatalf("EventsByItem: want 2, got %d", len(byItem))
	}
	// Newest first: the archive event we wrote second.
	if byItem[0].Verb != store.EventItemArchived {
		t.Fatalf("want newest-first ordering, got %q first", byItem[0].Verb)
	}
	if byItem[0].Data == nil {
		t.Fatalf("nil-data event should decode to a non-nil empty map")
	}
	// The jsonb payload round-trips.
	sc := byItem[1]
	if sc.Data["from"] != "To do" || sc.Data["to"] != "Doing" {
		t.Fatalf("jsonb data didn't round-trip: %+v", sc.Data)
	}

	byWs, err := pg.EventsByWorkspace(ctx, "ws1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(byWs) != 2 {
		t.Fatalf("EventsByWorkspace: want 2, got %d", len(byWs))
	}
}

// Exercises the 0015 migration (the ms_position column + its backfill applies
// via openTestDB→Migrate) and the is_milestone-guarded ReorderMilestones SQL.
func TestPGMilestoneReorder(t *testing.T) {
	pg := openTestDB(t)
	ctx := context.Background()

	ws, err := pg.CreateWorkspace(ctx, store.Workspace{Slug: "ms-reorder", Name: "MS Reorder"})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	t.Cleanup(func() { _ = pg.DeleteWorkspace(ctx, ws.ID) })
	bd, err := pg.CreateBoard(ctx, store.Board{WorkspaceID: ws.ID, Name: "Tasks", Slug: "tasks", Position: 0})
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	st, err := pg.CreateStatus(ctx, store.Status{WorkspaceID: ws.ID, BoardID: bd.ID, Name: "To do"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	mk := func(title string, milestone bool) store.Item {
		it, err := pg.CreateItem(ctx, store.Item{WorkspaceID: ws.ID, StatusID: st.ID, Title: title})
		if err != nil {
			t.Fatalf("item %s: %v", title, err)
		}
		if milestone {
			if err := pg.SetItemMilestone(ctx, it.ID, true); err != nil {
				t.Fatalf("flag %s: %v", title, err)
			}
		}
		return it
	}
	a := mk("A", true)
	b := mk("B", true)
	plain := mk("plain", false) // a non-milestone the reorder must ignore

	if err := pg.ReorderMilestones(ctx, ws.ID, []string{b.ID, a.ID, plain.ID}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	pos := func(id string) int {
		it, err := pg.ItemByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return it.MSPosition
	}
	if pos(b.ID) != 0 || pos(a.ID) != 1 {
		t.Fatalf("milestones not renumbered: B=%d A=%d", pos(b.ID), pos(a.ID))
	}
	// The guard kept the non-milestone at its default despite being passed in.
	if pos(plain.ID) != 0 {
		t.Fatalf("non-milestone ms_position should stay 0, got %d", pos(plain.ID))
	}
}
