package integration

import (
	"fmt"
	"testing"

	"acta2/internal/tasks"
)

func TestTaskInspectionPaginatesDirectChildren(t *testing.T) {
	f := securityDatabase(t)
	m := manager(f)
	ctx := t.Context()
	w, err := m.CreateWorkspace(ctx, f.token, "Inspect", "inspect", "")
	must(t, err)
	parent, err := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Parent"})
	must(t, err)
	var first tasks.Task
	for i := 0; i < 51; i++ {
		child, err := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: fmt.Sprintf("Child %02d", i), ParentID: parent.ID})
		must(t, err)
		if i == 0 {
			first = child
		}
	}
	_, err = m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Grandchild", ParentID: first.ID})
	must(t, err)
	detail, err := m.InspectTask(ctx, f.token, parent.Reference, "")
	must(t, err)
	if detail.Subtasks.Total != 51 || len(detail.Subtasks.Tasks) != 50 || !detail.Subtasks.More || detail.Subtasks.Tasks[0].ID != first.ID {
		t.Fatal(detail.Subtasks)
	}
	detail, err = m.InspectTask(ctx, f.token, parent.ID, detail.Subtasks.Cursor)
	must(t, err)
	if detail.Subtasks.Total != 51 || len(detail.Subtasks.Tasks) != 1 || detail.Subtasks.More || detail.Subtasks.Tasks[0].Title != "Child 50" {
		t.Fatal(detail.Subtasks)
	}
}
