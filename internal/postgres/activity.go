package postgres

import (
	"acta/internal/accounts"
	"acta/internal/activity"
	"acta/internal/auth"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

// The surrounding workspace write transaction serializes mutations. Recording
// and updating the presentation projection must commit with the domain change.
func (t workspaceTx) recordActivity(ctx context.Context, workspace, subjectType, subject string, change activity.Change, mentions ...string) error {
	if isMigration(ctx) {
		return nil
	}
	var id, actor string
	var lastAt time.Time
	var prior activity.Change
	e := t.tx.QueryRow(ctx, `SELECT e.id::text,e.actor_id::text,e.updated_at,e.change FROM activity_events v JOIN activity_entries e ON e.id=v.entry_id WHERE v.subject_type=$1 AND v.subject_id=$2 ORDER BY v.id DESC LIMIT 1`, subjectType, subject).Scan(&id, &actor, &lastAt, &prior)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	var now time.Time
	if e = t.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	merge := id != "" && activity.CanGroup(prior, change, t.actor.ID, actor, now, lastAt)
	if !merge {
		id = uuid.NewString()
	}
	raw, e := json.Marshal(change)
	if e != nil {
		return e
	}
	var event int64
	e = t.tx.QueryRow(ctx, `INSERT INTO activity_events(entry_id,workspace_id,subject_type,subject_id,actor_id,occurred_at,change) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, id, workspace, subjectType, subject, t.actor.ID, now, raw).Scan(&event)
	if e != nil {
		return e
	}
	if merge {
		prior.After = change.After
		raw, e = json.Marshal(prior)
		if e != nil {
			return e
		}
		_, e = t.tx.Exec(ctx, `UPDATE activity_entries SET last_event=$2,updated_at=$3,event_count=event_count+1,change=$4 WHERE id=$1`, id, event, now, raw)
	} else {
		_, e = t.tx.Exec(ctx, `INSERT INTO activity_entries(id,workspace_id,subject_type,subject_id,actor_id,first_event,last_event,started_at,updated_at,event_count,change) VALUES($1,$2,$3,$4,$5,$6,$6,$7,$7,1,$8)`, id, workspace, subjectType, subject, t.actor.ID, event, now, raw)
	}
	if e != nil {
		return e
	}
	if subjectType == "task" {
		task, err := t.TaskGet(ctx, subject)
		if err != nil {
			return err
		}
		if change.Kind == "task.created" {
			mentions = newMentions("", task.Description)
		}
		return t.notifyTask(ctx, task, id, event, change, mentions, "")
	}
	return nil
}

func (t workspaceTx) TaskActivity(ctx context.Context, task string, before int64) (activity.Page, error) {
	return t.activityPage(ctx, task, "", before, false, "")
}
func (t workspaceTx) ReadTaskActivity(ctx context.Context, task string, seen []activity.Seen) error {
	for _, s := range seen {
		through, e := strconv.ParseInt(s.Through, 10, 64)
		if e != nil || through < 1 {
			return &accounts.FieldError{Field: "entries", Message: "Supply the event position displayed by each activity entry."}
		}
		var exists bool
		e = t.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM activity_entries e JOIN activity_events v ON v.entry_id=e.id WHERE e.subject_type='task' AND e.subject_id=$1 AND e.id::text=$2 AND v.id=$3)`, task, s.ID, through).Scan(&exists)
		if e != nil {
			return e
		}
		if !exists {
			return auth.ErrNotFound
		}
		_, e = t.tx.Exec(ctx, `INSERT INTO activity_reads(account_id,entry_id,through_event) VALUES($1,$2,$3) ON CONFLICT(account_id,entry_id) DO UPDATE SET through_event=GREATEST(activity_reads.through_event,EXCLUDED.through_event)`, t.actor.ID, s.ID, through)
		if e != nil {
			return e
		}

		if !t.actor.IsAgent() {
			_, e = t.tx.Exec(ctx, `UPDATE notifications SET read_at=COALESCE(read_at,now()) WHERE owner_id=$1 AND task_id=$2 AND activity_id=$3 AND source_sequence<=$4`, t.actor.ID, task, s.ID, through)
			if e != nil {
				return e
			}
		}
	}
	return nil
}
