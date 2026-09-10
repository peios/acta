package postgres

import (
	"acta2/internal/accounts"
	"acta2/internal/auth"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

var _ auth.ManagementStore = (*Store)(nil)

type managedTx struct {
	securityTx
	actorID string
	actor   accounts.Account
}

func nameError(err error) error {
	var e *pgconn.PgError
	if errors.As(err, &e) && e.Code == "23505" && (e.ConstraintName == "account_name_unique" || e.ConstraintName == "account_namespace_unique" || e.ConstraintName == "account_handles_pkey") {
		return accounts.ErrNameUnavailable
	}
	return err
}
func (s *Store) managementTx(ctx context.Context, actor string, token []byte, permission string, now time.Time) (pgx.Tx, accounts.Account, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, accounts.Account{}, e
	}
	fail := func(err error) (pgx.Tx, accounts.Account, error) {
		tx.Rollback(ctx)
		return nil, accounts.Account{}, err
	}
	// Serialize administration, including last-Superuser checks. Ordinary
	// authentication never takes this installation lock.
	if _, e = tx.Exec(ctx, `SELECT singleton FROM installation WHERE singleton FOR UPDATE`); e != nil {
		return fail(e)
	}
	if e = lockAccountFamily(ctx, tx, actor); e != nil {
		return fail(e)
	}
	account, e := scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.id=$1`, actor))
	if e != nil {
		return fail(e)
	}
	if !account.Available() {
		return fail(auth.ErrUnauthenticated)
	}
	if accounts.RequiresMFASetup(account) {
		return fail(auth.ErrMFARequired)
	}
	if permission == "agents" && account.IsAgent() {
		return fail(auth.ErrForbidden)
	}
	if permission != "link" && permission != "agents" && permission != "workspace" && !accounts.CheckPermission(account, permission) {
		return fail(auth.ErrForbidden)
	}
	if _, e = (securityTx{tx, actor}).Session(ctx, token, now); e != nil {
		return fail(e)
	}
	return tx, account, nil
}
func (s *Store) WithManaged(ctx context.Context, actor string, token []byte, id, permission string, now time.Time, fn func(*auth.SecurityRecord, auth.ManagedTx) error) error {
	tx, actorAccount, e := s.managementTx(ctx, actor, token, permission, now)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	err := s.securityRecord(ctx, tx, id, true, func(r *auth.SecurityRecord, _ auth.SecurityTx) error {
		if r.Account.ParentID != nil {
			return auth.ErrNotFound
		}
		if permission == "link" {
			capability := accounts.ResetCredentials
			if r.Account.Pending {
				capability = accounts.CreateUsers
			}
			if !accounts.CheckPermission(actorAccount, capability) || (accounts.Privileged(r.Account) && !accounts.CheckPermission(actorAccount, accounts.Superuser)) {
				return auth.ErrForbidden
			}
		}
		return fn(r, managedTx{securityTx{tx, id}, actor, actorAccount})
	})
	if errors.Is(err, auth.ErrUnauthenticated) {
		return auth.ErrNotFound
	}
	return err
}
func (s *Store) CreatePending(ctx context.Context, actor string, token []byte, a accounts.Account, link auth.AccountLink, now time.Time) (accounts.Account, error) {
	tx, actorAccount, e := s.managementTx(ctx, actor, token, accounts.CreateUsers, now)
	if e != nil {
		return a, e
	}
	defer tx.Rollback(ctx)
	if !accounts.CanCreateUser(actorAccount) {
		return a, auth.ErrForbidden
	}
	_, e = tx.Exec(ctx, `INSERT INTO accounts(id,username,display_name,pending,created_at) VALUES($1,$2,$3,true,$4)`, a.ID, a.Username, a.DisplayName, now)
	if e != nil {
		return a, nameError(e)
	}
	if e = assignDefaultGroup(ctx, tx, a.ID); e != nil {
		return a, e
	}
	mt := managedTx{securityTx{tx, a.ID}, actor, actorAccount}
	if e = mt.SaveAccountLink(ctx, link); e != nil {
		return a, e
	}
	if e = mt.Event(ctx, "account_created", now); e != nil {
		return a, e
	}
	a, e = scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.id=$1`, a.ID))
	if e != nil {
		return a, e
	}
	return a, tx.Commit(ctx)
}
func (s *Store) ListManaged(ctx context.Context, actor string, token []byte, status, query string, offset int, now time.Time) ([]accounts.Account, bool, error) {
	tx, _, e := s.managementTx(ctx, actor, token, accounts.ViewUsers, now)
	if e != nil {
		return nil, false, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id
 WHERE a.parent_id IS NULL AND ($1='' OR CASE WHEN a.disabled_at IS NOT NULL THEN 'disabled' WHEN a.pending THEN 'pending' ELSE 'active' END=$1)
 AND ($2='' OR position(lower($2) in lower(a.username))>0 OR position(lower($2) in lower(COALESCE(a.display_name,'')))>0)
 ORDER BY a.username,a.id LIMIT 51 OFFSET $3`, status, query, offset)
	if e != nil {
		return nil, false, e
	}
	defer rows.Close()
	out := []accounts.Account{}
	for rows.Next() {
		a, e := scanAccount(rows)
		if e != nil {
			return nil, false, e
		}
		out = append(out, a)
	}
	if e = rows.Err(); e != nil {
		return nil, false, e
	}
	more := len(out) > 50
	if more {
		out = out[:50]
	}
	return out, more, tx.Commit(ctx)
}
func (t managedTx) SetProfile(ctx context.Context, version int64, username string, display *string) (accounts.Account, error) {
	result, e := t.tx.Exec(ctx, `UPDATE accounts SET username=$2,display_name=$3,profile_version=profile_version+1 WHERE id=$1 AND profile_version=$4`, t.accountID, username, display, version)
	if e != nil {
		return accounts.Account{}, nameError(e)
	}
	if result.RowsAffected() != 1 {
		return accounts.Account{}, accounts.ErrProfileChanged
	}
	if e = t.Event(ctx, "profile_updated", time.Now()); e != nil {
		return accounts.Account{}, e
	}
	return scanAccount(t.tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.id=$1`, t.accountID))
}
func (t managedTx) Event(ctx context.Context, kind string, now time.Time) error {
	var actor *string
	if t.actorID != "" {
		actor = &t.actorID
	}
	_, e := t.tx.Exec(ctx, `INSERT INTO account_events(account_id,actor_id,kind,occurred_at) VALUES($1,$2,$3,$4)`, t.accountID, actor, kind, now)
	return e
}
func (t managedTx) SetDisabled(ctx context.Context, disabled bool, now time.Time) error {
	if disabled {
		var e error
		if _, e = t.tx.Exec(ctx, `INSERT INTO disabled_session_notices(token_hash,account_id,expires_at) SELECT token_hash,account_id,expires_at FROM browser_sessions WHERE account_id=$1 ON CONFLICT(token_hash) DO NOTHING`, t.accountID); e != nil {
			return e
		}
		if e = t.RevokeSessions(ctx, []byte{}, ""); e != nil {
			return e
		}
		if _, e = t.tx.Exec(ctx, `DELETE FROM browser_sessions WHERE account_id IN (SELECT id FROM accounts WHERE parent_id=$1)`, t.accountID); e != nil {
			return e
		}
		if _, e = t.tx.Exec(ctx, `DELETE FROM account_links WHERE account_id=$1`, t.accountID); e != nil {
			return e
		}
	}
	var at *time.Time
	if disabled {
		at = &now
	}
	if _, e := t.tx.Exec(ctx, `UPDATE accounts SET disabled_at=$2 WHERE id=$1`, t.accountID, at); e != nil {
		return e
	}
	if e := ensureSuperuser(ctx, t.tx); e != nil {
		return e
	}
	kind := "account_enabled"
	if disabled {
		kind = "account_disabled"
	}
	return t.Event(ctx, kind, now)
}
func (t managedTx) SaveAccountLink(ctx context.Context, g auth.AccountLink) error {
	_, e := t.tx.Exec(ctx, `INSERT INTO account_links(account_id,token_hash,purpose,expires_at,security_version,disable_mfa,remove_passkeys) VALUES($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT(account_id) DO UPDATE SET token_hash=EXCLUDED.token_hash,purpose=EXCLUDED.purpose,expires_at=EXCLUDED.expires_at,security_version=EXCLUDED.security_version,disable_mfa=EXCLUDED.disable_mfa,remove_passkeys=EXCLUDED.remove_passkeys`, t.accountID, g.Digest, g.Purpose, g.ExpiresAt, g.Version, g.DisableMFA, g.RemovePasskeys)
	return e
}
func scanLink(row pgx.Row) (auth.AccountLink, error) {
	var g auth.AccountLink
	e := row.Scan(&g.AccountID, &g.Digest, &g.Purpose, &g.ExpiresAt, &g.Version, &g.DisableMFA, &g.RemovePasskeys)
	if errors.Is(e, pgx.ErrNoRows) {
		e = auth.ErrAccountLink
	}
	return g, e
}

