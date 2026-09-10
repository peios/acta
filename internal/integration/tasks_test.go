package integration

import (
	"acta2/internal/auth"
	"acta2/internal/tasks"
	ws "acta2/internal/workspaces"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"strings"
	"sync"
	"testing"
)

func patchTask(t *testing.T, f securityFixture, v tasks.Task, field string, value any) (tasks.Task, error) {
	t.Helper()
	raw, _ := json.Marshal(value)
	return manager(f).PatchTask(t.Context(), f.token, v.ID, tasks.Patch{Field: field, Version: v.Versions[field], Value: raw})
}
func TestTasksIdentityHierarchyAndConflicts(t *testing.T) {
	root := securityDatabase(t)
	owner, f, w := workspaceOwner(t, root, "tasks")
	ctx := t.Context()
	m := manager(f)
	c, e := m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	if len(c.Statuses) != 4 || c.Creation == c.Completed {
		t.Fatal(c)
	}
	a, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Parent"})
	must(t, e)
	b, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Child", ParentID: a.Reference, Assignees: []string{owner.ID}})
	must(t, e)
	a, e = m.Task(ctx, f.token, a.ID)
	must(t, e)
	if len(a.DescendantAssignees) != 1 || a.Children != 1 {
		t.Fatal("rollup", a)
	}
	if _, e = patchTask(t, f, a, "parent_id", b.ID); e == nil {
		t.Fatal("cycle allowed")
	}
	old := a
	a, e = patchTask(t, f, a, "title", "New title")
	must(t, e)
	_, e = patchTask(t, f, old, "title", "Lost update")
	var conflict *tasks.Conflict
	if !errors.As(e, &conflict) || conflict.Field != "title" || conflict.Version != a.Versions["title"] || conflict.Value != "New title" {
		t.Fatal("stale update", e)
	}
	a, e = patchTask(t, f, old, "description", "Independent description")
	must(t, e)
	a, e = patchTask(t, f, a, "status_id", c.Completed)
	must(t, e)
	list, e := m.Tasks(ctx, f.token, w.ID, tasks.Filter{})
	must(t, e)
	if len(list.Tasks) != 0 {
		t.Fatal("completed subtree exposed")
	}
	list, e = m.Tasks(ctx, f.token, w.ID, tasks.Filter{Parent: a.ID, State: "all"})
	must(t, e)
	if len(list.Tasks) != 1 {
		t.Fatal("child disappeared")
	}
	c, e = m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	must(t, m.TaskPrefix(ctx, f.token, w.ID, "NEW", c.Version))
	renamed, e := m.Task(ctx, f.token, b.Reference)
	must(t, e)
	if renamed.ID != b.ID || !strings.HasPrefix(renamed.Reference, "NEW-") {
		t.Fatal("alias broken")
	}
	_, other := permissionMember(t, root, "other")
	if _, e = manager(other).Task(ctx, other.token, a.Reference); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("private task revealed", e)
	}
	c, e = m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	if e = m.TaskPrefix(ctx, f.token, w.ID, "xx1", c.Version); e == nil {
		t.Fatal("invalid prefix")
	}
}
func TestTasksStatusesAssignmentAndReferences(t *testing.T) {
	root := securityDatabase(t)
	owner, f, w := workspaceOwner(t, root, "tasks")
	ctx := t.Context()
	m := manager(f)
	c, e := m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	v, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Target"})
	must(t, e)
	text := "Hello @" + owner.Username + " and " + v.Reference + ". `" + v.Reference + "`"
	a, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "References", Description: text, Assignees: []string{owner.ID}})
	must(t, e)
	if !strings.Contains(a.Description, "/tasks/"+v.ID) || !strings.Contains(a.Description, "/references/accounts/"+owner.ID) || !strings.Contains(a.Description, "`"+v.Reference+"`") {
		t.Fatal(a.Description)
	}
	a, e = patchTask(t, f, a, "status_id", c.Statuses[1].ID)
	must(t, e)
	next := tasks.StatusChange{Statuses: []tasks.Status{c.Statuses[0], c.Statuses[2], {ID: uuid.NewString(), Name: "Review"}}, Creation: c.Creation, Completed: c.Completed, Version: c.Version, Replacements: map[string]string{c.Statuses[1].ID: c.Creation}}
	must(t, m.TaskStatuses(ctx, f.token, w.ID, next))
	a, e = m.Task(ctx, f.token, a.ID)
	must(t, e)
	if a.StatusID != c.Creation || a.Versions["status_id"] != 3 {
		t.Fatal("status remap", a)
	}
	outsider, other := permissionMember(t, root, "outsider")
	if _, e = patchTask(t, f, a, "assignees", []string{outsider.ID}); e == nil {
		t.Fatal("assigned inaccessible")
	}
	w = joinWorkspace(t, f, w, outsider.ID)
	a, e = patchTask(t, f, a, "assignees", []string{owner.ID, outsider.ID})
	must(t, e)
	must(t, m.WorkspaceMembership(ctx, f.token, w.ID, outsider.ID, w.Version, false))
	a, e = m.Task(ctx, f.token, a.ID)
	must(t, e)
	found := false
	for _, p := range a.Assignees {
		if p.ID == outsider.ID {
			found = true
			if p.Available {
				t.Fatal("lost access unmarked")
			}
		}
	}
	if !found {
		t.Fatal("lost assignment")
	}
	if _, e = manager(other).CreateTask(ctx, other.token, w.ID, tasks.Create{Title: "Denied"}); e == nil {
		t.Fatal("nonmember create")
	}
	agent := agentAccount(t, f, "reviewer", nil)
	secret := agentCLI(t, f, agent.ID)
	v, e = m.CreateTask(ctx, secret, w.ID, tasks.Create{Title: "Agent task"})
	must(t, e)
	if v.ID == "" {
		t.Fatal("agent result")
	}
	access, e := m.AgentWorkspaces(ctx, f.token, agent.ID)
	must(t, e)
	must(t, m.SetAgentWorkspacePolicy(ctx, f.token, agent.ID, w.ID, access.Version, false, []string{}))
	if _, e = m.CreateTask(ctx, secret, w.ID, tasks.Create{Title: "Denied agent"}); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("agent ceiling", e)
	}
}
func TestTasksConcurrentNumbersAndParents(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "tasks")
	ctx := t.Context()
	m := manager(f)
	var wg sync.WaitGroup
	results := make(chan tasks.Task, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Concurrent"})
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
	seen := map[int64]bool{}
	for v := range results {
		if seen[v.Number] {
			t.Fatal("duplicate number")
		}
		seen[v.Number] = true
	}
	a, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "A"})
	must(t, e)
	b, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "B"})
	must(t, e)
	errorsOut := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); _, e := patchTask(t, f, a, "parent_id", b.ID); errorsOut <- e }()
	go func() { defer wg.Done(); _, e := patchTask(t, f, b, "parent_id", a.ID); errorsOut <- e }()
	wg.Wait()
	close(errorsOut)
	n := 0
	for e := range errorsOut {
		if e == nil {
			n++
		}
	}
	if n != 1 {
		t.Fatal("concurrent cycle", n)
	}
	_ = ws.EditTasks
}

