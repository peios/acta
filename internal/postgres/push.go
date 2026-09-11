package postgres

import (
	"acta/internal/auth"
	"acta/internal/push"
	"acta/internal/threads"
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Store) SavePushSubscription(ctx context.Context, owner string, digest []byte, sub push.Subscription) (string, error) {
	if err := push.Validate(sub); err != nil {
		return "", err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	// Use authentication's account-then-session order, also serializing the cap.
	var active string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM accounts WHERE id=$1 AND parent_id IS NULL AND disabled_at IS NULL AND NOT pending FOR UPDATE`, owner).Scan(&active); err != nil {
		return "", err
	}
	var session string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM browser_sessions WHERE account_id=$1 AND token_hash=$2 AND kind='browser' AND expires_at>now() AND last_seen_at>$3 FOR UPDATE`, owner, digest, time.Now().Add(-auth.SessionIdle)).Scan(&session); err != nil {
		return "", err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE owner_id=$1 AND endpoint<>$2`, owner, sub.Endpoint).Scan(&count); err != nil {
		return "", err
	}
	if count >= 16 {
		return "", push.ErrSubscription
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO push_subscriptions(id,owner_id,session_id,endpoint,p256dh,auth) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(endpoint) DO UPDATE SET session_id=EXCLUDED.session_id,p256dh=EXCLUDED.p256dh,auth=EXCLUDED.auth WHERE push_subscriptions.owner_id=EXCLUDED.owner_id RETURNING id::text`, uuid.NewString(), owner, session, sub.Endpoint, sub.Keys.P256dh, sub.Keys.Auth).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", push.ErrSubscription
	}
	if err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}
func (s *Store) DeletePushSubscription(ctx context.Context, owner string, digest []byte, endpoint string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM push_subscriptions p USING browser_sessions b WHERE p.owner_id=$1 AND p.endpoint=$2 AND b.id=p.session_id AND b.token_hash=$3`, owner, endpoint, digest)
	return err
}
func (s *Store) PushNotice(ctx context.Context, owner string, digest []byte, subscription, id string, revision int64) (threads.Notification, error) {

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return threads.Notification{}, err
	}
	defer tx.Rollback(ctx)
	allowed, err := notificationWorkspaces(ctx, tx, owner)
	if err != nil {
		return threads.Notification{}, err
	}
	n, err := scanNotice(tx.QueryRow(ctx, `SELECT `+noticeColumns+noticeJoins+` JOIN push_subscriptions p ON p.owner_id=n.owner_id JOIN browser_sessions b ON b.id=p.session_id
 WHERE n.owner_id=$1 AND b.token_hash=$2 AND p.id=$3 AND n.id=$4 AND n.revision=$5 AND n.read_at IS NULL AND n.resolved_at IS NULL AND (n.task_id IS NULL OR task.workspace_id::text=ANY($6))`, owner, digest, subscription, id, revision, allowed))
	if errors.Is(err, pgx.ErrNoRows) {
		err = auth.ErrNotFound
	}
	if err != nil {
		return n, err
	}
	return n, tx.Commit(ctx)
}

func (s *Store) ClaimPush(ctx context.Context) (*push.Delivery, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	// Prune old receipts and cancel work whose notification or session changed.
	if _, err = tx.Exec(ctx, `DELETE FROM push_deliveries WHERE expires_at<now()-interval '7 days'`); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE push_deliveries d SET done=true WHERE NOT d.done AND (d.expires_at<=now() OR NOT EXISTS (
 SELECT 1 FROM notifications n JOIN push_subscriptions p ON p.id=d.subscription_id JOIN browser_sessions b ON b.id=p.session_id JOIN accounts a ON a.id=p.owner_id
 WHERE n.id=d.notification_id AND n.revision=d.revision AND n.read_at IS NULL AND n.resolved_at IS NULL AND a.disabled_at IS NULL AND NOT a.pending AND b.expires_at>now() AND b.last_seen_at>$1))`, time.Now().Add(-auth.SessionIdle)); err != nil {
		return nil, err
	}
	var d push.Delivery
	err = tx.QueryRow(ctx, `SELECT d.id::text,d.subscription_id::text,d.notification_id::text,d.revision,p.endpoint,p.p256dh,p.auth FROM push_deliveries d JOIN push_subscriptions p ON p.id=d.subscription_id WHERE NOT d.done AND d.due_at<=now() ORDER BY d.due_at,d.id LIMIT 1 FOR UPDATE OF d SKIP LOCKED`).Scan(&d.ID, &d.SubscriptionID, &d.NotificationID, &d.Revision, &d.Subscription.Endpoint, &d.Subscription.Keys.P256dh, &d.Subscription.Keys.Auth)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, tx.Commit(ctx)
	}
	if err != nil {
		return nil, err
	}
	d.Lease = uuid.NewString()
	if _, err = tx.Exec(ctx, `UPDATE push_deliveries SET lease=$2,due_at=now()+interval '1 minute',attempts=attempts+1 WHERE id=$1`, d.ID, d.Lease); err != nil {
		return nil, err
	}
	return &d, tx.Commit(ctx)
}
func (s *Store) FinishPush(ctx context.Context, d push.Delivery, status int) error {
	if status == 404 || status == 410 {
		_, err := s.pool.Exec(ctx, `DELETE FROM push_subscriptions p USING push_deliveries d WHERE d.id=$1 AND d.lease=$2 AND p.id=d.subscription_id`, d.ID, d.Lease)
		return err
	}
	done := status >= 200 && status < 300 || status >= 400 && status < 500 && status != 408 && status != 429
	_, err := s.pool.Exec(ctx, `UPDATE push_deliveries SET done=$3,lease=NULL,due_at=now()+make_interval(secs=>LEAST(3600,30*power(2,LEAST(attempts,7)))::double precision) WHERE id=$1 AND lease=$2`, d.ID, d.Lease, done)
	return err
}

var _ push.Store = (*Store)(nil)