const linkColumns = `g.account_id::text,g.token_hash,g.purpose,g.expires_at,g.security_version,g.disable_mfa,g.remove_passkeys`

func (s *Store) ReadAccountLink(ctx context.Context, hash []byte, now time.Time) (auth.AccountLink, accounts.Account, error) {
	g, e := scanLink(s.pool.QueryRow(ctx, `SELECT `+linkColumns+` FROM account_links g JOIN accounts a ON a.id=g.account_id WHERE g.token_hash=$1 AND g.expires_at>$2 AND g.security_version=a.security_version AND a.disabled_at IS NULL AND a.pending=(g.purpose='invite')`, hash, now))
	if e != nil {
		return g, accounts.Account{}, e
	}
	a, e := scanAccount(s.pool.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.id=$1`, g.AccountID))
	return g, a, e
}
func (s *Store) RedeemAccountLink(ctx context.Context, hash []byte, now time.Time, fn func(*auth.SecurityRecord, auth.AccountLink, auth.ManagedTx) error) error {
	g, _, e := s.ReadAccountLink(ctx, hash, now)
	if e != nil {
		return e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	return s.securityRecord(ctx, tx, g.AccountID, true, func(r *auth.SecurityRecord, _ auth.SecurityTx) error {
		current, e := scanLink(tx.QueryRow(ctx, `SELECT `+linkColumns+` FROM account_links g WHERE g.token_hash=$1 AND g.account_id=$2 AND g.expires_at>$3 FOR UPDATE`, hash, g.AccountID, now))
		if e != nil {
			return e
		}
		if r.Account.DisabledAt != nil || r.Version != current.Version || r.Account.Pending != (current.Purpose == "invite") {
			return auth.ErrAccountLink
		}
		if e = fn(r, current, managedTx{securityTx{tx, g.AccountID}, "", accounts.Account{}}); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE accounts SET pending=false WHERE id=$1`, g.AccountID); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `DELETE FROM account_links WHERE account_id=$1`, g.AccountID)
		return e
	})
}

func (t managedTx) Actor() accounts.Account { return t.actor }
func (t managedTx) SetPermissions(ctx context.Context, version int64, grants []string, requireMFA bool, now time.Time) error {
	var current int64
	if e := t.tx.QueryRow(ctx, `SELECT permissions_version FROM accounts WHERE id=$1`, t.accountID).Scan(&current); e != nil {
		return e
	}
	if current != version {
		return auth.ErrPermissionsChanged
	}
	if grants == nil {
		grants = []string{}
	}
	if _, e := t.tx.Exec(ctx, `UPDATE accounts SET direct_permissions=$2,require_mfa=$3,permissions_version=permissions_version+1 WHERE id=$1`, t.accountID, grants, requireMFA); e != nil {
		return e
	}
	if e := ensureSuperuser(ctx, t.tx); e != nil {
		return e
	}
	// A previously issued credential link must not acquire new authority when
	// its account is promoted. Invalidate links on every access change.
	if _, e := t.tx.Exec(ctx, `DELETE FROM account_links WHERE account_id=$1`, t.accountID); e != nil {
		return e
	}
	if e := pruneAgentGrants(ctx, t.tx, t.accountID); e != nil {
		return e
	}
	return t.Event(ctx, "permissions_updated", now)
}