func TestTasksStatusValidationAndConfigurationPermissions(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "scope")
	ctx := t.Context()
	m := manager(f)
	c, e := m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	for _, status := range []string{"invalid", uuid.NewString()} {
		if _, e = m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Invalid", StatusID: status}); e == nil {
			t.Fatal("invalid status accepted")
		}
	}
	a, member := permissionMember(t, root, "member")
	w = joinWorkspace(t, f, w, a.ID)
	w = workspaceGrants(t, f, w, a.ID, false, []string{ws.ManageStatuses})
	if _, e = manager(member).CreateTask(ctx, member.token, w.ID, tasks.Create{Title: "Denied"}); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("status manager created task", e)
	}
	if e = manager(member).TaskPrefix(ctx, member.token, w.ID, "DENIED", c.Version); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("status manager edited prefix", e)
	}
	c.Statuses[1].Name = "Review"
	must(t, manager(member).TaskStatuses(ctx, member.token, w.ID, tasks.StatusChange{Statuses: c.Statuses[:3], Creation: c.Creation, Completed: c.Completed, Version: c.Version}))
	people, e := m.TaskPeople(ctx, f.token, w.ID, "member")
	must(t, e)
	if len(people) != 1 || people[0].ID != a.ID {
		t.Fatal("assignable directory", people)
	}
}

