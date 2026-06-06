package web

import (
	"testing"

	"github.com/peios/acta/internal/store"
)

func TestBuildCardAssigneeAvatar(t *testing.T) {
	st := store.Status{ID: "s1", Name: "Todo"}
	users := map[string]store.User{
		"u1": {ID: "u1", Display: "Jack Palfrey"},
		"a1": {ID: "a1", Display: "Claude", AgentOfID: "u1"},
	}

	// assigned to a human -> avatar with initials + a colour, not an agent
	cv := buildCard(store.Item{ID: "i1", RefNum: 7, StatusID: "s1", AssigneeID: "u1"}, nil, st, boardFilter{}, users, nil, nil, "ACME", "")
	if !cv.HasAssignee {
		t.Fatal("want HasAssignee for an assigned item")
	}
	if cv.RefID != "ACME-7" {
		t.Errorf("RefID: want ACME-7, got %q", cv.RefID)
	}
	if cv.AvatarText != "JP" {
		t.Errorf("initials: want JP, got %q", cv.AvatarText)
	}
	if cv.AvatarStyle == "" {
		t.Error("want a non-empty avatar style")
	}
	if cv.IsAgent {
		t.Error("a human assignee is not an agent")
	}

	// assigned to an agent -> IsAgent true (square avatar)
	if cvA := buildCard(store.Item{ID: "i2", StatusID: "s1", AssigneeID: "a1"}, nil, st, boardFilter{}, users, nil, nil, "ACME", ""); !cvA.IsAgent {
		t.Error("want IsAgent for an agent assignee")
	}

	// unassigned -> no avatar
	if cv0 := buildCard(store.Item{ID: "i3", StatusID: "s1"}, nil, st, boardFilter{}, users, nil, nil, "ACME", ""); cv0.HasAssignee {
		t.Error("unassigned item must not have an avatar")
	}

	// assignee not in the users map -> no avatar (don't render a broken chip)
	if cvX := buildCard(store.Item{ID: "i4", StatusID: "s1", AssigneeID: "ghost"}, nil, st, boardFilter{}, users, nil, nil, "ACME", ""); cvX.HasAssignee {
		t.Error("unresolvable assignee must not have an avatar")
	}
}

func TestInitials(t *testing.T) {
	cases := map[string]string{"Jack Palfrey": "JP", "Claude": "CL", "x": "X", "  Ann  Marie  Lee ": "AL", "": "?"}
	for in, want := range cases {
		if got := initials(in); got != want {
			t.Errorf("initials(%q): want %q, got %q", in, want, got)
		}
	}
}
