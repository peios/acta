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

type groupTx struct {
	store   *Store
	tx      pgx.Tx
	groupID string
	actor   accounts.Account
}

func (t groupTx) Actor() accounts.Account { return t.actor }

const groupColumns = `g.id::text,g.name,g.description,g.is_default,g.permissions,g.require_mfa,g.version,(SELECT count(*) FROM group_memberships m WHERE m.group_id=g.id)`

func scanGroup(row pgx.Row) (accounts.Group, error) {
	var g accounts.Group
	e := row.Scan(&g.ID, &g.Name, &g.Description, &g.Default, &g.Permissions, &g.RequireMFA, &g.Version, &g.MemberCount)
	if errors.Is(e, pgx.ErrNoRows) {
		e = auth.ErrNotFound
	}
	return g, e
}
func groupError(e error) error {
	var pg *pgconn.PgError
	if errors.As(e, &pg) && pg.Code == "23505" && pg.ConstraintName == "group_name_unique" {
		return &accounts.FieldError{Field: "name", Message: "A group with that name already exists."}
	}
	return e
}
func ensureSuperuser(ctx context.Context, tx pgx.Tx) error {
	var exists bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM superuser_accounts su JOIN accounts a ON a.id=su.id WHERE a.disabled_at IS NULL AND NOT a.pending)`).Scan(&exists); e != nil {
		return e
	}
	if !exists {
		return &accounts.FieldError{Field: "permissions", Message: "At least one active account must retain Superuser access."}
	}
	return nil
}
func (s *Store) CreateGroup(ctx context.Context, actor string, token []byte, g accounts.Group, now time.Time) (accounts.Group, error) {
	tx, a, e := s.managementTx(ctx, actor, token, accounts.ManagePermissions, now)
	if e != nil {
		return g, e
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, `INSERT INTO permission_groups(id,name,description) VALUES($1,$2,$3)`, g.ID, g.Name, g.Description)
	if e != nil {
		return g, groupError(e)
	}
	if e = (groupTx{s, tx, g.ID, a}).Event(ctx, "created", nil, now); e != nil {
		return g, e
	}
	return g, tx.Commit(ctx)
}
func (s *Store) ListGroups(ctx context.Context, actor string, token []byte, query string, offset int, now time.Time) ([]accounts.Group, bool, error) {
	tx, _, e := s.managementTx(ctx, actor, token, accounts.ManagePermissions, now)
	if e != nil {
		return nil, false, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT `+groupColumns+` FROM permission_groups g WHERE $1='' OR position(lower($1) in lower(g.name))>0 ORDER BY g.is_default DESC,lower(g.name),g.id LIMIT 51 OFFSET $2`, query, offset)
	if e != nil {
		return nil, false, e
	}
	defer rows.Close()
	out := []accounts.Group{}
	for rows.Next() {
		g, e := scanGroup(rows)
		if e != nil {
			return nil, false, e
		}
		out = append(out, g)
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
func (s *Store) WithGroup(ctx context.Context, actor string, token []byte, id string, now time.Time, fn func(*accounts.Group, auth.GroupTx) error) error {
	tx, a, e := s.managementTx(ctx, actor, token, accounts.ManagePermissions, now)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	g, e := scanGroup(tx.QueryRow(ctx, `SELECT `+groupColumns+` FROM permission_groups g WHERE g.id=$1 FOR UPDATE OF g`, id))
	if e != nil {
		return e
	}
	if e = fn(&g, groupTx{s, tx, id, a}); e != nil {
		return e
	}
	if e = ensureSuperuser(ctx, tx); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (t groupTx) SaveGroup(ctx context.Context, g accounts.Group) error {
	_, e := t.tx.Exec(ctx, `UPDATE permission_groups SET name=$2,description=$3,permissions=$4,require_mfa=$5,version=$6 WHERE id=$1`, g.ID, g.Name, g.Description, g.Permissions, g.RequireMFA, g.Version)
	return groupError(e)
}
func (t groupTx) MemberIDs(ctx context.Context) ([]string, error) {
	rows, e := t.tx.Query(ctx, `SELECT account_id::text FROM group_memberships WHERE group_id=$1 ORDER BY account_id`, t.groupID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
func (t groupTx) Members(ctx context.Context, query string, offset int) ([]accounts.Account, bool, error) {
	rows, e := t.tx.Query(ctx, `SELECT `+accountColumns+` FROM group_memberships m JOIN accounts a ON a.id=m.account_id LEFT JOIN accounts p ON p.id=a.parent_id WHERE m.group_id=$1 AND ($2='' OR position(lower($2) in lower(a.username))>0 OR position(lower($2) in lower(COALESCE(a.display_name,'')))>0) ORDER BY a.username,a.id LIMIT 51 OFFSET $3`, t.groupID, query, offset)
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
	return out, more, nil
}
func (t groupTx) SetMembership(ctx context.Context, id string, version int64, member bool) (bool, error) {
	var current int64
	e := t.tx.QueryRow(ctx, `SELECT permissions_version FROM accounts WHERE id=$1 AND parent_id IS NULL FOR UPDATE`, id).Scan(&current)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, auth.ErrNotFound
	}
	if e != nil {
		return false, e
	}
	if current != version {
		return false, auth.ErrPermissionsChanged
	}
	var tag pgconn.CommandTag
	if member {
		tag, e = t.tx.Exec(ctx, `INSERT INTO group_memberships(group_id,account_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, t.groupID, id)
	} else {
		tag, e = t.tx.Exec(ctx, `DELETE FROM group_memberships WHERE group_id=$1 AND account_id=$2`, t.groupID, id)
	}
	return tag.RowsAffected() > 0, e
}
func (t groupTx) RefreshMember(ctx context.Context, id string, now time.Time, fn func(*auth.SecurityRecord) error) error {
	return t.store.mutateSecurityRecord(ctx, t.tx, id, true, func(r *auth.SecurityRecord, tx auth.SecurityTx) error {
		if e := fn(r); e != nil {
			return e
		}
		if _, e := t.tx.Exec(ctx, `UPDATE accounts SET permissions_version=permissions_version+1 WHERE id=$1`, id); e != nil {
			return e
		}
		if _, e := t.tx.Exec(ctx, `DELETE FROM account_links WHERE account_id=$1`, id); e != nil {
			return e
		}
		if e := pruneAgentGrants(ctx, t.tx, id); e != nil {
			return e
		}
		return (managedTx{securityTx{t.tx, id}, t.actor.ID, t.actor}).Event(ctx, "group_access_updated", now)
	})
}
func (t groupTx) Event(ctx context.Context, kind string, id *string, now time.Time) error {
	_, e := t.tx.Exec(ctx, `INSERT INTO group_events(group_id,actor_id,account_id,kind,occurred_at) VALUES($1,$2,$3,$4,$5)`, t.groupID, t.actor.ID, id, kind, now)
	return e
}

func assignDefaultGroup(ctx context.Context, tx pgx.Tx, id string) error {
	_, e := tx.Exec(ctx, `INSERT INTO group_memberships(group_id,account_id) SELECT id,$1 FROM permission_groups WHERE is_default`, id)
	return e
}
