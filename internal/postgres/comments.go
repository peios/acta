package postgres

import (
	"acta2/internal/accounts"
	"acta2/internal/activity"
	"acta2/internal/comments"
	"acta2/internal/tasks"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (t workspaceTx) CommentCreate(ctx context.Context, task tasks.Task, in comments.Create, hash string) (activity.Entry, error) {
	var id, oldTask, oldHash string
	e := t.tx.QueryRow(ctx, `SELECT id::text,task_id::text,request_hash FROM task_comments WHERE author_id=$1 AND request_id=$2`, t.actor.ID, in.RequestID).Scan(&id, &oldTask, &oldHash)
	if e == nil {
		if oldTask != task.ID || oldHash != hash {
			return activity.Entry{}, &accounts.FieldError{Field: "request_id", Message: "This request ID was already used for another post. Use a new UUID."}
		}
		return t.CommentGet(ctx, task.ID, id)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return activity.Entry{}, e
	}
	thread := ""
	if in.ReplyTo != "" {
		parent, e := t.CommentGet(ctx, task.ID, in.ReplyTo)
		if e != nil {
			return activity.Entry{}, e
		}
		thread = parent.ID
		if parent.Comment.ThreadID != "" {
			thread = parent.Comment.ThreadID
		}
	}
	id = uuid.NewString()
	if e = t.commentEvent(ctx, task, id, thread, "comment.created", true); e != nil {
		return activity.Entry{}, e
	}
	_, e = t.tx.Exec(ctx, `INSERT INTO task_comments(id,task_id,author_id,body,reply_to,request_id,request_hash) VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7)`, id, task.ID, t.actor.ID, in.Body, in.ReplyTo, in.RequestID, hash)
	if e != nil {
		return activity.Entry{}, e
	}
	if e = t.commentNotification(ctx, task, id, "comment.created", "", in.Body, in.ReplyTo); e != nil {
		return activity.Entry{}, e
	}
	return t.CommentGet(ctx, task.ID, id)
}
func (t workspaceTx) CommentUpdate(ctx context.Context, task tasks.Task, old activity.Entry, in comments.Update) (activity.Entry, error) {
	c := old.Comment
	if in.Delete && c.Deleted || !in.Delete && !c.Deleted && in.Body == c.Body {
		return old, nil
	}
	if c.Version != in.Version || c.Deleted {
		return activity.Entry{}, &tasks.Conflict{Field: "comment", Version: c.Version, Value: c}
	}
	kind := "comment.edited"
	if in.Delete {
		kind = "comment.deleted"
		in.Body = ""
	}
	_, e := t.tx.Exec(ctx, `UPDATE task_comments SET body=$2,version=version+1,edited=edited OR NOT $3,deleted=$3 WHERE id=$1`, old.ID, in.Body, in.Delete)
	if e != nil {
		return activity.Entry{}, e
	}
	if e = t.commentEvent(ctx, task, old.ID, c.ThreadID, kind, false); e != nil {
		return activity.Entry{}, e
	}
	if e = t.commentNotification(ctx, task, old.ID, kind, c.Body, in.Body, ""); e != nil {
		return activity.Entry{}, e
	}
	return t.CommentGet(ctx, task.ID, old.ID)
}
func (t workspaceTx) commentEvent(ctx context.Context, task tasks.Task, id, thread, kind string, create bool) error {
	raw, _ := json.Marshal(activity.Change{Kind: kind})
	var event int64
	e := t.tx.QueryRow(ctx, `INSERT INTO activity_events(entry_id,workspace_id,subject_type,subject_id,actor_id,occurred_at,change) VALUES($1,$2,'task',$3,$4,clock_timestamp(),$5) RETURNING id`, id, task.WorkspaceID, task.ID, t.actor.ID, raw).Scan(&event)
	if e != nil {
		return e
	}
	if create {
		_, e = t.tx.Exec(ctx, `INSERT INTO activity_entries(id,workspace_id,subject_type,subject_id,actor_id,first_event,last_event,started_at,updated_at,event_count,change,thread_id) SELECT entry_id,workspace_id,subject_type,subject_id,actor_id,id,id,occurred_at,occurred_at,1,change,NULLIF($2,'')::uuid FROM activity_events WHERE id=$1`, event, thread)
	} else {
		_, e = t.tx.Exec(ctx, `UPDATE activity_entries SET last_event=$2,updated_at=(SELECT occurred_at FROM activity_events WHERE id=$2) WHERE id=$1`, id, event)
	}
	if e != nil {
		return e
	}
	return t.taskChanged(ctx, task.WorkspaceID)
}

func (t workspaceTx) commentNotification(ctx context.Context, task tasks.Task, id, kind, before, body, reply string) error {
	var event int64
	if e := t.tx.QueryRow(ctx, `SELECT last_event FROM activity_entries WHERE id=$1`, id).Scan(&event); e != nil {
		return e
	}
	author := ""
	if reply != "" {
		if e := t.tx.QueryRow(ctx, `SELECT author_id::text FROM task_comments WHERE id=$1`, reply).Scan(&author); e != nil {
			return e
		}
	}
	if kind == "comment.deleted" {
		if _, e := t.tx.Exec(ctx, `UPDATE notifications SET resolved_at=now() WHERE activity_id=$1`, id); e != nil {
			return e
		}
		return nil
	}
	return t.notifyTask(ctx, task, id, event, activity.Change{Kind: kind}, newMentions(before, body), author)
}
