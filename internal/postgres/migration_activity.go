package postgres

import (
	"acta2/internal/activity"
	"acta2/internal/comments"
	"acta2/internal/migration"
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

func (t workspaceTx) migrationEvent(ctx context.Context, id, w, task, thread, author string, change activity.Change, created, updated time.Time) (string, error) {
	newEntry := id == ""
	if newEntry {
		id = uuid.NewString()
	}
	raw, _ := json.Marshal(change)
	var event int64
	e := t.tx.QueryRow(ctx, `INSERT INTO activity_events(entry_id,workspace_id,subject_type,subject_id,actor_id,occurred_at,change) VALUES($1,$2,'task',$3,$4,$5,$6) RETURNING id`, id, w, task, t.actor.ID, updated, raw).Scan(&event)
	if e != nil {
		return "", e
	}
	if newEntry {
		_, e = t.tx.Exec(ctx, `INSERT INTO activity_entries(id,workspace_id,subject_type,subject_id,actor_id,first_event,last_event,started_at,updated_at,event_count,change,thread_id) VALUES($1,$2,'task',$3,$4,$5,$5,$6,$7,1,$8,NULLIF($9,'')::uuid)`, id, w, task, author, event, created, updated, raw, thread)
	} else {
		_, e = t.tx.Exec(ctx, `UPDATE activity_entries SET actor_id=$2,last_event=$3,started_at=$4,updated_at=$5,event_count=event_count+1,change=$6 WHERE id=$1`, id, author, event, created, updated, raw)
	}
	if e != nil {
		return "", e
	}
	return id, t.taskChanged(ctx, w)
}
func (t workspaceTx) migrationActivity(ctx context.Context, op string, in migration.Request, now time.Time) (string, error) {
	f := migrationFields(in.Fields)
	taskID, author, body, reply, thread := "", t.actor.ID, "", "", ""
	created := now
	change := activity.Change{Kind: "comment.created"}
	var e error
	if op == "edit" {
		e = t.tx.QueryRow(ctx, `SELECT subject_id::text,actor_id::text,started_at,change,COALESCE(thread_id::text,'') FROM activity_entries WHERE id=$1`, in.ID).Scan(&taskID, &author, &created, &change, &thread)
		if e != nil {
			return "", e
		}
		if in.Kind == "comment" {
			var deleted bool
			e = t.tx.QueryRow(ctx, `SELECT body,author_id::text,deleted FROM task_comments WHERE id=$1`, in.ID).Scan(&body, &author, &deleted)
			if e != nil {
				return "", e
			}
			if deleted {
				return "", migration.Invalid("id", "Deleted comments cannot be rewritten.")
			}
			change = activity.Change{Kind: "comment.edited"}
		}
	}
	taskID, e = f.text("task_id", taskID)
	if e != nil {
		return "", e
	}
	task, e := t.TaskGet(ctx, taskID)
	if e != nil {
		return "", e
	}
	created, updated, e := f.dates(created, now)
	if e != nil {
		return "", e
	}
	if in.Kind == "comment" {
		author, e = t.migrationAuthor(ctx, f, "author_id", author)
		if e != nil {
			return "", e
		}
		body, e = f.text("body", body)
		if e != nil {
			return "", e
		}
		body, e = comments.Body(body)
		if e != nil {
			return "", e
		}
		reply, e = f.nullable("reply_to", reply)
		if e != nil {
			return "", e
		}
		if reply != "" {
			p, e := t.CommentGet(ctx, task.ID, reply)
			if e != nil {
				return "", e
			}
			thread = p.ID
			if p.Comment.ThreadID != "" {
				thread = p.Comment.ThreadID
			}
		}
	} else {
		raw, ok := f["change"]
		if op == "create" && !ok {
			return "", migration.Invalid("change", "Supply an activity change.")
		}
		if ok {
			change = activity.Change{}
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&change) != nil {
				return "", migration.Invalid("change", "Invalid activity change.")
			}
		}
		switch change.Kind {
		case "task.created", "task.changed", "task.archived", "task.restored", "document.created", "document.updated", "document.deleted":
		default:
			return "", migration.Invalid("change", "Use a recognized task/document activity kind.")
		}
		author = t.actor.ID
	}
	id, e := t.migrationEvent(ctx, in.ID, task.WorkspaceID, task.ID, thread, author, change, created, updated)
	if e != nil {
		return "", e
	}
	if in.Kind == "comment" {
		if op == "create" {
			_, e = t.tx.Exec(ctx, `INSERT INTO task_comments(id,task_id,author_id,body,reply_to,request_id,request_hash) VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,'migration')`, id, task.ID, author, body, reply, uuid.NewString())
		} else {
			_, e = t.tx.Exec(ctx, `UPDATE task_comments SET body=$2,author_id=$3,version=version+1,edited=true WHERE id=$1`, id, body, author)
		}
	}
	return id, e
}
