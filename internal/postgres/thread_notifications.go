package postgres

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"acta2/internal/threads"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func notificationChange(ctx context.Context, tx pgx.Tx, owner, id string, sequence int64, n *threads.NotificationChange) error {
	if n == nil {
		return nil
	}
	if n.Resolve {
		_, err := tx.Exec(ctx, `UPDATE notifications SET resolved_at=COALESCE(resolved_at,now()) WHERE thread_id=$1 AND run_id=$2 AND lane_id=$3 AND notice_key=$4`, id, n.RunID, n.LaneID, n.Key)
		return err
	}
	_, err := tx.Exec(ctx, `WITH changed AS (INSERT INTO notifications(id,owner_id,thread_id,run_id,lane_id,notice_key,turn_id,kind,title,blocking,source_sequence) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
 ON CONFLICT(thread_id,run_id,lane_id,notice_key) DO UPDATE SET kind=EXCLUDED.kind,title=EXCLUDED.title,revision=nextval('thread_notification_revision'),created_at=now(),read_at=NULL,source_sequence=EXCLUDED.source_sequence
 WHERE notifications.kind<>EXCLUDED.kind AND notifications.resolved_at IS NULL
 RETURNING id,owner_id,revision)
 INSERT INTO push_deliveries(subscription_id,notification_id,revision)
 SELECT p.id,n.id,n.revision FROM changed n JOIN push_subscriptions p ON p.owner_id=n.owner_id
 JOIN browser_sessions b ON b.id=p.session_id WHERE b.expires_at>now() AND b.last_seen_at>now()-interval '7 days'
 ON CONFLICT DO NOTHING`, uuid.NewString(), owner, id, n.RunID, n.LaneID, n.Key, n.TurnID, n.Kind, n.Title, n.Blocking, sequence)
	return err
}

