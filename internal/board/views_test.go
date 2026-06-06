package board_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
)

// TestSeedDefaultViews proves a freshly-seeded board carries the five default
// views, in order, with the slugs/queries the migration also backfills.
func TestSeedDefaultViews(t *testing.T) {
	svc, wsID, _ := setup(t)
	ctx := context.Background()
	tasks, err := svc.DefaultBoard(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	views, err := svc.BoardViews(ctx, tasks.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ slug, name, query string }{
		{"all-items", "All items", ""},
		{"my-items", "My items", "assignee=me"},
		{"current-release", "Current Release", "release=active"},
		{"milestones", "Milestones", "mode=milestone"},
		{"releases", "Releases", "mode=release"},
	}
	if len(views) != len(want) {
		t.Fatalf("want %d seeded views, got %d", len(want), len(views))
	}
	for i, w := range want {
		if views[i].Slug != w.slug || views[i].Name != w.name || views[i].Query != w.query || views[i].Position != i {
			t.Errorf("view %d = %+v, want slug=%s name=%s query=%q pos=%d", i, views[i], w.slug, w.name, w.query, i)
		}
	}
	// Backlog is seeded too — every board gets the defaults.
	bl, err := svc.BoardBySlug(ctx, wsID, "backlog")
	if err != nil {
		t.Fatal(err)
	}
	if bv, err := svc.BoardViews(ctx, bl.ID); err != nil || len(bv) != len(want) {
		t.Fatalf("backlog views = %d (err %v), want %d", len(bv), err, len(want))
	}
}

func TestNormalizeViewQuery(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"mode=status", ""}, // status is the default — dropped
		{"mode=milestone", "mode=milestone"},
		{"mode=release", "mode=release"},
		{"mode=bogus", ""}, // unknown mode dropped
		{"assignee=me", "assignee=me"},
		{"release=active", "release=active"},
		{"status=b&status=a", "status=a&status=b"},                         // sorted
		{"status=a&status=a", "status=a"},                                  // deduped
		{"project=p1&assignee=me", "assignee=me&project=p1"},               // keys sorted
		{"assignee=me&item=xyz&board=tasks", "assignee=me"},                // junk dropped
		{"q=+hello+", "q=hello"},                                           // trimmed
		{"priority=urgent&priority=high", "priority=high&priority=urgent"}, // attr facet, sorted
		{"type=bug", "type=bug"},
		{"size=m&size=m", "size=m"}, // attr facet deduped
		{"due=overdue", "due=overdue"},
		{"due=bogus", ""}, // only "overdue" is a valid due token
		{"priority=high&type=bug&size=l&due=overdue", "due=overdue&priority=high&size=l&type=bug"}, // keys sorted
	}
	for _, c := range cases {
		in, _ := url.ParseQuery(c.in)
		if got := board.NormalizeViewQuery(in); got != c.want {
			t.Errorf("NormalizeViewQuery(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestViewQueryHiddenByReleases(t *testing.T) {
	cases := []struct {
		query                        string
		hasReleases, hasActive, want bool
	}{
		{"release=active", true, true, false},
		{"release=active", true, false, true},  // needs active, none active
		{"release=active", false, false, true}, // no releases at all
		{"mode=release", true, false, false},   // releases exist, doesn't need active
		{"mode=release", false, false, true},   // no releases
		{"release=r123", true, false, false},   // specific release, releases exist
		{"release=r123", false, false, true},   // specific release, none exist
		{"", true, true, false},                // not release-oriented
		{"assignee=me", false, false, false},   // not release-oriented
	}
	for _, c := range cases {
		if got := board.ViewQueryHiddenByReleases(c.query, c.hasReleases, c.hasActive); got != c.want {
			t.Errorf("ViewQueryHiddenByReleases(%q, %v, %v) = %v, want %v", c.query, c.hasReleases, c.hasActive, got, c.want)
		}
	}
}

func TestCreateBoardView(t *testing.T) {
	svc, wsID, _ := setup(t)
	ctx := context.Background()
	tasks, _ := svc.DefaultBoard(ctx, wsID)

	// The raw query carries junk (item=, mode=status) that normalisation strips.
	v, err := svc.CreateBoardView(ctx, tasks.ID, "My Bugs", "?status=x&item=open&mode=status", "user1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Slug != "my-bugs" || v.Query != "status=x" || v.Icon != "filter" || v.CreatedBy != "user1" {
		t.Fatalf("create = %+v", v)
	}
	if v.Position != 5 { // appended after the five seeded defaults
		t.Errorf("position = %d, want 5", v.Position)
	}

	// A second view with the same name gets a disambiguated slug.
	v2, err := svc.CreateBoardView(ctx, tasks.ID, "My Bugs", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if v2.Slug != "my-bugs-2" {
		t.Errorf("dup slug = %q, want my-bugs-2", v2.Slug)
	}

	// An empty/whitespace name is rejected.
	if _, err := svc.CreateBoardView(ctx, tasks.ID, "  ", "", ""); err == nil {
		t.Error("blank name should be rejected")
	}
}

func TestUpdateBoardViewQuery(t *testing.T) {
	svc, wsID, _ := setup(t)
	ctx := context.Background()
	tasks, _ := svc.DefaultBoard(ctx, wsID)
	views, _ := svc.BoardViews(ctx, tasks.ID)

	// Overwrite All items (query "") with a filter; the raw query is normalised.
	if err := svc.UpdateBoardViewQuery(ctx, views[0].ID, "?assignee=me&item=junk&mode=status"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.BoardView(ctx, views[0].ID)
	if got.Query != "assignee=me" {
		t.Errorf("updated query = %q, want assignee=me", got.Query)
	}
	if err := svc.UpdateBoardViewQuery(ctx, "nope", ""); !errors.Is(err, store.ErrBoardViewNotFound) {
		t.Errorf("update unknown = %v, want ErrBoardViewNotFound", err)
	}
}

func TestRenameDeleteReorderBoardView(t *testing.T) {
	svc, wsID, _ := setup(t)
	ctx := context.Background()
	tasks, _ := svc.DefaultBoard(ctx, wsID)
	views, _ := svc.BoardViews(ctx, tasks.ID)

	// Rename a default — defaults aren't protected.
	if err := svc.RenameBoardView(ctx, views[0].ID, "Everything"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.BoardView(ctx, views[0].ID)
	if got.Name != "Everything" {
		t.Errorf("rename: name = %q", got.Name)
	}

	// Reorder: reverse the strip and confirm positions follow.
	ids := make([]string, len(views))
	for i, v := range views {
		ids[len(views)-1-i] = v.ID
	}
	if err := svc.ReorderBoardViews(ctx, tasks.ID, ids); err != nil {
		t.Fatal(err)
	}
	reordered, _ := svc.BoardViews(ctx, tasks.ID)
	for i, id := range ids {
		if reordered[i].ID != id {
			t.Fatalf("reorder[%d] = %s, want %s", i, reordered[i].ID, id)
		}
	}

	// Delete a default — count drops.
	if err := svc.DeleteBoardView(ctx, views[2].ID); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.BoardViews(ctx, tasks.ID)
	if len(after) != len(views)-1 {
		t.Fatalf("after delete = %d, want %d", len(after), len(views)-1)
	}
}
