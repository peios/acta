package web

import (
	"testing"
	"time"

	"github.com/peios/acta/internal/store"
)

func TestBoardFilterStatusVisibility(t *testing.T) {
	// Empty set imposes no constraint.
	if !newBoardFilter(nil, nil, "me").statusVisible("anything") {
		t.Fatal("empty status filter should show all")
	}
	f := newBoardFilter([]string{"s1", ""}, nil, "me") // blank value ignored
	if !f.statusVisible("s1") {
		t.Fatal("s1 selected should be visible")
	}
	if f.statusVisible("s2") {
		t.Fatal("s2 not selected should be hidden")
	}
}

func TestBoardFilterAssigneeVisibility(t *testing.T) {
	f := newBoardFilter(nil, []string{"me", "unassigned", "u2"}, "uMe")
	cases := []struct {
		assignee string
		want     bool
	}{
		{"", true},    // unassigned selected
		{"uMe", true}, // me token resolves to uMe
		{"u2", true},  // explicit id
		{"u9", false}, // not selected
	}
	for _, c := range cases {
		if got := f.assigneeVisible(c.assignee); got != c.want {
			t.Errorf("assigneeVisible(%q) = %v, want %v", c.assignee, got, c.want)
		}
	}
	// With nothing selected, everyone is visible.
	none := newBoardFilter(nil, nil, "uMe")
	if !none.assigneeVisible("") || !none.assigneeVisible("anyone") {
		t.Fatal("empty assignee filter should show all")
	}
	// "me" must not match the empty (unassigned) id.
	meOnly := newBoardFilter(nil, []string{"me"}, "uMe")
	if meOnly.assigneeVisible("") {
		t.Fatal("me filter should not match unassigned cards")
	}
}

func TestCardHiddenCombinesFacets(t *testing.T) {
	f := newBoardFilter([]string{"s1"}, []string{"me"}, "uMe")
	shown := store.Item{StatusID: "s1", AssigneeID: "uMe"}
	if f.cardHidden(shown) {
		t.Fatal("card matching both facets should be visible")
	}
	wrongStatus := store.Item{StatusID: "s2", AssigneeID: "uMe"}
	wrongAssignee := store.Item{StatusID: "s1", AssigneeID: "other"}
	if !f.cardHidden(wrongStatus) || !f.cardHidden(wrongAssignee) {
		t.Fatal("a card failing either facet should be hidden")
	}
}

func TestAssigneeFacetHierarchy(t *testing.T) {
	now := time.Now()
	users := []store.User{
		{ID: "h2", Display: "Bob"},
		{ID: "h1", Display: "Alice"},
		{ID: "a1", Display: "alice-bot", AgentOfID: "h1"},
		{ID: "hX", Display: "Gone", DisabledAt: &now},                      // disabled human excluded
		{ID: "aX", Display: "dead-bot", AgentOfID: "h1", DisabledAt: &now}, // disabled agent excluded
	}
	f := newBoardFilter(nil, []string{"me", "h1"}, "h2")
	fac := assigneeFacetFrom(users, f)

	if !fac.MeSelected || fac.UnassignedSelected {
		t.Fatalf("token selection wrong: me=%v unassigned=%v", fac.MeSelected, fac.UnassignedSelected)
	}
	if len(fac.People) != 2 {
		t.Fatalf("want 2 active humans, got %d", len(fac.People))
	}
	if fac.People[0].Display != "Alice" || fac.People[1].Display != "Bob" {
		t.Fatalf("humans not sorted by display: %q, %q", fac.People[0].Display, fac.People[1].Display)
	}
	if !fac.People[0].Selected {
		t.Fatal("Alice (h1) should be selected")
	}
	if !fac.People[1].IsYou {
		t.Fatal("Bob (h2) should be flagged as you")
	}
	if len(fac.People[0].Agents) != 1 || fac.People[0].Agents[0].Display != "alice-bot" {
		t.Fatalf("Alice should have exactly one active agent, got %+v", fac.People[0].Agents)
	}
}