// Requests arriving after a restart, terminal descriptor, or already accepted
// answer are retained for diagnostics but must not generate stale attention.
func reconcileNotificationAttention(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `UPDATE notifications n SET resolved_at=now()
      FROM provider_threads t WHERE n.thread_id=t.id AND t.id=$1 AND n.kind='attention' AND n.resolved_at IS NULL AND (
        n.run_id::text<>t.descriptor->>'run_id' OR t.descriptor->>'state' NOT IN ('running','starting') OR EXISTS (
          SELECT 1 FROM provider_thread_commands c WHERE c.thread_id=n.thread_id AND c.outcome->>'outcome'='accepted'
          AND c.payload->>'run_id'=n.run_id::text AND COALESCE(c.payload->>'lane_id','')=n.lane_id
          AND ((c.payload->>'action'='approval' AND n.notice_key='approval/'||(c.payload->>'approval_id'))
            OR (c.payload->>'action'='answer' AND n.notice_key='question/'||(c.payload->>'question_id')))))`, id)
	return err
}
func frameNotifications(ctx context.Context, tx pgx.Tx, owner, id string, f threads.Frame) error {
	if err := notificationChange(ctx, tx, owner, id, f.Sequence, threads.FrameNotification(f)); err != nil {
		return err
	}
	if f.Kind == "approval/request" || f.Kind == "question/request" {
		return reconcileNotificationAttention(ctx, tx, id)
	}
	if f.Kind == "turn/completed" {
		var d struct {
			TurnID string `json:"turn_id"`
		}
		if err := json.Unmarshal(f.Data, &d); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE notifications SET resolved_at=COALESCE(resolved_at,now()) WHERE thread_id=$1 AND run_id=$2 AND lane_id=$3 AND turn_id=$4 AND kind='attention' AND blocking`, id, f.RunID, f.LaneID, d.TurnID)
		return err
	}
	return nil
}
func providerExitNotification(ctx context.Context, tx pgx.Tx, owner string, prior, next threads.Descriptor) error {
	if next.Revision <= prior.Revision {
		return nil
	}
	if next.RunID != prior.RunID {
		// Old-run permissions cannot be answered in a replacement provider process.
		if _, err := tx.Exec(ctx, `UPDATE notifications SET resolved_at=COALESCE(resolved_at,now()) WHERE thread_id=$1 AND run_id<>$2 AND kind='attention'`, next.ID, next.RunID); err != nil {
			return err
		}
		return nil
	}
	if (prior.State != "running" && prior.State != "starting") || (next.State != "exited" && next.State != "error" && next.State != "uncertain") {
		return nil
	}
	var killed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_thread_commands WHERE thread_id=$1 AND payload->>'action'='kill' AND payload->>'run_id'=$2 AND (outcome IS NULL OR COALESCE(outcome->>'outcome','')<>'rejected'))`, next.ID, next.RunID).Scan(&killed); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE notifications SET resolved_at=COALESCE(resolved_at,now()) WHERE thread_id=$1 AND run_id=$2 AND kind='attention'`, next.ID, next.RunID); err != nil {
		return err
	}
	if killed {
		return nil
	}
	return notificationChange(ctx, tx, owner, next.ID, 0, &threads.NotificationChange{Key: "provider-exit", RunID: next.RunID, Kind: "failed", Title: "Provider exited unexpectedly"})
}
func (s *Store) ThreadNotifications(ctx context.Context, owner string) (threads.NotificationPage, error) {
	out := threads.NotificationPage{Items: []threads.Notification{}, Counts: map[string]int{}}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	allowed, err := notificationWorkspaces(ctx, tx, owner)
	if err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT COALESCE(n.thread_id::text,n.task_id::text),count(*) FROM notifications n LEFT JOIN tasks task ON task.id=n.task_id WHERE n.owner_id=$1 AND n.read_at IS NULL AND n.resolved_at IS NULL AND (n.task_id IS NULL OR task.workspace_id::text=ANY($2)) GROUP BY n.thread_id,n.task_id`, owner, allowed)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id string
		var count int
		if err = rows.Scan(&id, &count); err != nil {
			rows.Close()
			return out, err
		}
		out.Counts[id] = count
		out.Total += count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, `SELECT `+noticeColumns+noticeJoins+` WHERE n.owner_id=$1 AND n.read_at IS NULL AND n.resolved_at IS NULL AND (n.task_id IS NULL OR task.workspace_id::text=ANY($2)) ORDER BY n.revision DESC LIMIT 200`, owner, allowed)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		n, e := scanNotice(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, n)
	}

	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (s *Store) ReadThreadNotifications(ctx context.Context, owner string, reads []threads.NotificationRead) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, r := range reads {
		if _, err = tx.Exec(ctx, `WITH seen AS (UPDATE notifications SET read_at=COALESCE(read_at,now()) WHERE owner_id=$1 AND id=$2 AND revision=$3 RETURNING activity_id,source_sequence)
 INSERT INTO activity_reads(account_id,entry_id,through_event) SELECT $1,activity_id,source_sequence FROM seen WHERE activity_id IS NOT NULL
 ON CONFLICT(account_id,entry_id) DO UPDATE SET through_event=GREATEST(activity_reads.through_event,EXCLUDED.through_event)`, owner, r.ID, r.Revision); err != nil {
			break
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var _ threads.NotificationStore = (*Store)(nil)

const noticeColumns = `n.id::text,n.revision,COALESCE(n.thread_id::text,''),n.lane_id,COALESCE(n.run_id::text,''),n.source_sequence,n.kind,n.title,t.descriptor->>'name',t.descriptor->>'cwd',n.created_at,COALESCE(n.task_id::text,''),COALESCE(w.slug,''),COALESCE(c.prefix||'-'||task.number,''),COALESCE(task.title,''),COALESCE(n.activity_id::text,'')`
const noticeJoins = ` FROM notifications n LEFT JOIN provider_threads t ON t.id=n.thread_id LEFT JOIN tasks task ON task.id=n.task_id LEFT JOIN workspaces w ON w.id=task.workspace_id LEFT JOIN task_settings c ON c.workspace_id=w.id`

func scanNotice(row pgx.Row) (threads.Notification, error) {
	var n threads.Notification
	var name, cwd *string
	e := row.Scan(&n.ID, &n.Revision, &n.ThreadID, &n.LaneID, &n.RunID, &n.Sequence, &n.Kind, &n.Title, &name, &cwd, &n.CreatedAt, &n.TaskID, &n.WorkspaceSlug, &n.TaskReference, &n.TaskTitle, &n.ActivityID)
	if n.ThreadID != "" {
		n.ThreadName = "Agent"
		if name != nil && *name != "" {
			n.ThreadName = *name
		} else if cwd != nil {
			n.ThreadName = filepath.Base(strings.ReplaceAll(*cwd, "\\", "/"))
		}
	}
	return n, e
}
