package integration

import (
	"acta/internal/tasks"
	"testing"
)

func TestTaskAssignmentPageIsolation(t *testing.T) {
	root := securityDatabase(t)
	owner, f, w := workspaceOwner(t, root, "assignmentpage")
	ctx, m := t.Context(), manager(f)
	roots := []tasks.Task{}
	children := []tasks.Task{}
	for _, name := range []string{"First", "Second"} {
		task, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: name, Assignees: []string{owner.ID}})
		must(t, e)
		child, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: name + " child", ParentID: task.ID, Assignees: []string{owner.ID}})
		must(t, e)
		roots = append(roots, task)
		children = append(children, child)
	}
	page, e := m.Tasks(ctx, f.token, w.ID, tasks.Filter{State: "all", Sort: "number", Direction: "asc"})
	must(t, e)
	if len(page.Tasks) != 2 {
		t.Fatal(page)
	}
	for i, v := range page.Tasks {
		if v.ID != roots[i].ID || len(v.Assignees) != 1 || len(v.Assignees[0].Sources) != 0 || len(v.DescendantAssignees) != 1 {
			t.Fatal(v)
		}
		sources := v.DescendantAssignees[0].Sources
		if len(sources) != 1 || sources[0].ID != children[i].ID || sources[0].Reference != children[i].Reference {
			t.Fatal("assignment source leaked between roots", v)
		}
	}
}
