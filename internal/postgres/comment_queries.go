package postgres

import (
	"acta/internal/activity"
	"acta/internal/auth"
	"context"
)

// The latest event actor determines unread state, including a moderator editing
// somebody else's comment. Replies have independent acknowledgements.
const activityUnread = `(v.actor_id<>$2::uuid AND e.last_event>COALESCE(r.through_event,0))`

func (t workspaceTx) activityPage(ctx context.Context, task, thread string, before int64, replies bool, id string) (activity.Page, error) {
	out := activity.Page{Entries: []activity.Entry{}}
	if !replies && id == "" {
		e := t.tx.QueryRow(ctx, `SELECT COALESCE((SELECT COALESCE(e.thread_id,e.id)::text FROM activity_entries e WHERE e.subject_type='task' AND e.subject_id=$1 ORDER BY e.last_event DESC LIMIT 1),''),COALESCE((SELECT max(last_event)::text FROM activity_entries WHERE subject_type='task' AND subject_id=$1),'')`, task).Scan(&out.LatestEntry, &out.LatestEvent)
		if e != nil {
			return out, e
		}
	}

	e := t.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM activity_entries e JOIN activity_events v ON v.id=e.last_event LEFT JOIN activity_reads r ON r.entry_id=e.id AND r.account_id=$2 WHERE e.subject_type='task' AND e.subject_id=$1 AND ($3::text='' OR e.thread_id::text=$3) AND `+activityUnread+`)`, task, t.actor.ID, thread).Scan(&out.Unread)
	if e != nil {
		return out, e
	}
	rows, e := t.tx.Query(ctx, `SELECT e.id::text,e.first_event::text,e.last_event::text,a.id::text,CASE WHEN p.id IS NULL THEN a.username ELSE p.username||'/'||a.username END,a.display_name,COALESCE(a.parent_id::text,''),e.started_at,e.updated_at,e.event_count,e.change,`+activityUnread+`,
 CASE WHEN c.id IS NULL THEN NULL ELSE jsonb_build_object('body',c.body,'version',c.version,'deleted',c.deleted,'edited',c.edited,'thread_id',COALESCE(e.thread_id::text,''),'reply_to',COALESCE(c.reply_to::text,'')) END,
 COALESCE(replies.count,0),COALESCE(replies.unread,false),COALESCE(replies.last::text,'')
 FROM activity_entries e JOIN activity_events v ON v.id=e.last_event JOIN accounts a ON a.id=e.actor_id LEFT JOIN accounts p ON p.id=a.parent_id LEFT JOIN activity_reads r ON r.entry_id=e.id AND r.account_id=$2 LEFT JOIN task_comments c ON c.id=e.id
 LEFT JOIN LATERAL (SELECT count(*) AS count,max(child.last_event) AS last,bool_or(cv.actor_id<>$2::uuid AND child.last_event>COALESCE(cr.through_event,0)) AS unread FROM activity_entries child JOIN activity_events cv ON cv.id=child.last_event LEFT JOIN activity_reads cr ON cr.entry_id=child.id AND cr.account_id=$2 WHERE child.thread_id=e.id) replies ON c.id IS NOT NULL AND e.thread_id IS NULL
 WHERE e.subject_type='task' AND e.subject_id=$1 AND ($6::text<>'' AND e.id::text=$6 OR $6::text='' AND (($5 AND e.thread_id::text=$4) OR (NOT $5 AND e.thread_id IS NULL))) AND ($3::bigint=0 OR e.first_event<$3) ORDER BY e.first_event DESC LIMIT 51`, task, t.actor.ID, before, thread, replies, id)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v activity.Entry
		if e = rows.Scan(&v.ID, &v.First, &v.Last, &v.Actor.ID, &v.Actor.Username, &v.Actor.DisplayName, &v.Actor.OwnerID, &v.StartedAt, &v.UpdatedAt, &v.Count, &v.Change, &v.Unread, &v.Comment, &v.ReplyCount, &v.ThreadUnread, &v.ThreadLast); e != nil {
			return out, e
		}
		out.Entries = append(out.Entries, v)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if len(out.Entries) > activity.PageSize {
		out.More = true
		out.Entries = out.Entries[:activity.PageSize]
		out.Cursor = out.Entries[len(out.Entries)-1].First
	}
	return out, nil
}
func (t workspaceTx) CommentGet(ctx context.Context, task, id string) (activity.Entry, error) {
	if id == "" {
		return activity.Entry{}, auth.ErrNotFound
	}
	p, e := t.activityPage(ctx, task, "", 0, false, id)
	if e != nil {
		return activity.Entry{}, e
	}
	if len(p.Entries) != 1 || p.Entries[0].Comment == nil {
		return activity.Entry{}, auth.ErrNotFound
	}
	return p.Entries[0], nil
}
func (t workspaceTx) CommentReplies(ctx context.Context, task, id string, before int64) (activity.Page, error) {
	return t.activityPage(ctx, task, id, before, true, "")
}
