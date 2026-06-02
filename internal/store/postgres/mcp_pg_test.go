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
