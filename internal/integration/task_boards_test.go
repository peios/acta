package integration

import (
	"acta2/internal/tasks"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestTaskBoardsLifecycleAndIsolation(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "boards")
	m, ctx := manager(f), t.Context()
	c, e := m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	if len(c.Boards) != 2 || len(c.Statuses) != 4 {
		t.Fatal("defaults", c)
	}
	backlog := c.Boards[1]
	if backlog.Slug != "backlog" || backlog.Completed != "" {
		t.Fatal(backlog)
	}
	a, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Board lifecycle needle", Board: "backlog"})
	must(t, e)
	child, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Child", ParentID: a.ID})
	must(t, e)
	if a.Board != "backlog" || child.Board != "backlog" || a.StatusID != backlog.Creation {
		t.Fatal(a, child)
	}
	if _, e = m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Mismatch", Board: "tasks", StatusID: backlog.Creation}); e == nil {
		t.Fatal("mismatched board/status accepted")
	}
	if _, e = m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Invalid board", Board: "bogus"}); e == nil {
		t.Fatal("invalid board accepted")
	}
	p, e := m.Tasks(ctx, f.token, w.ID, tasks.Filter{State: "all"})
	must(t, e)
	if p.Total != 0 {
		t.Fatal("backlog leaked", p)
	}
	p, e = m.Tasks(ctx, f.token, w.ID, tasks.Filter{Board: "backlog", State: "unfinished"})
	must(t, e)
	if p.Total != 1 {
		t.Fatal("backlog not unfinished", p)
	}
	found, e := m.SearchTasks(ctx, f.token, tasks.SearchQuery{Query: "lifecycle needle", Workspace: w.ID})
	must(t, e)
	if len(found.Tasks) != 1 || found.Tasks[0].ID != a.ID {
		t.Fatal("global search missed backlog", found)
	}
	mainViews, e := m.TaskViews(ctx, f.token, w.ID)
	must(t, e)
	backViews, e := m.TaskViews(ctx, f.token, w.ID, "backlog")
	must(t, e)
	if len(mainViews) != 1 || len(backViews) != 1 || mainViews[0].ID == backViews[0].ID || backViews[0].Board != "backlog" {
		t.Fatal(mainViews, backViews)
	}
	if e = m.DeleteTaskView(ctx, f.token, w.ID, backViews[0].ID, backViews[0].Version); e == nil {
		t.Fatal("deleted final backlog preset")
	}
	wrong := backViews[0].ViewSettings
	wrong.Board = "tasks"
	if _, e = m.SaveTaskView(ctx, f.token, w.ID, backViews[0].ID, backViews[0].Version, wrong); e == nil {
		t.Fatal("preset crossed board")
	}
	raw, _ := json.Marshal("tasks")
	moved, e := m.PatchTask(ctx, f.token, a.ID, tasks.Patch{Field: "board", Version: a.Versions["status_id"], Value: raw})
	must(t, e)
	if moved.ID != a.ID || moved.Reference != a.Reference || moved.Board != "tasks" || moved.StatusID != c.Creation {
		t.Fatal("move changed identity", moved)
	}
	child, e = m.Task(ctx, f.token, child.ID)
	must(t, e)
	if child.Board != "backlog" || child.ParentID != a.ID {
		t.Fatal("move changed child", child)
	}
	p, e = m.Tasks(ctx, f.token, w.ID, tasks.Filter{Board: "backlog", State: "all"})
	must(t, e)
	if p.Total != 1 || p.Tasks[0].ID != child.ID {
		t.Fatal("cross-board child hidden", p)
	}
	p, e = m.Tasks(ctx, f.token, w.ID, tasks.Filter{Parent: a.ID, State: "all"})
	must(t, e)
	if p.Total != 1 {
		t.Fatal("child absent from parent", p)
	}
	raw, _ = json.Marshal("backlog")
	if _, e = m.PatchTask(ctx, f.token, a.ID, tasks.Patch{Field: "board", Version: a.Versions["status_id"], Value: raw}); e == nil {
		t.Fatal("stale move accepted")
	}
	history, e := m.TaskActivity(ctx, f.token, a.ID, "")
	must(t, e)
	if len(history.Entries) < 2 {
		t.Fatal("missing move activity", history)
	}
	// Backlog can have its own Done with no collision, and replacement is confined to its board.
	done := uuid.NewString()
	c, e = m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	change := tasks.StatusChange{Board: "backlog", Version: c.Version, Creation: backlog.Creation, Completed: done, Statuses: []tasks.Status{{ID: backlog.Creation, Name: "Backlog"}, {ID: done, Name: "Done"}}}
	must(t, m.TaskStatuses(ctx, f.token, w.ID, change))
	child, e = patchTask(t, f, child, "status_id", done)
	must(t, e)
	sameBoard, _ := json.Marshal("backlog")
	same, e := m.PatchTask(ctx, f.token, child.ID, tasks.Patch{Field: "board", Version: child.Versions["status_id"], Value: sameBoard})
	must(t, e)
	if same.StatusID != done || same.Versions["status_id"] != child.Versions["status_id"] {
		t.Fatal("same-board move reset status", same)
	}
	p, e = m.Tasks(ctx, f.token, w.ID, tasks.Filter{Board: "backlog", State: "completed"})
	must(t, e)
	if p.Total != 1 {
		t.Fatal("board completion", p)
	}
	c, e = m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	if c.Completed == done {
		t.Fatal("primary completion changed")
	}
	change.Version = c.Version
	change.Statuses = []tasks.Status{{ID: backlog.Creation, Name: "Ideas"}}
	change.Completed = ""
	change.Replacements = map[string]string{done: backlog.Creation}
	must(t, m.TaskStatuses(ctx, f.token, w.ID, change))
	child, e = m.Task(ctx, f.token, child.ID)
	must(t, e)
	if child.StatusID != backlog.Creation {
		t.Fatal("replacement", child)
	}
	c, e = m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	change.Version = c.Version
	change.Statuses = []tasks.Status{{ID: c.Creation, Name: "Stolen"}}
	change.Creation = c.Creation
	if e = m.TaskStatuses(ctx, f.token, w.ID, change); e == nil {
		t.Fatal("status stolen across boards")
	}
	c, e = m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	if c.Statuses[0].Name != "To do" {
		t.Fatal("failed change not rolled back", c)
	}
}
