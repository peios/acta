package postgres

import (
	"acta/internal/activity"
	"acta/internal/auth"
	"acta/internal/tasks"
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"strings"
)

func (t workspaceTx) TaskFollowing(ctx context.Context, task string) (bool, error) {
	var v bool
	e := t.tx.QueryRow(ctx, `SELECT COALESCE((SELECT following FROM task_followers WHERE task_id=$1 AND owner_id=$2),false)`, task, t.actor.ID).Scan(&v)
	return v, e
}
func (t workspaceTx) SetTaskFollowing(ctx context.Context, task string, v bool) error {
	_, e := t.tx.Exec(ctx, `INSERT INTO task_followers(task_id,owner_id,following) VALUES($1,$2,$3) ON CONFLICT(task_id,owner_id) DO UPDATE SET following=EXCLUDED.following`, task, t.actor.ID, v)
	return e
}
func (t workspaceTx) autoFollow(ctx context.Context, task, account string) error {
	_, e := t.tx.Exec(ctx, `INSERT INTO task_followers(task_id,owner_id,following) SELECT $1,COALESCE(parent_id,id),true FROM accounts WHERE id=$2 ON CONFLICT DO NOTHING`, task, account)
	return e
}

// Mention identities come from resolved Markdown links, never code blocks or
// substring matches. Compare revisions so editing unrelated prose cannot re-ping.
func mentionIDs(body string) map[string]bool {
	ids := map[string]bool{}
	doc := goldmark.New().Parser().Parse(text.NewReader([]byte(body)))
	ast.Walk(doc, func(n ast.Node, enter bool) (ast.WalkStatus, error) {
		if enter {
			if link, ok := n.(*ast.Link); ok {
				id := strings.TrimPrefix(string(link.Destination), "/references/accounts/")
				if id != string(link.Destination) {
					if _, e := uuid.Parse(id); e == nil {
						ids[id] = true
					}
				}
			}
		}
		return ast.WalkContinue, nil
	})
	return ids
}
func newMentions(before, after string) []string {
	old := mentionIDs(before)
	out := []string{}
	for id := range mentionIDs(after) {
		if !old[id] {
			out = append(out, id)
		}
	}
	return out
}

// Only human accounts receive inbox records. Agents' actions and assignments
// resolve to their owner, while self-action suppression uses the actual actor.
func (t workspaceTx) notifyTask(ctx context.Context, task tasks.Task, entry string, event int64, change activity.Change, mentions []string, replyAuthor string) error {
	if isMigration(ctx) {
		return nil
	}
	recipients := map[string]string{}
	add := func(account, title string) error {
		a, e := t.Account(ctx, account)
		if errors.Is(e, pgx.ErrNoRows) || errors.Is(e, auth.ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		if a.IsAgent() {
			if a.Owner == nil {
				return nil
			}
			a = *a.Owner
		}
		if a.ID == t.actor.ID || !a.Available() {
			return nil
		}
		access, e := t.Access(ctx, a, task.WorkspaceID)
		if e != nil {
			return e
		}
		if !access.Allowed {
			return nil
		}
		recipients[a.ID] = title
		return nil
	}
	if change.Kind == "task.created" {
		if e := t.autoFollow(ctx, task.ID, t.actor.ID); e != nil {
			return e
		}
	}
	assigned := []string{}
	if change.Kind == "task.created" || change.Field == "assignees" {
		old := map[string]bool{}
		for _, p := range change.Before.Items {
			old[p.ID] = true
		}
		for _, p := range task.Assignees {
			if !old[p.ID] {
				assigned = append(assigned, p.ID)
				if e := t.autoFollow(ctx, task.ID, p.ID); e != nil {
					return e
				}
			}
		}
	}
	rows, e := t.tx.Query(ctx, `SELECT owner_id::text FROM task_followers WHERE task_id=$1 AND following`, task.ID)
	if e != nil {
		return e
	}
	followers := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		followers = append(followers, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	verb := "Updated task"
	switch change.Kind {
	case "task.archived":
		verb = "Archived task"
	case "task.restored":
		verb = "Restored task"
	case "task.created":
		verb = "Created task"
	case "comment.created":
		verb = "Commented"
	case "comment.edited":
		verb = "Edited a comment"
	case "comment.deleted":
		verb = "Deleted a comment"
	case "document.created":
		verb = "Added a document"
	case "document.version":
		verb = "Updated a document"
	case "document.deleted":
		verb = "Deleted a document"
	}
	if change.Kind == "task.changed" {
		verb = "Changed " + strings.TrimSuffix(change.Field, "_id")
		if change.Field == "parent_id" {
			verb = "Moved task"
		}
	}
	title := t.actor.Handle() + " · " + verb
	for _, id := range followers {
		if e = add(id, title); e != nil {
			return e
		}
	}
	for _, id := range assigned {
		if e = add(id, t.actor.Handle()+" · Assigned to you"); e != nil {
			return e
		}
	}
	if replyAuthor != "" {
		if e = add(replyAuthor, t.actor.Handle()+" · Replied to your comment"); e != nil {
			return e
		}
	}
	for _, id := range mentions {
		if e = add(id, t.actor.Handle()+" · Mentioned you"); e != nil {
			return e
		}
	}
	for owner, title := range recipients {
		_, e = t.tx.Exec(ctx, `WITH changed AS (
   INSERT INTO notifications(id,owner_id,task_id,activity_id,notice_key,kind,title,blocking,source_sequence)
   VALUES($1,$2,$3,$4::uuid,$4::text,'task',$5,false,$6)
   ON CONFLICT(owner_id,task_id,activity_id) WHERE task_id IS NOT NULL DO UPDATE SET
    title=EXCLUDED.title,revision=nextval('thread_notification_revision'),source_sequence=EXCLUDED.source_sequence,created_at=now(),read_at=NULL
   WHERE notifications.source_sequence<EXCLUDED.source_sequence RETURNING id,owner_id,revision)
   INSERT INTO push_deliveries(subscription_id,notification_id,revision)
   SELECT p.id,n.id,n.revision FROM changed n JOIN push_subscriptions p ON p.owner_id=n.owner_id
   JOIN browser_sessions b ON b.id=p.session_id WHERE b.expires_at>now() AND b.last_seen_at>now()-interval '7 days' ON CONFLICT DO NOTHING`, uuid.NewString(), owner, task.ID, entry, title, event)
		if e != nil {
			return e
		}
	}
	return nil
}

// Apply the normal workspace access resolver once per distinct workspace for
// inbox and push reads, including inherited Superuser rights and revocation.
func notificationWorkspaces(ctx context.Context, tx pgx.Tx, owner string) ([]string, error) {
	t := workspaceTx{tx: tx}
	a, e := t.Account(ctx, owner)
	if e != nil {
		return nil, e
	}
	if !a.Available() || a.IsAgent() {
		return nil, auth.ErrForbidden
	}
	rows, e := tx.Query(ctx, `SELECT DISTINCT task.workspace_id::text FROM notifications n JOIN tasks task ON task.id=n.task_id WHERE n.owner_id=$1 AND n.read_at IS NULL AND n.resolved_at IS NULL`, owner)
	if e != nil {
		return nil, e
	}
	candidates := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		candidates = append(candidates, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	allowed := []string{}
	for _, id := range candidates {
		access, e := t.Access(ctx, a, id)
		if e != nil {
			return nil, e
		}
		if access.Allowed {
			allowed = append(allowed, id)
		}
	}
	return allowed, nil
}
