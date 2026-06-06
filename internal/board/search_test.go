package board_test

import (
	"context"
	"testing"

	"github.com/peios/acta/internal/store"
)

func searchTitles(items []store.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}

func TestSearchItemsMatchesTitleAndDescription(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	todo := statuses[0].ID

	a, _ := svc.CreateItem(ctx, wsID, todo, "loregd device-wiring")
	b, _ := svc.CreateItem(ctx, wsID, todo, "unrelated card")
	if err := svc.UpdateDescription(ctx, b.ID, "notes on the registry daemon"); err != nil {
		t.Fatal(err)
	}
	svc.CreateItem(ctx, wsID, todo, "nothing to see here")

	cases := []struct {
		name, q, want string // want = matched item id, "" for none
	}{
		{"title substring", "loregd", a.ID},
		{"description substring", "registry", b.ID},
		{"case-insensitive", "LOREGD", a.ID},
		{"no match", "zzz-nope", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := svc.SearchItems(ctx, wsID, "",c.q, false)
			if err != nil {
				t.Fatal(err)
			}
			if c.want == "" {
				if len(got) != 0 {
					t.Fatalf("want no hits, got %v", searchTitles(got))
				}
				return
			}
			if len(got) != 1 || got[0].ID != c.want {
				t.Fatalf("want one hit, got %v", searchTitles(got))
			}
		})
	}
}

func TestSearchItemsRanksTitleBeforeBody(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	todo := statuses[0].ID

	body, _ := svc.CreateItem(ctx, wsID, todo, "beta card")
	if err := svc.UpdateDescription(ctx, body.ID, "mentions kappa in the body"); err != nil {
		t.Fatal(err)
	}
	title, _ := svc.CreateItem(ctx, wsID, todo, "kappa headline")

	got, err := svc.SearchItems(ctx, wsID, "","kappa", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want both hits, got %v", searchTitles(got))
	}
	if got[0].ID != title.ID {
		t.Fatalf("a title match must rank above a body-only match; order = %v", searchTitles(got))
	}
}

// The search term is matched literally — LIKE wildcards in user input must not
// act as wildcards, or "%" would match every row.
func TestSearchItemsTreatsWildcardsLiterally(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	todo := statuses[0].ID

	pct, _ := svc.CreateItem(ctx, wsID, todo, "100% migrated")
	svc.CreateItem(ctx, wsID, todo, "plain card")
	und, _ := svc.CreateItem(ctx, wsID, todo, "a_b boundary")
	svc.CreateItem(ctx, wsID, todo, "axb other")

	if got, _ := svc.SearchItems(ctx, wsID, "","%", false); len(got) != 1 || got[0].ID != pct.ID {
		t.Fatalf(`"%%" must match a literal percent: got %v`, searchTitles(got))
	}
	if got, _ := svc.SearchItems(ctx, wsID, "","a_b", false); len(got) != 1 || got[0].ID != und.ID {
		t.Fatalf(`"a_b" must match a literal underscore, not "axb": got %v`, searchTitles(got))
	}
}

func TestSearchItemsArchivedGating(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	todo := statuses[0].ID

	live, _ := svc.CreateItem(ctx, wsID, todo, "widget live")
	gone, _ := svc.CreateItem(ctx, wsID, todo, "widget retired")
	if err := svc.Archive(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}

	if got, _ := svc.SearchItems(ctx, wsID, "","widget", false); len(got) != 1 || got[0].ID != live.ID {
		t.Fatalf("default search must skip archived: got %v", searchTitles(got))
	}
	if got, _ := svc.SearchItems(ctx, wsID, "","widget", true); len(got) != 2 {
		t.Fatalf("include-archived search: want 2, got %v", searchTitles(got))
	}
}

// A board id scopes the search; "" spans every board. This is the mechanism
// behind excluding Backlog from an unscoped search.
func TestSearchItemsScopedToBoard(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	tasksTodo := statuses[0].ID

	def, err := svc.DefaultBoard(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	boards, err := svc.Boards(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	var backlog store.Board
	for _, b := range boards {
		if b.ID != def.ID {
			backlog = b
			break
		}
	}
	if backlog.ID == "" {
		t.Fatal("expected a seeded Backlog board alongside the default")
	}
	blStatuses, err := svc.BoardStatuses(ctx, backlog.ID)
	if err != nil {
		t.Fatal(err)
	}

	onTasks, _ := svc.CreateItem(ctx, wsID, tasksTodo, "shared-term on tasks")
	onBacklog, _ := svc.CreateItem(ctx, wsID, blStatuses[0].ID, "shared-term on backlog")

	// Scoped to the default board → Backlog excluded (the unscoped-search default).
	if got, _ := svc.SearchItems(ctx, wsID, def.ID, "shared-term", false); len(got) != 1 || got[0].ID != onTasks.ID {
		t.Fatalf("default-board scope must exclude Backlog: got %v", searchTitles(got))
	}
	// Scoped to Backlog → only the Backlog item.
	if got, _ := svc.SearchItems(ctx, wsID, backlog.ID, "shared-term", false); len(got) != 1 || got[0].ID != onBacklog.ID {
		t.Fatalf("Backlog scope wrong: got %v", searchTitles(got))
	}
	// Unscoped ("") → both, the board=* case.
	if got, _ := svc.SearchItems(ctx, wsID, "", "shared-term", false); len(got) != 2 {
		t.Fatalf("all-board search: want 2, got %v", searchTitles(got))
	}
}

// Search transcends the board and nesting — a subtask is found like any item.
func TestSearchItemsSpansSubtasks(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	todo := statuses[0].ID

	parent, _ := svc.CreateItem(ctx, wsID, todo, "umbrella epic")
	child, _ := svc.CreateItem(ctx, wsID, todo, "buried gamma subtask")
	if err := svc.Reparent(ctx, child.ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.SearchItems(ctx, wsID, "","gamma", false); len(got) != 1 || got[0].ID != child.ID {
		t.Fatalf("search must reach subtasks: got %v", searchTitles(got))
	}
}
