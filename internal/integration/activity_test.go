package integration

import (
	"acta2/internal/activity"
	"acta2/internal/auth"
	"acta2/internal/tasks"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func history(t *testing.T, f securityFixture, ref string) activity.Page {
	t.Helper()
	v, e := manager(f).TaskActivity(t.Context(), f.token, ref, "")
	must(t, e)
	return v
}
func TestTaskActivityAtomicGroupingAndReadTracking(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "activity")
	ctx := t.Context()
	m := manager(f)
	task, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Before"})
	must(t, e)
	p := history(t, root, task.ID)
	if len(p.Entries) != 1 || p.Entries[0].Kind != "task.created" || !p.Unread {
		t.Fatal(p)
	}
	if history(t, f, task.ID).Unread {
		t.Fatal("own creation unread")
	}
	task, e = patchTask(t, f, task, "title", "Middle")
	must(t, e)
	task, e = patchTask(t, f, task, "title", "After")
	must(t, e)
	p = history(t, root, task.ID)
	if len(p.Entries) != 2 || p.Entries[0].Count != 2 || p.Entries[0].Before.Text != "Before" || p.Entries[0].After.Text != "After" {
		t.Fatal(p)
	}
	task, e = patchTask(t, f, task, "title", "Third")
	must(t, e)
	p = history(t, root, task.ID)
	if p.Entries[0].Count != 3 || p.Entries[0].Before.Text != "Before" || p.Entries[0].After.Text != "Third" {
		t.Fatal("group lost first value", p)
	}
	seen := activity.Seen{ID: p.Entries[0].ID, Through: p.Entries[0].Last}
	must(t, manager(root).ReadTaskActivity(ctx, root.token, task.ID, []activity.Seen{seen}))
	p = history(t, root, task.ID)
	if p.Entries[0].Unread || !p.Entries[1].Unread || !p.Unread {
		t.Fatal("read skipped unseen creation", p)
	}
	task, e = patchTask(t, f, task, "title", "Extended")
	must(t, e)
	p = history(t, root, task.ID)
	if p.Entries[0].ID != seen.ID || !p.Entries[0].Unread || p.Entries[0].Count != 4 {
		t.Fatal("group extension not unread", p)
	}
	must(t, manager(root).ReadTaskActivity(ctx, root.token, task.ID, []activity.Seen{seen}))
	if !history(t, root, task.ID).Entries[0].Unread {
		t.Fatal("old ack cleared new edit")
	}
	// Read-only retrieval is inert and own actions never advance another actor's read position.
	if !history(t, root, task.ID).Unread {
		t.Fatal("retrieval marked read")
	}
	task, e = patchTask(t, root, task, "description", "secret description never stored in activity")
	must(t, e)
	p = history(t, root, task.ID)
	if p.Entries[0].Unread || !p.Unread {
		t.Fatal("own edit cleared older unread", p)
	}
	raw, e := json.Marshal(p)
	must(t, e)
	if strings.Contains(string(raw), "secret description") {
		t.Fatal("description revision retained")
	}
	var leaked bool
	must(t, root.conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM activity_events WHERE change::text LIKE '%secret description%')`).Scan(&leaked))
	if leaked {
		t.Fatal("raw description retained")
	}
	task, e = patchTask(t, f, task, "title", "After interruption")
	must(t, e)
	p = history(t, root, task.ID)
	if p.Entries[0].Count != 1 || p.Entries[0].ID == seen.ID {
		t.Fatal("intervening event didn't break group")
	}
	var count int
	must(t, root.conn.QueryRow(ctx, `SELECT count(*) FROM activity_events WHERE subject_id=$1`, task.ID).Scan(&count))
	// Saved-value retries and rejected stale writes do not record activity.
	_, e = patchTask(t, f, task, "title", task.Title)
	must(t, e)
	_, e = m.PatchTask(ctx, f.token, task.ID, tasks.Patch{Field: "title", Version: 1, Value: json.RawMessage(`"stale"`)})
	if e == nil {
		t.Fatal("stale accepted")
	}
	var after int
	must(t, root.conn.QueryRow(ctx, `SELECT count(*) FROM activity_events WHERE subject_id=$1`, task.ID).Scan(&after))
	if after != count {
		t.Fatal("no-op emitted event")
	}
	// An event-store failure rolls back the task mutation too.
	_, e = root.conn.Exec(ctx, `CREATE FUNCTION fail_activity_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test failure'; END; $$; CREATE TRIGGER fail_activity BEFORE INSERT ON activity_events FOR EACH ROW EXECUTE FUNCTION fail_activity_insert()`)
	must(t, e)
	_, e = patchTask(t, f, task, "title", "Must roll back")
	if e == nil {
		t.Fatal("injected failure ignored")
	}
	current, e := m.Task(ctx, f.token, task.ID)
	must(t, e)
	if current.Title != task.Title {
		t.Fatal("task committed without event")
	}
	_, e = root.conn.Exec(ctx, `DROP TRIGGER fail_activity ON activity_events`)
	must(t, e)
	if _, e = root.conn.Exec(ctx, `UPDATE activity_events SET occurred_at=now() WHERE subject_id=$1`, task.ID); e == nil {
		t.Fatal("raw event mutable")
	}
	if _, e = root.conn.Exec(ctx, `DELETE FROM activity_events WHERE subject_id=$1`, task.ID); e == nil {
		t.Fatal("raw event deletable")
	}
	_, outsider := permissionMember(t, root, "outsideactivity")
	if _, e = manager(outsider).TaskActivity(ctx, outsider.token, task.ID, ""); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("activity access leak", e)
	}
	if e = manager(outsider).ReadTaskActivity(ctx, outsider.token, task.ID, []activity.Seen{seen}); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("read access leak", e)
	}
	other, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Other"})
	must(t, e)
	if e = manager(root).ReadTaskActivity(ctx, root.token, other.ID, []activity.Seen{seen}); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("cross-task ack accepted", e)
	}
}
func TestTaskActivityPagingAndStatusReplacement(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	w, e := m.CreateWorkspace(ctx, f.token, "Activity pages", "activity-pages", "")
	must(t, e)
	task, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Task"})
	must(t, e)
	for i := 0; i < 54; i++ {
		field := "title"
		if i%2 == 1 {
			field = "description"
		}
		task, e = patchTask(t, f, task, field, fmt.Sprintf("Edit %02d", i))
		must(t, e)
	}
	p := history(t, f, task.ID)
	if len(p.Entries) != 50 || !p.More || p.Cursor == "" {
		t.Fatal(p)
	}
	older, e := m.TaskActivity(ctx, f.token, task.Reference, p.Cursor)
	must(t, e)
	if len(older.Entries) != 5 || older.More || older.Entries[4].Kind != "task.created" {
		t.Fatal(older)
	}
	c, e := m.TaskConfig(ctx, f.token, w.ID)
	must(t, e)
	task, e = patchTask(t, f, task, "status_id", c.Statuses[1].ID)
	must(t, e)
	must(t, m.TaskStatuses(ctx, f.token, w.ID, tasks.StatusChange{Statuses: []tasks.Status{c.Statuses[0], c.Statuses[2]}, Creation: c.Creation, Completed: c.Completed, Version: c.Version, Replacements: map[string]string{c.Statuses[1].ID: c.Completed}}))
	p = history(t, f, task.ID)
	if p.Entries[0].Reason != "status_replaced" || p.Entries[0].Before.Items[0].Label != "In progress" || p.Entries[0].After.Items[0].Label != "Done" {
		t.Fatal(p.Entries[0])
	}
	// Expiring the projection's last-edit time simulates an idle grouping window.
	task, e = m.Task(ctx, f.token, task.ID)
	must(t, e)
	task, e = patchTask(t, f, task, "title", "First")
	must(t, e)
	first := history(t, f, task.ID).Entries[0]
	_, e = f.conn.Exec(ctx, `UPDATE activity_entries SET updated_at=clock_timestamp()-interval '61 seconds' WHERE id=$1`, first.ID)
	must(t, e)
	task, e = patchTask(t, f, task, "title", "Second")
	must(t, e)
	if history(t, f, task.ID).Entries[0].ID == first.ID {
		t.Fatal("expired edits merged")
	}
}
