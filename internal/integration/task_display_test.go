package integration

import (
	"acta/internal/tasks"
	"cmp"
	"slices"
	"strings"
	"testing"
)

func TestTaskDisplayPersistence(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "display")
	ctx, m := t.Context(), manager(f)
	v, e := m.CreateTaskView(ctx, f.token, w.ID, "Grouped", tasks.ViewSettings{Display: tasks.ViewDisplay{Columns: []string{}, Sort: "title", Direction: "asc", Group: "status", Density: "compact"}})
	must(t, e)
	if len(v.Display.Columns) != 0 || v.Display.Group != "status" || v.Display.Sort != "title" {
		t.Fatal(v)
	}
	listed, e := m.TaskViews(ctx, f.token, w.ID)
	must(t, e)
	if len(listed) != 2 || listed[1].Display.Density != "compact" || listed[0].Display.Sort != "number" {
		t.Fatal(listed)
	}
	changed := v.ViewSettings
	changed.Display.Columns = []string{"assignees", "status"}
	saved, e := m.SaveTaskView(ctx, f.token, w.ID, v.ID, v.Version, changed)
	must(t, e)
	if saved.Version != v.Version+1 || !slices.Equal(saved.Display.Columns, []string{"status", "assignees"}) {
		t.Fatal(saved)
	}
	retry, e := m.SaveTaskView(ctx, f.token, w.ID, v.ID, v.Version, changed)
	must(t, e)
	if retry.Version != saved.Version {
		t.Fatal("non-idempotent display save")
	}
	for _, bad := range []tasks.ViewDisplay{{Sort: "injected"}, {Direction: "sideways"}, {Group: "assignees"}, {Density: "tiny"}, {Columns: []string{"secret"}}} {
		if _, e = m.SaveTaskView(ctx, f.token, w.ID, v.ID, saved.Version, tasks.ViewSettings{Display: bad}); e == nil {
			t.Fatal("invalid display accepted", bad)
		}
	}
}
func TestTasksSortedPagination(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "sorted")
	ctx, m := t.Context(), manager(f)
	config, e := m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	config.Statuses = config.Statuses[:3] // This test exercises primary-board pagination.
	ranks := map[string]int{}
	for i, s := range config.Statuses {
		ranks[s.ID] = i
	}
	all := []tasks.Task{}
	for i := 0; i < 57; i++ {
		v, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: []string{"Zulu", "alpha", "Beta"}[i%3], StatusID: config.Statuses[i%len(config.Statuses)].ID, Priority: tasks.PropertyOptions("priority")[i%5].Value, Type: tasks.PropertyOptions("type")[i%4].Value, Size: tasks.PropertyOptions("size")[i%6].Value})
		must(t, e)
		all = append(all, v)
	}
	// A child sorts before the roots by title but must never leak into root results.
	_, e = m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "AAAA child", ParentID: all[0].ID})
	must(t, e)
	for _, sort := range []string{"number", "title", "status", "created", "updated", "priority", "type", "size"} {
		for _, direction := range []string{"asc", "desc"} {
			t.Run(sort+"/"+direction, func(t *testing.T) {
				expected := slices.Clone(all)
				slices.SortFunc(expected, func(a, b tasks.Task) int {
					n := 0
					switch sort {
					case "priority", "type", "size":
						rank := func(v tasks.Task) int {
							value := map[string]string{"priority": v.Priority, "type": v.Type, "size": v.Size}[sort]
							for i, o := range tasks.PropertyOptions(sort) {
								if o.Value == value {
									return i
								}
							}
							return -1
						}
						n = cmp.Compare(rank(a), rank(b))
					case "number":
						n = cmp.Compare(a.Number, b.Number)
					case "title":
						n = cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
					case "status":
						n = cmp.Compare(ranks[a.StatusID], ranks[b.StatusID])
					case "created":
						n = a.CreatedAt.Compare(b.CreatedAt)
					case "updated":
						n = a.UpdatedAt.Compare(b.UpdatedAt)
					}
					if n == 0 {
						n = cmp.Compare(a.Number, b.Number)
					}
					if direction == "desc" {
						n = -n
					}
					return n
				})
				filter := tasks.Filter{State: "all", Sort: sort, Direction: direction}
				first, e := m.Tasks(ctx, f.token, w.ID, filter)
				must(t, e)
				if !first.More || len(first.Tasks) != 50 || first.Cursor == "" || first.Total != 57 {
					t.Fatal(first)
				}
				filter.Cursor = first.Cursor
				second, e := m.Tasks(ctx, f.token, w.ID, filter)
				must(t, e)
				if second.More || len(second.Tasks) != 7 || second.Total != 57 {
					t.Fatal(second)
				}
				got := append(first.Tasks, second.Tasks...)
				for i, v := range got {
					if v.ID != expected[i].ID {
						t.Fatalf("wrong order at %d: got %s want %s", i, v.ID, expected[i].ID)
					}
				}
			})
		}
	}
	for _, filter := range []tasks.Filter{
		{State: "all", Query: "alpha"},
		{State: "all", Statuses: []string{config.Statuses[0].ID}},
		{State: "all", Unassigned: true},
		{State: "all", Parent: all[0].ID},
		{State: "all", Query: "no matches"},
	} {
		page, e := m.Tasks(ctx, f.token, w.ID, filter)
		must(t, e)
		expected := int64(19)
		if filter.Unassigned {
			expected = 57
		}
		if filter.Parent != "" {
			expected = 1
		}
		if filter.Query == "no matches" {
			expected = 0
		}
		if page.Total != expected {
			t.Fatalf("count for %+v: got %d want %d", filter, page.Total, expected)
		}
	}
	for _, filter := range []tasks.Filter{{Sort: "invalid"}, {Direction: "invalid"}, {Cursor: "nonsense"}, {Sort: "title", Before: 1}, {Sort: "title", Cursor: tasks.Cursor{Sort: "number", Direction: "desc", Number: 1, Value: "1"}.Encode()}} {
		if _, e = m.Tasks(ctx, f.token, w.ID, filter); e == nil {
			t.Fatal("invalid ordering accepted", filter)
		}
	}
}
