package integration

import (
	"acta2/internal/activity"
	"acta2/internal/auth"
	"acta2/internal/comments"
	"acta2/internal/tasks"
	ws "acta2/internal/workspaces"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"sync"
	"testing"
)

func postComment(t *testing.T, f securityFixture, task, body, reply string) activity.Entry {
	t.Helper()
	v, e := manager(f).CreateComment(t.Context(), f.token, task, comments.Create{Body: body, ReplyTo: reply, RequestID: uuid.NewString()})
	must(t, e)
	return v
}
func TestCommentsThreadsEditsAndSparseReads(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "comments")
	ctx := t.Context()
	m := manager(f)
	task, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Comments"})
	must(t, e)
	c := postComment(t, f, task.ID, "Original", "")
	if _, e = m.Comment(ctx, f.token, task.ID, ""); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("missing comment ID accepted", e)
	}
	reply := postComment(t, root, task.ID, "Reply", c.ID)
	nested := postComment(t, f, task.ID, "Reply to reply", reply.ID)
	if nested.Comment.ThreadID != c.ID || nested.Comment.ReplyTo != reply.ID {
		t.Fatal(nested)
	}
	p := history(t, root, task.ID)
	if len(p.Entries) != 2 || p.Entries[0].ID != c.ID || p.Entries[0].ReplyCount != 2 || !p.Entries[0].ThreadUnread {
		t.Fatal(p)
	}
	must(t, manager(root).ReadTaskActivity(ctx, root.token, task.ID, []activity.Seen{{ID: c.ID, Through: c.Last}}))
	if !history(t, root, task.ID).Unread {
		t.Fatal("parent ack cleared collapsed replies")
	}
	replies, e := manager(root).CommentReplies(ctx, root.token, task.ID, c.ID, "")
	must(t, e)
	if len(replies.Entries) != 2 || replies.Entries[0].ID != nested.ID || !replies.Entries[0].Unread || replies.Entries[1].Unread {
		t.Fatal(replies)
	}
	must(t, manager(root).ReadTaskActivity(ctx, root.token, task.ID, []activity.Seen{{ID: nested.ID, Through: nested.Last}}))
	if history(t, root, task.ID).Entries[0].ThreadUnread {
		t.Fatal("reply read not applied")
	}
	// A moderator editing the author's own comment becomes unread for that author.
	edited, e := manager(root).UpdateComment(ctx, root.token, task.ID, c.ID, comments.Update{Body: "Moderated", Version: 1})
	must(t, e)
	if edited.First != c.First || !edited.Comment.Edited || edited.Comment.Version != 2 || edited.Actor.ID != c.Actor.ID {
		t.Fatal(edited)
	}
	own, e := m.Comment(ctx, f.token, task.ID, c.ID)
	must(t, e)
	if !own.Unread {
		t.Fatal("moderator edit hidden as own activity")
	}
	_, e = m.UpdateComment(ctx, f.token, task.ID, c.ID, comments.Update{Body: "stale", Version: 1})
	var conflict *tasks.Conflict
	if !errors.As(e, &conflict) || conflict.Version != 2 {
		t.Fatal(e)
	}
	deleted, e := m.UpdateComment(ctx, f.token, task.ID, c.ID, comments.Update{Delete: true, Version: 2})
	must(t, e)
	if !deleted.Comment.Deleted || deleted.Comment.Body != "" || deleted.ReplyCount != 2 || deleted.Comment.CanEdit {
		t.Fatal(deleted)
	}
	if _, e = m.UpdateComment(ctx, f.token, task.ID, c.ID, comments.Update{Body: "resurrect", Version: 3}); e == nil {
		t.Fatal("deleted comment resurrected")
	}
	postComment(t, f, task.ID, "Still reply to placeholder", c.ID)
	var leaked bool
	must(t, root.conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM activity_events WHERE change::text LIKE '%Moderated%' OR change::text LIKE '%Original%')`).Scan(&leaked))
	if leaked {
		t.Fatal("comment text retained in immutable event")
	}
}
func TestCommentsIdempotencyPermissionsAndAtomicity(t *testing.T) {
	root := securityDatabase(t)
	owner, f, w := workspaceOwner(t, root, "comment-access")
	ctx := t.Context()
	m := manager(f)
	task, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Task"})
	must(t, e)
	input := comments.Create{Body: "Once", RequestID: uuid.NewString()}
	first, e := m.CreateComment(ctx, f.token, task.ID, input)
	must(t, e)
	retry, e := m.CreateComment(ctx, f.token, task.Reference, input)
	must(t, e)
	if first.ID != retry.ID || len(history(t, f, task.ID).Entries) != 2 {
		t.Fatal("duplicate post")
	}
	input.Body = "Different"
	if _, e = m.CreateComment(ctx, f.token, task.ID, input); e == nil {
		t.Fatal("request key reuse accepted")
	}
	other, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Other"})
	must(t, e)
	if _, e = m.CreateComment(ctx, f.token, other.ID, comments.Create{Body: "Wrong task", ReplyTo: first.ID, RequestID: uuid.NewString()}); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal(e)
	}
	_, outsiderF := permissionMember(t, root, "outsider-comment")
	if _, e = manager(outsiderF).Comment(ctx, outsiderF.token, task.ID, first.ID); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("outsider read", e)
	}
	w = workspaceGrants(t, root, w, owner.ID, false, []string{ws.EditTasks})
	_, e = m.CreateComment(ctx, f.token, task.ID, comments.Create{Body: "No permission", RequestID: uuid.NewString()})
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal(e)
	}
	visible, e := m.Comment(ctx, f.token, task.ID, first.ID)
	must(t, e)
	if visible.Comment.CanEdit {
		t.Fatal("edit capability survived revocation")
	}
	_, e = m.UpdateComment(ctx, f.token, task.ID, first.ID, comments.Update{Body: "No", Version: 1})
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal(e)
	}
	w = workspaceGrants(t, root, w, owner.ID, false, []string{ws.CommentTasks})
	theirs := postComment(t, root, task.ID, "Admin's comment", "")
	_, e = m.UpdateComment(ctx, f.token, task.ID, theirs.ID, comments.Update{Body: "No", Version: 1})
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal(e)
	}
	w = workspaceGrants(t, root, w, owner.ID, false, []string{ws.ManageComments})
	_, e = m.UpdateComment(ctx, f.token, task.ID, theirs.ID, comments.Update{Body: "Allowed", Version: 1})
	must(t, e)
	_, e = m.UpdateComment(ctx, f.token, task.ID, first.ID, comments.Update{Body: "Own still needs commenting", Version: 1})
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal(e)
	}
	// Rejecting event persistence also rolls back a content update.
	_, e = root.conn.Exec(ctx, `CREATE FUNCTION fail_comment_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected'; END; $$; CREATE TRIGGER fail_comment_event BEFORE INSERT ON activity_events FOR EACH ROW EXECUTE FUNCTION fail_comment_event()`)
	must(t, e)
	_, e = manager(root).UpdateComment(ctx, root.token, task.ID, first.ID, comments.Update{Body: "Must rollback", Version: 1})
	if e == nil {
		t.Fatal("injection ignored")
	}
	current, e := m.Comment(ctx, f.token, task.ID, first.ID)
	must(t, e)
	if current.Comment.Body != "Once" || current.Comment.Version != 1 {
		t.Fatal(current)
	}
}
func TestCommentRepliesPaginationAndGroupingBarrier(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	w, e := m.CreateWorkspace(ctx, f.token, "Comment pages", "comment-pages", "")
	must(t, e)
	task, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Before"})
	must(t, e)
	root := postComment(t, f, task.ID, "Thread", "")
	task, e = patchTask(t, f, task, "title", "First")
	must(t, e)
	prior := history(t, f, task.ID).Entries[0]
	_, e = m.UpdateComment(ctx, f.token, task.ID, root.ID, comments.Update{Body: "Updated older comment", Version: 1})
	must(t, e)
	task, e = patchTask(t, f, task, "title", "Second")
	must(t, e)
	if history(t, f, task.ID).Entries[0].ID == prior.ID {
		t.Fatal("editing older comment failed to break grouping")
	}
	for i := 0; i < 53; i++ {
		postComment(t, f, task.ID, fmt.Sprintf("Reply %d", i), root.ID)
	}
	page, e := m.CommentReplies(ctx, f.token, task.ID, root.ID, "")
	must(t, e)
	if len(page.Entries) != 50 || !page.More || page.Cursor == "" {
		t.Fatal(page)
	}
	older, e := m.CommentReplies(ctx, f.token, task.ID, root.ID, page.Cursor)
	must(t, e)
	if len(older.Entries) != 3 || older.More {
		t.Fatal(older)
	}
	if len(history(t, f, task.ID).Entries) != 4 {
		t.Fatal("replies leaked into main timeline")
	}
}

