package postgres

import (
	"acta2/internal/accounts"
	"acta2/internal/auth"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"slices"
	"time"
)

// Parentage is immutable. Lock the human owner first on every account mutation,
// including agent authentication, so revocation/pruning cannot race issuance.
func lockAccountFamily(ctx context.Context, tx pgx.Tx, id string) error {
	var parent *string
	if e := tx.QueryRow(ctx, `SELECT parent_id::text FROM accounts WHERE id=$1`, id).Scan(&parent); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return auth.ErrNotFound
		}
		return e
	}
	owner := id
	if parent != nil {
		owner = *parent
	}
	_, e := tx.Exec(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, owner)
	return e
}
func (t securityTx) OwnedAgent(ctx context.Context, id string) (accounts.Account, auth.SecurityTx, error) {
	var a accounts.Account
	var found string
	e := t.tx.QueryRow(ctx, `SELECT id::text FROM accounts WHERE id=$1 AND parent_id=$2 FOR UPDATE`, id, t.accountID).Scan(&found)
	if errors.Is(e, pgx.ErrNoRows) {
		return a, nil, auth.ErrNotFound
	}
	if e != nil {
		return a, nil, e
	}
	a, e = scanAccount(t.tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.id=$1`, id))
	return a, securityTx{t.tx, id}, e
}
func (s *Store) CreateAgent(ctx context.Context, owner string, token []byte, a accounts.Account, now time.Time) (accounts.Account, error) {
	tx, actor, e := s.managementTx(ctx, owner, token, "agents", now)
	if e != nil {
		return a, e
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, `INSERT INTO accounts(id,parent_id,username,display_name,created_at) VALUES($1,$2,$3,$4,$5)`, a.ID, owner, a.Username, a.DisplayName, now)
	if e != nil {
		return a, nameError(e)
	}
	if e = (managedTx{securityTx{tx, a.ID}, owner, actor}).Event(ctx, "agent_created", now); e != nil {
		return a, e
	}
	a, e = scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.id=$1`, a.ID))
	if e != nil {
		return a, e
	}
	return a, tx.Commit(ctx)
}
func (s *Store) ListAgents(ctx context.Context, owner string, token []byte, now time.Time) ([]accounts.Account, error) {
	tx, _, e := s.managementTx(ctx, owner, token, "agents", now)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.parent_id=$1 ORDER BY a.username,a.id`, owner)
	if e != nil {
		return nil, e
	}
	out := []accounts.Account{}
	for rows.Next() {
		a, e := scanAccount(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, a)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) WithAgent(ctx context.Context, owner string, token []byte, id string, now time.Time, fn func(*auth.SecurityRecord, auth.ManagedTx) error) error {
	tx, actor, e := s.managementTx(ctx, owner, token, "agents", now)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Verify ownership before reading private security state or allowing mutation.
	if _, _, e = (securityTx{tx, owner}).OwnedAgent(ctx, id); e != nil {
		return e
	}
	return s.securityRecord(ctx, tx, id, true, func(r *auth.SecurityRecord, _ auth.SecurityTx) error {
		return fn(r, managedTx{securityTx{tx, id}, owner, actor})
	})
}

// All direct/group access changes call this while the owner's lock is held.
// Prune stored grants, rather than merely masking them, so later restoration of
// owner access cannot resurrect an old delegation.
func pruneAgentGrants(ctx context.Context, tx pgx.Tx, owner string) error {
	a, e := scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.id=$1`, owner))
	if e != nil {
		return e
	}
	if a.IsAgent() {
		return nil
	}
	rows, e := tx.Query(ctx, `SELECT id::text,direct_permissions FROM accounts WHERE parent_id=$1 ORDER BY id FOR UPDATE`, owner)
	if e != nil {
		return e
	}
	type entry struct {
		id     string
		grants []string
	}
	var changed []entry
	allowed := accounts.ResolvePermissions(a)
	for rows.Next() {
		var id string
		var before []string
		if e = rows.Scan(&id, &before); e != nil {
			rows.Close()
			return e
		}
		after := []string{}
		for _, p := range before {
			if p != accounts.Superuser && slices.Contains(allowed, p) {
				after = append(after, p)
			}
		}
		if !slices.Equal(before, after) {
			changed = append(changed, entry{id, after})
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, item := range changed {
		if _, e = tx.Exec(ctx, `UPDATE accounts SET direct_permissions=$2,permissions_version=permissions_version+1 WHERE id=$1`, item.id, item.grants); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO account_events(account_id,kind,occurred_at) VALUES($1,'agent_grants_pruned',now())`, item.id); e != nil {
			return e
		}
	}
	return pruneWorkspaceAgentGrants(ctx, tx, owner, "")
}
