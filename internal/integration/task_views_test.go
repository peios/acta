package integration

import (
	"acta2/internal/auth"
	"acta2/internal/tasks"
	"errors"
	"sync"
	"testing"
)

func TestTaskViewsOwnershipAndVersions(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "views")
	ctx, m := t.Context(), manager(f)
	views, e := m.TaskViews(ctx, f.token, w.ID)
	must(t, e)
	if len(views) != 1 || views[0].Name != "All tasks" {
		t.Fatal(views)
	}
	initial := views[0]
	// Listing across browsers returns the same initial tab.
	again, e := m.TaskViews(ctx, f.token, w.ID)
	must(t, e)
	if len(again) != 1 || again[0].ID != initial.ID {
		t.Fatal(again)
	}
	custom, e := m.CreateTaskView(ctx, f.token, w.ID, "My work", tasks.ViewSettings{Filters: tasks.ViewFilters{}})
	must(t, e)
	c, e := m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	filters := tasks.ViewFilters{Statuses: []string{c.Completed, c.Creation, c.Completed}, Unassigned: true}
	branched, e := m.CreateTaskView(ctx, f.token, w.ID, "Current filters", tasks.ViewSettings{Filters: filters})
	must(t, e)
	if branched.Version != 1 || len(branched.Filters.Statuses) != 2 || !branched.Filters.Unassigned {
		t.Fatal("new tab did not save normalized filters at creation", branched)
	}
	if _, e = m.CreateTaskView(ctx, f.token, w.ID, "Invalid", tasks.ViewSettings{Filters: tasks.ViewFilters{Statuses: []string{"invalid"}}}); e == nil {
		t.Fatal("invalid creation filters accepted")
	}
	persisted, e := m.TaskViews(ctx, f.token, w.ID)
	must(t, e)
	if len(persisted) != 3 || persisted[2].ID != branched.ID || !persisted[2].Filters.Unassigned || len(persisted[2].Filters.Statuses) != 2 {
		t.Fatal("creation filters not persisted, or invalid creation left a tab", persisted)
	}
	if len(persisted[1].Filters.Statuses) != 0 || persisted[1].Filters.Unassigned || persisted[1].Version != custom.Version {
		t.Fatal("creating another view changed the original saved view", persisted[1])
	}
	saved, e := m.SaveTaskView(ctx, f.token, w.ID, custom.ID, custom.Version, tasks.ViewSettings{Filters: filters})
	must(t, e)
	if saved.Version != 2 || len(saved.Filters.Statuses) != 2 || !saved.Filters.Unassigned {
		t.Fatal(saved)
	}
	retry, e := m.SaveTaskView(ctx, f.token, w.ID, custom.ID, custom.Version, tasks.ViewSettings{Filters: filters})
	must(t, e)
	if retry.Version != saved.Version {
		t.Fatal("identical retry changed version")
	}
	_, e = m.SaveTaskView(ctx, f.token, w.ID, custom.ID, custom.Version, tasks.ViewSettings{Filters: tasks.ViewFilters{}})
	if !errors.Is(e, auth.ErrPermissionsChanged) {
		t.Fatal("stale save accepted", e)
	}
	if _, e = m.CreateTaskView(ctx, f.token, w.ID, "my WORK", tasks.ViewSettings{Filters: tasks.ViewFilters{}}); e == nil {
		t.Fatal("duplicate name accepted")
	}
	if _, e = m.CreateTaskView(ctx, f.token, w.ID, "  ", tasks.ViewSettings{Filters: tasks.ViewFilters{}}); e == nil {
		t.Fatal("empty name accepted")
	}
	if _, e = m.SaveTaskView(ctx, f.token, w.ID, custom.ID, saved.Version, tasks.ViewSettings{Filters: tasks.ViewFilters{Statuses: []string{"invalid"}}}); e == nil {
		t.Fatal("invalid filter accepted")
	}
	// Plain members may save their own preferences, but cannot read or overwrite others'.
	other, otherSession := permissionMember(t, root, "other-viewer")
	joinWorkspace(t, f, w, other.ID)
	theirs, e := manager(otherSession).TaskViews(ctx, otherSession.token, w.ID)
	must(t, e)
	if len(theirs) != 1 || theirs[0].ID == initial.ID {
		t.Fatal("views shared across users", theirs)
	}
	_, e = manager(otherSession).SaveTaskView(ctx, otherSession.token, w.ID, custom.ID, saved.Version, tasks.ViewSettings{Filters: tasks.ViewFilters{}})
	if !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("cross-account save", e)
	}
	_, e = manager(root).SaveTaskView(ctx, root.token, w.ID, custom.ID, saved.Version, tasks.ViewSettings{Filters: tasks.ViewFilters{}})
	if !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("superuser overwrote personal view", e)
	}
	_, outside := permissionMember(t, root, "outside-viewer")
	if _, e = manager(outside).TaskViews(ctx, outside.token, w.ID); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("private workspace views exposed", e)
	}
	own, e := manager(otherSession).SaveTaskView(ctx, otherSession.token, w.ID, theirs[0].ID, theirs[0].Version, tasks.ViewSettings{Filters: tasks.ViewFilters{Unassigned: true}})
	must(t, e)
	if !own.Filters.Unassigned {
		t.Fatal(own)
	}
	_, _, w2 := workspaceOwner(t, root, "other-views")
	// Root can access both workspaces, but IDs are still constrained to the requested workspace.
	rootViews, e := manager(root).TaskViews(ctx, root.token, w.ID)
	must(t, e)
	_, e = manager(root).SaveTaskView(ctx, root.token, w2.ID, rootViews[0].ID, rootViews[0].Version, tasks.ViewSettings{Filters: tasks.ViewFilters{Unassigned: true}})
	if !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("cross-workspace save", e)
	}
}
func TestTaskViewsConcurrentInitialization(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "view-race")
	var wg sync.WaitGroup
	results := make(chan []tasks.View, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := manager(f).TaskViews(t.Context(), f.token, w.ID)
			results <- v
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		must(t, e)
	}
	id := ""
	for v := range results {
		if len(v) != 1 {
			t.Fatal(v)
		}
		if id != "" && id != v[0].ID {
			t.Fatal("initial tab duplicated")
		}
		id = v[0].ID
	}
}

