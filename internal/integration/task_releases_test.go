package integration

import (
	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/tasks"
	ws "acta/internal/workspaces"
	"errors"
	"slices"
	"testing"
)

func TestTaskReleases(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "releases")
	m, ctx := manager(f), t.Context()
	for _, name := range []string{"2026.10", "2026.9", "Beta"} {
		_, e := m.CreateTaskRelease(ctx, f.token, w.ID, tasks.ReleaseCreate{Name: name})
		must(t, e)
	}
	if _, e := m.CreateTaskRelease(ctx, f.token, w.ID, tasks.ReleaseCreate{Name: "beta"}); e == nil {
		t.Fatal("case-insensitive duplicate name accepted")
	}
	rs, e := m.TaskReleases(ctx, f.token, w.ID)
	must(t, e)
	names := []string{}
	for _, r := range rs {
		names = append(names, r.Name)
	}
	if !slices.Equal(names, []string{"2026.9", "2026.10", "Beta"}) || rs[0].State != "planned" {
		t.Fatal("natural order", names, rs)
	}
	next, later := rs[0], rs[1]

	// Partial updates share one version; backwards transitions are allowed.
	open, frozen, planned, notes := "open", "frozen", "planned", "Ships the events transition."
	next, e = m.UpdateTaskRelease(ctx, f.token, w.ID, next.ID, tasks.ReleaseUpdate{State: &open, Description: &notes, Version: next.Version})
	must(t, e)
	if next.State != "open" || next.Description != notes || next.Version != 2 {
		t.Fatal("update", next)
	}
	if _, e = m.UpdateTaskRelease(ctx, f.token, w.ID, next.ID, tasks.ReleaseUpdate{State: &frozen, Version: 1}); !errors.Is(e, tasks.ErrReleaseConflict) {
		t.Fatal("stale release update", e)
	}
	if retry, e := m.UpdateTaskRelease(ctx, f.token, w.ID, next.ID, tasks.ReleaseUpdate{State: &open, Version: 1}); e != nil || retry.Version != 2 {
		t.Fatal("unchanged retry should be idempotent", retry, e)
	}
	next, e = m.UpdateTaskRelease(ctx, f.token, w.ID, next.ID, tasks.ReleaseUpdate{State: &frozen, Version: next.Version})
	must(t, e)
	next, e = m.UpdateTaskRelease(ctx, f.token, w.ID, next.ID, tasks.ReleaseUpdate{State: &planned, Version: next.Version})
	must(t, e)
	if next.State != "planned" {
		t.Fatal("backwards transition", next)
	}
	next, e = m.UpdateTaskRelease(ctx, f.token, w.ID, next.ID, tasks.ReleaseUpdate{State: &open, Version: next.Version})
	must(t, e)

	// Tasks target a release directly; subtasks do not inherit it.
	c, e := m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	parent, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Phase 1", ReleaseID: next.ID})
	must(t, e)
	if parent.ReleaseID != next.ID || parent.Release == nil || parent.Release.Name != "2026.9" || parent.Release.State != "open" {
		t.Fatal("create with release", parent)
	}
	child, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Phase 2", ParentID: parent.ID})
	must(t, e)
	if child.ReleaseID != "" || child.Release != nil {
		t.Fatal("release inherited", child)
	}
	done, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Finished", ParentID: parent.ID, StatusID: c.Completed, ReleaseID: next.ID})
	must(t, e)
	old := child
	child, e = patchTask(t, f, child, "release_id", later.ID)
	must(t, e)
	history, e := m.TaskActivity(ctx, f.token, child.ID, "")
	must(t, e)
	recorded := false
	for _, v := range history.Entries {
		if v.Field == "release_id" && len(v.Before.Items) == 0 && len(v.After.Items) == 1 && v.After.Items[0].Label == "2026.10" {
			recorded = true
		}
	}
	if !recorded {
		t.Fatal("activity", history)
	}
	_, e = patchTask(t, f, old, "release_id", next.ID)
	var conflict *tasks.Conflict
	if !errors.As(e, &conflict) || conflict.Value != later.ID || conflict.Version != 2 {
		t.Fatal("release conflict", e)
	}

	// A release from another workspace is rejected.
	_, otherOwner, other := workspaceOwner(t, root, "other-releases")
	foreign, e := manager(otherOwner).CreateTaskRelease(ctx, otherOwner.token, other.ID, tasks.ReleaseCreate{Name: "2026.9"})
	must(t, e)
	var field *accounts.FieldError
	if _, e = patchTask(t, f, child, "release_id", foreign.ID); !errors.As(e, &field) || field.Field != "release_id" {
		t.Fatal("foreign release accepted", e)
	}
	if _, e = m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Foreign", ReleaseID: foreign.ID}); !errors.As(e, &field) {
		t.Fatal("foreign release accepted on create", e)
	}

	// Release filters match one hierarchy level unless all_depths is set.
	count := func(filter tasks.Filter) int64 {
		t.Helper()
		filter.State = "all"
		p, e := m.Tasks(ctx, f.token, w.ID, filter)
		must(t, e)
		return p.Total
	}
	if n := count(tasks.Filter{Releases: []string{next.ID}}); n != 1 {
		t.Fatal("root release filter", n)
	}
	if n := count(tasks.Filter{Releases: []string{next.ID}, AllDepths: true}); n != 2 {
		t.Fatal("all-depth release filter", n)
	}
	if n := count(tasks.Filter{Releases: []string{later.ID, tasks.NoRelease}, AllDepths: true}); n != 1 {
		t.Fatal("none and other release", n)
	}
	if n := count(tasks.Filter{Group: "release", GroupID: next.ID}); n != 1 {
		t.Fatal("release group", n)
	}
	if _, e = m.Tasks(ctx, f.token, w.ID, tasks.Filter{AllDepths: true, Parent: parent.ID}); e == nil {
		t.Fatal("all_depths with parent accepted")
	}
	groups, e := m.TaskGroups(ctx, f.token, w.ID, "release")
	must(t, e)
	if len(groups) != 4 || groups[0].ID != tasks.NoRelease || groups[1].ID != next.ID {
		t.Fatal("release groups", groups)
	}

	// Progress counts active tasks targeted directly; archived tasks drop out.
	next, e = m.TaskRelease(ctx, f.token, w.ID, next.ID)
	must(t, e)
	if next.Total != 2 || next.Finished != 1 {
		t.Fatal("progress", next)
	}
	_, e = m.ArchiveTask(ctx, f.token, done.ID, tasks.Archive{Archived: true, Version: done.Versions["archived"]})
	must(t, e)
	next, e = m.TaskRelease(ctx, f.token, w.ID, next.ID)
	must(t, e)
	if next.Total != 1 || next.Finished != 0 {
		t.Fatal("archived progress", next)
	}

	// Inspection, search and activity carry the release.
	detail, e := m.InspectTask(ctx, f.token, parent.ID, "")
	must(t, e)
	if detail.Release == nil || len(detail.Subtasks.Tasks) != 1 || detail.Subtasks.Tasks[0].ID != child.ID || detail.Subtasks.Tasks[0].Release == nil || detail.Subtasks.Tasks[0].Release.ID != later.ID {
		t.Fatal("inspect", detail)
	}
	found, e := m.SearchTasks(ctx, f.token, tasks.SearchQuery{Query: parent.Reference})
	must(t, e)
	if len(found.Tasks) == 0 || found.Tasks[0].Release == nil || found.Tasks[0].Release.State != "open" {
		t.Fatal("search", found)
	}
	child, e = patchTask(t, f, child, "release_id", "")
	must(t, e)
	if child.ReleaseID != "" || child.Release != nil {
		t.Fatal("clear", child)
	}

	// Saved views keep release filters, grouping and the column.
	view, e := m.CreateTaskView(ctx, f.token, w.ID, "Next release", tasks.ViewSettings{Filters: tasks.ViewFilters{Releases: []string{next.ID, tasks.NoRelease, next.ID}}, Display: tasks.ViewDisplay{Group: "release", Columns: []string{"release", "status"}}})
	must(t, e)
	if !slices.Equal(view.Filters.Releases, []string{next.ID, tasks.NoRelease}) && !slices.Equal(view.Filters.Releases, []string{tasks.NoRelease, next.ID}) {
		t.Fatal("view filters", view.Filters)
	}
	if view.Display.Group != "release" || !slices.Equal(view.Display.Columns, []string{"status", "release"}) {
		t.Fatal("view display", view.Display)
	}

	// Managing releases needs its own permission; targeting needs Edit tasks.
	a, member := permissionMember(t, root, "editor")
	w = joinWorkspace(t, f, w, a.ID)
	w = workspaceGrants(t, f, w, a.ID, false, []string{ws.EditTasks, ws.ManageStatuses})
	if _, e = manager(member).CreateTaskRelease(ctx, member.token, w.ID, tasks.ReleaseCreate{Name: "Denied"}); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("release created without permission", e)
	}
	if _, e = manager(member).UpdateTaskRelease(ctx, member.token, w.ID, next.ID, tasks.ReleaseUpdate{State: &frozen, Version: next.Version}); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("release updated without permission", e)
	}
	child, e = patchTask(t, member, child, "release_id", next.ID)
	must(t, e)
	w = workspaceGrants(t, f, w, a.ID, false, []string{ws.ManageReleases})
	if _, e = manager(member).UpdateTaskRelease(ctx, member.token, w.ID, next.ID, tasks.ReleaseUpdate{State: &frozen, Version: next.Version}); e != nil {
		t.Fatal("release manager denied", e)
	}
}