func TestTasksStatusAndAssigneeFilters(t *testing.T) {
	root := securityDatabase(t)
	owner, f, w := workspaceOwner(t, root, "filters")
	ctx, m := t.Context(), manager(f)
	c, e := m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	parent, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Completed parent", StatusID: c.Completed})
	must(t, e)
	child, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Assigned child", ParentID: parent.ID, Assignees: []string{owner.ID}})
	must(t, e)
	unassigned, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Unassigned root"})
	must(t, e)
	checks := []struct {
		name   string
		filter tasks.Filter
		ids    []string
	}{
		{"assignee excludes nested work", tasks.Filter{State: "all", Assignees: []string{owner.ID}}, nil},
		{"status excludes children", tasks.Filter{State: "all", Statuses: []string{c.Creation}}, []string{unassigned.ID}},
		{"categories intersect", tasks.Filter{State: "all", Statuses: []string{c.Completed}, Assignees: []string{owner.ID}}, nil},
		{"unassigned direct only", tasks.Filter{State: "all", Unassigned: true}, []string{unassigned.ID, parent.ID}},
		{"unassigned or chosen person", tasks.Filter{State: "all", Assignees: []string{owner.ID}, Unassigned: true}, []string{unassigned.ID, parent.ID}},
		{"multiple statuses", tasks.Filter{State: "all", Statuses: []string{c.Creation, c.Completed}}, []string{unassigned.ID, parent.ID}},
		{"explicit parent remains scoped", tasks.Filter{State: "all", Parent: parent.ID, Assignees: []string{owner.ID}}, []string{child.ID}},
		{"query intersects filters", tasks.Filter{State: "all", Query: "Unassigned", Assignees: []string{owner.ID}}, nil},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			page, e := m.Tasks(ctx, f.token, w.ID, check.filter)
			must(t, e)
			if len(page.Tasks) != len(check.ids) {
				t.Fatalf("got %d tasks, want %d", len(page.Tasks), len(check.ids))
			}
			for i, id := range check.ids {
				if page.Tasks[i].ID != id {
					t.Fatalf("task %d: got %s, want %s", i, page.Tasks[i].ID, id)
				}
			}
		})
	}
	if _, e := m.Tasks(ctx, f.token, w.ID, tasks.Filter{Statuses: []string{"not-an-id"}}); e == nil {
		t.Fatal("invalid status accepted")
	}
	// Filtering must occur before pagination; matching children never enter root pages.
	for i := 0; i < 51; i++ {
		_, e = m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Pagination root", Assignees: []string{owner.ID}})
		must(t, e)
	}
	filter := tasks.Filter{State: "all", Assignees: []string{owner.ID}}
	first, e := m.Tasks(ctx, f.token, w.ID, filter)
	must(t, e)
	if len(first.Tasks) != 50 || !first.More {
		t.Fatal("first filtered page", len(first.Tasks), first.More)
	}
	filter.Before = first.Next
	second, e := m.Tasks(ctx, f.token, w.ID, filter)
	must(t, e)
	if len(second.Tasks) != 1 || second.More || second.Tasks[0].ParentID != "" {
		t.Fatal("second filtered page", second)
	}
}
