package tasks

import (
	"cmp"
	"github.com/google/uuid"
	"slices"
	"strings"
)

// Group identifies a query bucket and, separately, the direct assignment used
// when creating or moving into it. Unavailable retained assignments remain visible.
type Group struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AssignID  string `json:"assign_id"`
	Available bool   `json:"available"`
}

func ValidateGroupFilter(f Filter) error {
	if f.Group == "" && f.GroupID == "" {
		return nil
	}
	if IsProperty(f.Group) {
		if f.GroupID == "" {
			return field("group_id", "Choose a group.")
		}
		_, e := PropertyValue(f.Group, f.GroupID)
		return e
	}
	if f.Group != "assignee" && f.Group != "agents" {
		return field("group", "Choose assignee, agents, priority, type or size grouping.")
	}
	if f.GroupID != "unassigned" {
		if _, e := uuid.Parse(f.GroupID); e != nil {
			return field("group_id", "Choose a group.")
		}
	}
	return nil
}

func AssignmentGroups(mode, owner string, people []Person) []Group {
	out := []Group{{ID: "unassigned", Name: "Unassigned", Available: true}}
	var rest []Group
	for _, p := range people {
		if mode == "assignee" && p.Agent {
			continue
		}
		if mode == "agents" && p.ID != owner && p.OwnerID != owner {
			continue
		}
		name := p.Username
		if p.DisplayName != nil && *p.DisplayName != "" {
			name = *p.DisplayName
		}
		g := Group{ID: p.ID, Name: name, AssignID: p.ID, Available: p.Available}
		if mode == "agents" && p.ID == owner {
			g.Name = "Assigned to me"
			out = append(out, g)
		} else {
			rest = append(rest, g)
		}
	}
	slices.SortFunc(rest, func(a, b Group) int {
		if n := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); n != 0 {
			return n
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return append(out, rest...)
}
