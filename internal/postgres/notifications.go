package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"acta/internal/notifications"
)

func (s *Store) Notifications(ctx context.Context, owner string) (notifications.NotificationPage, error) {
	out := notifications.NotificationPage{
		Items:  []notifications.Notification{},
		Counts: map[string]int{},
	}
	tx, err := s.pool.BeginTx(
		ctx,
		pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly},
	)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	allowed, err := notificationWorkspaces(ctx, tx, owner)
	if err != nil {
		return out, err
	}
	rows, err := tx.Query(
		ctx,
		`SELECT n.task_id::text,count(*) FROM notifications n LEFT JOIN tasks task ON task.id=n.task_id WHERE n.owner_id=$1 AND n.read_at IS NULL AND n.resolved_at IS NULL AND task.workspace_id::text=ANY($2) GROUP BY n.task_id`,
		owner,
		allowed,
	)
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
	rows, err = tx.Query(
		ctx,
		`SELECT `+noticeColumns+noticeJoins+` WHERE n.owner_id=$1 AND n.read_at IS NULL AND n.resolved_at IS NULL AND task.workspace_id::text=ANY($2) ORDER BY n.revision DESC LIMIT 200`,
		owner,
		allowed,
	)
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
func (s *Store) ReadNotifications(ctx context.Context, owner string, reads []notifications.NotificationRead) error {
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

var _ notifications.NotificationStore = (*Store)(nil)

const noticeColumns = `n.id::text,n.revision,n.source_sequence,n.kind,n.title,n.created_at,n.task_id::text,w.slug,c.prefix||'-'||task.number,task.title,COALESCE(n.activity_id::text,'')`
const noticeJoins = ` FROM notifications n JOIN tasks task ON task.id=n.task_id JOIN workspaces w ON w.id=task.workspace_id JOIN task_settings c ON c.workspace_id=w.id`

func scanNotice(row pgx.Row) (notifications.Notification, error) {
	var n notifications.Notification
	err := row.Scan(
		&n.ID,
		&n.Revision,
		&n.Sequence,
		&n.Kind,
		&n.Title,
		&n.CreatedAt,
		&n.TaskID,
		&n.WorkspaceSlug,
		&n.TaskReference,
		&n.TaskTitle,
		&n.ActivityID,
	)
	return n, err
}
