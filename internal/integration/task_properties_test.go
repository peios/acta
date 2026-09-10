package integration

import (
	"acta2/internal/accounts"
	"acta2/internal/tasks"
	"errors"
	"slices"
	"testing"
)

func TestTaskProperties(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "properties")
	m, ctx := manager(f), t.Context()
	parent, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Parent"})
	must(t, e)
	if parent.Priority != "none" || parent.Type != "none" || parent.Size != "none" {
		t.Fatal(parent)
	}
	child, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Child", ParentID: parent.ID, Priority: "high", Type: "bug", Size: "xl"})
	must(t, e)
	old := parent
	parent, e = patchTask(t, f, parent, "priority", "high")
	must(t, e)
	_, e = patchTask(t, f, old, "priority", "urgent")
	var conflict *tasks.Conflict
	if !errors.As(e, &conflict) || conflict.Value != "high" || conflict.Version != 2 {
		t.Fatal(e)
	}
	retry, e := patchTask(t, f, old, "priority", "high")
	must(t, e)
	if retry.Versions["priority"] != 2 {
		t.Fatal(retry)
	}
	parent, e = patchTask(t, f, old, "type", "bug")
	must(t, e)
	if parent.Priority != "high" || parent.Size != "none" {
		t.Fatal("independent fields or size rollup", parent)
	}
	for _, filter := range []tasks.Filter{{Priorities: []string{"high"}, Types: []string{"bug"}}, {Group: "priority", GroupID: "high"}, {Group: "type", GroupID: "bug"}, {Group: "size", GroupID: "none"}} {
		filter.State = "all"
		p, e := m.Tasks(ctx, f.token, w.ID, filter)
		must(t, e)
		if p.Total != 1 || p.Tasks[0].ID != parent.ID {
			t.Fatal("root filtering", filter, p)
		}
	}
	p, e := m.Tasks(ctx, f.token, w.ID, tasks.Filter{State: "all", Priorities: []string{"high"}, Sizes: []string{"xl"}})
	must(t, e)
	if p.Total != 0 {
		t.Fatal("child promoted", p)
	}
	p, e = m.Tasks(ctx, f.token, w.ID, tasks.Filter{State: "all", Parent: parent.ID, Sizes: []string{"xl"}})
	must(t, e)
	if p.Total != 1 || p.Tasks[0].ID != child.ID {
		t.Fatal(p)
	}
	for _, name := range []string{"priority", "type", "size"} {
		_, e = patchTask(t, f, parent, name, "invalid")
		var field *accounts.FieldError
		if !errors.As(e, &field) {
			t.Fatal("bad enum", e)
		}
		groups, e := m.TaskGroups(ctx, f.token, w.ID, name)
		must(t, e)
		if len(groups) != len(tasks.PropertyOptions(name)) {
			t.Fatal(groups)
		}
	}
	_, e = m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Invalid", Size: "hours"})
	if e == nil {
		t.Fatal("invalid create")
	}
	history, e := m.TaskActivity(ctx, f.token, parent.ID, "")
	must(t, e)
	found := false
	for _, v := range history.Entries {
		if v.Field == "priority" {
			found = v.Before.Text == "None" && v.After.Text == "High"
		}
	}
	if !found {
		t.Fatal("activity missing metadata", history)
	}
	parent, e = patchTask(t, f, parent, "priority", "")
	must(t, e)
	if parent.Priority != "none" {
		t.Fatal("clear", parent)
	}
	view, e := m.CreateTaskView(ctx, f.token, w.ID, "Metadata", tasks.ViewSettings{Filters: tasks.ViewFilters{Priorities: []string{"high", "none", "high"}, Types: []string{"bug"}, Sizes: []string{"s", "m"}}, Display: tasks.ViewDisplay{Group: "priority", Sort: "size", Columns: []string{"priority", "type", "size"}}})
	must(t, e)
	views, e := m.TaskViews(ctx, f.token, w.ID)
	must(t, e)
	found = false
	for _, v := range views {
		if v.ID == view.ID {
			found = slices.Equal(v.Filters.Priorities, []string{"high", "none"}) && v.Display.Group == "priority" && v.Display.Sort == "size"
		}
	}
	if !found {
		t.Fatal("preset roundtrip", views)
	}
}
