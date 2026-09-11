// Package postgres implements auth's storage contracts using PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"acta/internal/accounts"
	"acta/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

var _ auth.Store = (*Store)(nil)

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("invalid ACTA_DATABASE_URL")
	}
	config.MaxConns = 8
	config.MinConns = 0
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	store := &Store{pool: pool}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("cannot connect to PostgreSQL; check ACTA_DATABASE_URL and database availability")
	}
	if err = store.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err = store.backfillConversations(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}
func (s *Store) Close() { s.pool.Close() }
func (s *Store) SetupComplete(ctx context.Context) (bool, error) {
	var complete bool
	err := s.pool.QueryRow(ctx, `SELECT completed_at IS NOT NULL FROM installation WHERE singleton`).Scan(&complete)
	return complete, err
}

func (s *Store) GrantSetup(ctx context.Context, hash []byte, expires time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var complete bool
	if err = tx.QueryRow(ctx, `SELECT completed_at IS NOT NULL FROM installation WHERE singleton FOR UPDATE`).Scan(&complete); err != nil {
		return err
	}
	if complete {
		return auth.ErrSetupComplete
	}
	if _, err = tx.Exec(ctx, `INSERT INTO setup_grants(token_hash,expires_at) VALUES($1,$2)`, hash, expires); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) SetupGrantValid(ctx context.Context, hash []byte, now time.Time) (bool, error) {
	var valid bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM setup_grants WHERE token_hash=$1 AND expires_at>$2)`, hash, now).Scan(&valid)
	return valid, err
}
func (s *Store) CompleteSetup(ctx context.Context, hash []byte, a accounts.Account, passwordHash string, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var complete bool
	if err = tx.QueryRow(ctx, `SELECT completed_at IS NOT NULL FROM installation WHERE singleton FOR UPDATE`).Scan(&complete); err != nil {
		return err
	}
	if complete {
		return auth.ErrSetupComplete
	}
	result, err := tx.Exec(ctx, `DELETE FROM setup_grants WHERE token_hash=$1 AND expires_at>$2`, hash, now)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return auth.ErrSetupGrant
	}
	// This operation always creates the root administrator, regardless of any
	// incidental fields on the supplied domain value.
	if _, err = tx.Exec(ctx, `INSERT INTO accounts(id,username,direct_permissions,created_at,display_name) VALUES($1,$2,ARRAY['site.superuser'],$3,$4)`, a.ID, a.Username, now, a.DisplayName); err != nil {
		return err
	}
	if err = assignDefaultGroup(ctx, tx, a.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO password_credentials(account_id,password_hash) VALUES($1,$2)`, a.ID, passwordHash); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE installation SET completed_at=$1,completed_by=$2 WHERE singleton`, now, a.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM setup_grants`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO account_events(account_id,kind,occurred_at) VALUES($1,'administrator_created',$2)`, a.ID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Read current and previous names in the same snapshot, including after a
// rename. The current name is a claim too, but is not part of the history list.
const accountGroupProjection = `COALESCE((SELECT jsonb_agg(jsonb_build_object('id',g.id,'name',g.name,'direct_permissions',g.permissions,'direct_require_mfa',g.require_mfa,'is_default',g.is_default,'permissions_version',g.version) ORDER BY lower(g.name),g.id) FROM group_memberships m JOIN permission_groups g ON g.id=m.group_id WHERE m.account_id=a.id),'[]'::jsonb)`

var accountColumns = `a.id::text,a.username,a.parent_id::text,COALESCE(p.username,''),a.display_name,a.direct_permissions,a.require_mfa,a.mfa_enrolled,a.permissions_version,a.profile_version,a.pending,a.disabled_at,a.created_at, (EXISTS(SELECT 1 FROM superuser_accounts su WHERE su.id=a.id) AND a.disabled_at IS NULL AND NOT a.pending AND NOT EXISTS(SELECT 1 FROM superuser_accounts su JOIN accounts other ON other.id=su.id WHERE other.id<>a.id AND other.disabled_at IS NULL AND NOT other.pending)),
 ARRAY(SELECT h.handle FROM account_handles h WHERE h.account_id=a.id AND h.handle<>CASE WHEN a.parent_id IS NULL THEN a.username ELSE p.username || '/' || a.username END ORDER BY h.handle),
 ` + accountGroupProjection + `,
 COALESCE((SELECT permissions FROM permission_groups WHERE is_default),'{}'::text[]),
 p.disabled_at,COALESCE(p.pending,false),COALESCE(p.direct_permissions,'{}'::text[]),COALESCE(p.require_mfa,false),COALESCE(p.mfa_enrolled,false),
 ` + strings.ReplaceAll(accountGroupProjection, "a.id", "p.id")

func scanAccount(row pgx.Row) (accounts.Account, error) {
	var a accounts.Account
	var owner accounts.Account
	err := row.Scan(&a.ID, &a.Username, &a.ParentID, &a.ParentUsername, &a.DisplayName, &a.DirectPermissions, &a.RequireMFA, &a.MFAEnrolled, &a.PermissionsVersion, &a.ProfileVersion, &a.Pending, &a.DisabledAt, &a.CreatedAt, &a.LastActiveSuperuser, &a.PreviousUsernames, &a.Groups, &a.DefaultGrants, &owner.DisabledAt, &owner.Pending, &owner.DirectPermissions, &owner.RequireMFA, &owner.MFAEnrolled, &owner.Groups)
	if errors.Is(err, pgx.ErrNoRows) {
		err = auth.ErrNotFound
	}
	if a.ParentID != nil {
		owner.ID = *a.ParentID
		owner.Username = a.ParentUsername
		a.Owner = &owner
	}
	return a, err
}
func (s *Store) CredentialByUsername(ctx context.Context, username string) (auth.Credential, error) {
	var c auth.Credential
	// Browser password authentication currently applies to root human accounts.
	err := s.pool.QueryRow(ctx, `SELECT a.id::text,a.username,a.display_name,a.direct_permissions,c.password_hash,a.security_version,a.pending,a.disabled_at
 FROM accounts a JOIN password_credentials c ON c.account_id=a.id
 WHERE a.parent_id IS NULL AND a.username=$1`, username).
		Scan(&c.Account.ID, &c.Account.Username, &c.Account.DisplayName, &c.Account.DirectPermissions, &c.PasswordHash, &c.SecurityVersion, &c.Account.Pending, &c.Account.DisabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = auth.ErrNotFound
	}
	return c, err
}
func (s *Store) CreateSession(ctx context.Context, session auth.Session) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `INSERT INTO browser_sessions(token_hash,account_id,created_at,last_seen_at,expires_at)
 SELECT $1,id,$3,$4,$5 FROM accounts WHERE id=$2 AND disabled_at IS NULL AND NOT pending`, session.Digest, session.AccountID, session.CreatedAt, session.LastSeenAt, session.ExpiresAt)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return auth.ErrCredentials
	}
	if _, err = tx.Exec(ctx, `INSERT INTO account_events(account_id,kind,occurred_at) VALUES($1,'signed_in',$2)`, session.AccountID, session.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) UseSession(ctx context.Context, hash []byte, now, cutoff time.Time) (accounts.Account, error) {
	a, err := scanAccount(s.pool.QueryRow(ctx, `WITH used AS (
 UPDATE browser_sessions SET last_seen_at=GREATEST(last_seen_at,$2)
 WHERE token_hash=$1 AND expires_at>$2 AND last_seen_at>$3 RETURNING account_id)
 SELECT `+accountColumns+` FROM used JOIN accounts a ON a.id=used.account_id
 LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.disabled_at IS NULL AND NOT a.pending AND (a.parent_id IS NULL OR (p.disabled_at IS NULL AND NOT p.pending))`, hash, now, cutoff))
	if errors.Is(err, auth.ErrNotFound) {
		var disabled bool
		e := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM disabled_session_notices n JOIN accounts a ON a.id=n.account_id WHERE n.token_hash=$1 AND n.expires_at>$2 AND a.disabled_at IS NOT NULL)`, hash, now).Scan(&disabled)
		if e != nil {
			return a, e
		}
		if disabled {
			return a, auth.ErrAccountDisabled
		}
	}
	return a, err
}
func (s *Store) DeleteSession(ctx context.Context, hash []byte) error {
	_, err := s.pool.Exec(ctx, `WITH removed AS (DELETE FROM browser_sessions WHERE token_hash=$1 RETURNING account_id)
 INSERT INTO account_events(account_id,kind,occurred_at) SELECT account_id,'signed_out',now() FROM removed`, hash)
	return err
}
func (s *Store) Attempt(ctx context.Context, key []byte, limit int, window time.Duration, now time.Time) (bool, error) {
	var attempts int
	err := s.pool.QueryRow(ctx, `INSERT INTO authentication_attempts(bucket_hash,attempts,expires_at) VALUES($1,1,$2)
 ON CONFLICT(bucket_hash) DO UPDATE SET
 attempts=CASE WHEN authentication_attempts.expires_at<=$3 THEN 1 ELSE LEAST(authentication_attempts.attempts+1,$4+1) END,
 expires_at=CASE WHEN authentication_attempts.expires_at<=$3 THEN $2 ELSE authentication_attempts.expires_at END
 RETURNING attempts`, key, now.Add(window), now, limit).Scan(&attempts)
	return attempts <= limit, err
}
func (s *Store) Cleanup(ctx context.Context, now, cutoff time.Time) error {
	for _, table := range []string{"oauth_requests", "oauth_tokens"} {
		if _, err := s.pool.Exec(ctx, "DELETE FROM "+table+" WHERE expires_at<=$1", now); err != nil {
			return err
		}
	}
	// Unused registrations are bounded in time; active clients remain registered.
	if _, err := s.pool.Exec(ctx, `DELETE FROM oauth_clients c WHERE created_at<$1 AND NOT EXISTS(SELECT 1 FROM oauth_tokens t WHERE t.client_id=c.id) AND NOT EXISTS(SELECT 1 FROM oauth_requests r WHERE r.data->>'ClientID'=c.id)`, now.Add(-30*24*time.Hour)); err != nil {
		return err
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM device_requests WHERE expires_at<=$1`, now); err != nil {
		return err
	}
	// Each small deletion is independent; no long transaction holds all tables.
	for _, q := range []string{
		`DELETE FROM setup_grants WHERE expires_at<=$1`,
		`DELETE FROM security_flows WHERE expires_at<=$1`,
		`DELETE FROM account_links WHERE expires_at<=$1`,
		`DELETE FROM disabled_session_notices WHERE expires_at<=$1`,
		`DELETE FROM authentication_attempts WHERE expires_at<=$1`,
	} {
		if _, err := s.pool.Exec(ctx, q, now); err != nil {
			return err
		}
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM browser_sessions WHERE expires_at<=$1 OR last_seen_at<=$2`, now, cutoff)
	return err
}