func TestConcurrentCommentRetriesAndVersions(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	w, e := m.CreateWorkspace(ctx, f.token, "Concurrent comments", "concurrent-comments", "")
	must(t, e)
	task, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Concurrent"})
	must(t, e)
	in := comments.Create{Body: "One logical post", RequestID: uuid.NewString()}
	var wg sync.WaitGroup
	results := make([]activity.Entry, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Go(func() { results[i], errs[i] = m.CreateComment(ctx, f.token, task.ID, in) })
	}
	wg.Wait()
	for _, e := range errs {
		must(t, e)
	}
	if results[0].ID != results[1].ID {
		t.Fatal("parallel retry duplicated comment")
	}
	for i := range 2 {
		wg.Go(func() {
			_, errs[i] = m.UpdateComment(ctx, f.token, task.ID, results[0].ID, comments.Update{Body: fmt.Sprintf("Edit %d", i), Version: 1})
		})
	}
	wg.Wait()
	successes := 0
	for _, e := range errs {
		if e == nil {
			successes++
		} else {
			var conflict *tasks.Conflict
			if !errors.As(e, &conflict) {
				t.Fatal(e)
			}
		}
	}
	if successes != 1 {
		t.Fatal("concurrent edits both won")
	}
}

func TestLatestReplyFindsAnOlderThread(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	w, e := m.CreateWorkspace(ctx, f.token, "Older thread", "older-thread", "")
	must(t, e)
	task, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "A"})
	must(t, e)
	root := postComment(t, f, task.ID, "Old root", "")
	for i := range 52 {
		postComment(t, f, task.ID, fmt.Sprintf("Other thread %d", i), "")
	}
	reply := postComment(t, f, task.ID, "New reply on old thread", root.ID)
	page := history(t, f, task.ID)
	if !page.More || page.LatestEntry != root.ID || page.LatestEvent != reply.Last {
		t.Fatal(page)
	}
	for _, entry := range page.Entries {
		if entry.ID == root.ID {
			t.Fatal("root unexpectedly on first page")
		}
	}
	old, e := m.TaskActivity(ctx, f.token, task.ID, page.Cursor)
	must(t, e)
	if old.LatestEntry != root.ID {
		t.Fatal("latest metadata lost while paging")
	}
	agent := agentAccount(t, f, "commenter", nil)
	token := agentCLI(t, f, agent.ID)
	agentComment, e := m.CreateComment(ctx, token, task.ID, comments.Create{Body: "Agent is a distinct author", ReplyTo: root.ID, RequestID: uuid.NewString()})
	must(t, e)
	read, e := m.Comment(ctx, f.token, task.ID, agentComment.ID)
	must(t, e)
	if !read.Unread || read.Actor.ID != agent.ID || read.Actor.OwnerID == "" {
		t.Fatal("agent reply not distinct", read)
	}
}