func TestTaskViewRenameAndDelete(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "view-edit")
	ctx, m := t.Context(), manager(f)
	initial, e := m.TaskViews(ctx, f.token, w.ID)
	must(t, e)
	custom, e := m.CreateTaskView(ctx, f.token, w.ID, "Original", tasks.ViewSettings{Filters: tasks.ViewFilters{Unassigned: true}})
	must(t, e)
	renamed, e := m.RenameTaskView(ctx, f.token, w.ID, custom.ID, custom.Version, "  Renamed  ")
	must(t, e)
	if renamed.Name != "Renamed" || renamed.Version != custom.Version+1 || !renamed.Filters.Unassigned {
		t.Fatal(renamed)
	}
	retry, e := m.RenameTaskView(ctx, f.token, w.ID, custom.ID, custom.Version, "Renamed")
	must(t, e)
	if retry.Version != renamed.Version {
		t.Fatal("rename retry changed version")
	}
	if _, e = m.RenameTaskView(ctx, f.token, w.ID, custom.ID, custom.Version, "Stale"); !errors.Is(e, auth.ErrPermissionsChanged) {
		t.Fatal("stale rename accepted", e)
	}
	if e = m.DeleteTaskView(ctx, f.token, w.ID, custom.ID, custom.Version); !errors.Is(e, auth.ErrPermissionsChanged) {
		t.Fatal("stale deletion accepted", e)
	}
	for _, name := range []string{"ALL TASKS", " "} {
		if _, e = m.RenameTaskView(ctx, f.token, w.ID, custom.ID, renamed.Version, name); e == nil {
			t.Fatal("invalid rename accepted", name)
		}
	}
	if _, e = manager(root).RenameTaskView(ctx, root.token, w.ID, custom.ID, renamed.Version, "Intrusion"); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("superuser renamed another account's view", e)
	}
	if e = manager(root).DeleteTaskView(ctx, root.token, w.ID, custom.ID, renamed.Version); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("superuser deleted another account's view", e)
	}
	must(t, m.DeleteTaskView(ctx, f.token, w.ID, initial[0].ID, initial[0].Version))
	remaining, e := m.TaskViews(ctx, f.token, w.ID)
	must(t, e)
	if len(remaining) != 1 || remaining[0].ID != custom.ID {
		t.Fatal("deleted initial view was recreated", remaining)
	}
	if e = m.DeleteTaskView(ctx, f.token, w.ID, custom.ID, renamed.Version); e == nil {
		t.Fatal("last view deleted")
	}
	// Two simultaneous deletes of different views must leave one surviving view.
	second, e := m.CreateTaskView(ctx, f.token, w.ID, "Second", tasks.ViewSettings{Filters: tasks.ViewFilters{}})
	must(t, e)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, view := range []tasks.View{renamed, second} {
		wg.Add(1)
		go func(v tasks.View) { defer wg.Done(); results <- m.DeleteTaskView(ctx, f.token, w.ID, v.ID, v.Version) }(view)
	}
	wg.Wait()
	close(results)
	succeeded := 0
	for e := range results {
		if e == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatal("concurrent deletion results", succeeded)
	}
	remaining, e = m.TaskViews(ctx, f.token, w.ID)
	must(t, e)
	if len(remaining) != 1 {
		t.Fatal("last view protection failed", remaining)
	}
}

func TestBoardPresetPersistence(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "board-views")
	ctx, m := t.Context(), manager(f)
	views, err := m.TaskViews(ctx, f.token, w.ID)
	must(t, err)
	view := views[0]
	if view.Display.Mode != "table" {
		t.Fatalf("default mode: %q", view.Display.Mode)
	}
	settings := view.ViewSettings
	settings.Display.Mode = "board"
	settings.Display.Group = "status"
	saved, err := m.SaveTaskView(ctx, f.token, w.ID, view.ID, view.Version, settings)
	must(t, err)
	retry, err := m.SaveTaskView(ctx, f.token, w.ID, view.ID, view.Version, settings)
	must(t, err)
	if retry.Version != saved.Version {
		t.Fatal("identical board save not idempotent")
	}
	loaded, err := m.TaskViews(ctx, f.token, w.ID)
	must(t, err)
	if loaded[0].Display.Mode != "board" || loaded[0].Display.Group != "status" {
		t.Fatal("board settings lost", loaded)
	}
	settings.Display.Mode = "invalid"
	if _, err := m.SaveTaskView(ctx, f.token, w.ID, view.ID, saved.Version, settings); err == nil {
		t.Fatal("invalid mode accepted")
	}
}
