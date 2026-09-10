package integration

import (
	"acta2/internal/tasks"
	"testing"
)

func TestTaskAssignmentGroups(t *testing.T) {
	root := securityDatabase(t)
	owner, f, w := workspaceOwner(t, root, "grouping")
	ctx, m := t.Context(), manager(f)
	agent := agentAccount(t, f, "reviewer", nil)
	other, otherSession := permissionMember(t, root, "other")
	w = joinWorkspace(t, f, w, other.ID)
	otherAgent := agentAccount(t, otherSession, "builder", nil)
	for i := 0; i < 57; i++ {
		_, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Shared", Assignees: []string{owner.ID, agent.ID, otherAgent.ID}})
		must(t, e)
	}
	_, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Empty"})
	must(t, e)
	_, e = m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Other only", Assignees: []string{other.ID}})
	must(t, e)
	for _, tc := range []struct {
		mode, id string
		count    int64
	}{
		{"assignee", owner.ID, 57}, {"assignee", other.ID, 58},
		{"agents", owner.ID, 57}, {"agents", agent.ID, 57},
		{"agents", otherAgent.ID, 0}, {"agents", other.ID, 0},
		{"agents", "unassigned", 1}, {"assignee", "unassigned", 1},
	} {
		filter := tasks.Filter{State: "all", Group: tc.mode, GroupID: tc.id}
		page, e := m.Tasks(ctx, f.token, w.ID, filter)
		must(t, e)
		if page.Total != tc.count {
			t.Fatalf("%+v: got %d", tc, page.Total)
		}
		if tc.count > 50 {
			if len(page.Tasks) != 50 || !page.More {
				t.Fatal("incorrect pagination", page)
			}
			filter.Cursor = page.Cursor
			next, e := m.Tasks(ctx, f.token, w.ID, filter)
			must(t, e)
			if next.Total != tc.count || int64(len(next.Tasks)) != tc.count-50 {
				t.Fatal(next)
			}
		}
	}
	// Lane membership intersects rather than replaces the current filters.
	page, e := m.Tasks(ctx, f.token, w.ID, tasks.Filter{State: "all", Group: "agents", GroupID: owner.ID, Assignees: []string{agent.ID}})
	must(t, e)
	if page.Total != 57 {
		t.Fatal(page)
	}
	page, e = m.Tasks(ctx, f.token, w.ID, tasks.Filter{State: "all", Group: "agents", GroupID: owner.ID, Unassigned: true})
	must(t, e)
	if page.Total != 0 {
		t.Fatal(page)
	}
	groups, e := m.TaskGroups(ctx, f.token, w.ID, "agents")
	must(t, e)
	if len(groups) != 3 || groups[0].ID != "unassigned" || groups[1].ID != owner.ID || groups[2].ID != agent.ID {
		t.Fatal(groups)
	}
	groups, e = m.TaskGroups(ctx, f.token, w.ID, "assignee")
	must(t, e)
	found := false
	for _, g := range groups {
		if g.ID == other.ID {
			found = true
		}
		if g.ID == agent.ID || g.ID == otherAgent.ID {
			t.Fatal("agent exposed as human group")
		}
	}
	if !found {
		t.Fatal("missing owner roll-up")
	}
	// Membership removal must not hide existing assignments or offer them as destinations.
	must(t, m.WorkspaceMembership(ctx, f.token, w.ID, other.ID, w.Version, false))
	groups, e = m.TaskGroups(ctx, f.token, w.ID, "assignee")
	must(t, e)
	found = false
	for _, g := range groups {
		if g.ID == other.ID {
			found = true
			if g.Available {
				t.Fatal("removed member is assignable")
			}
		}
	}
	if !found {
		t.Fatal("retained assignment hidden")
	}
	if _, e = manager(otherSession).TaskGroups(ctx, otherSession.token, w.ID, "assignee"); e == nil {
		t.Fatal("groups exposed outside workspace")
	}
	// Both modes persist as personal view display settings.
	for _, mode := range []string{"assignee", "agents"} {
		view, e := m.CreateTaskView(ctx, f.token, w.ID, mode, tasks.ViewSettings{Display: tasks.ViewDisplay{Group: mode}})
		must(t, e)
		if view.Display.Group != mode {
			t.Fatal(view)
		}
	}
}
