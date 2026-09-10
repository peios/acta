package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"acta2/internal/accounts"
	"acta2/internal/auth"
	ws "acta2/internal/workspaces"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type workspaceTx struct {
	tx    pgx.Tx
	actor accounts.Account
}

func (t workspaceTx) Actor() accounts.Account { return t.actor }
func (t workspaceTx) Account(ctx context.Context, id string) (accounts.Account, error) {
	return scanAccount(t.tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.id::text=$1`, id))
}
func (s *Store) WithWorkspaces(ctx context.Context, actor string, token []byte, now time.Time, write bool, fn func(auth.WorkspaceTx) error) error {
	var tx pgx.Tx
	var a accounts.Account
	var e error
	if write {
		tx, a, e = s.managementTx(ctx, actor, token, "workspace", now)
	} else {
		tx, e = s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		a, e = (workspaceTx{tx, accounts.Account{}}).Account(ctx, actor)
		if e == nil && !a.Available() {
			e = auth.ErrUnauthenticated
		}
		if e == nil && accounts.RequiresMFASetup(a) {
			e = auth.ErrMFARequired
		}
		if e == nil {
			_, e = (securityTx{tx, actor}).readSession(ctx, token, now, false)
		}
	}
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = fn(workspaceTx{tx, a}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

const workspaceColumns = `w.id::text,w.name,w.slug,w.description,w.version,w.created_at,ARRAY(SELECT s.slug FROM workspace_slugs s WHERE s.workspace_id=w.id AND s.slug<>w.slug ORDER BY s.slug)`

func scanWorkspace(row pgx.Row) (ws.Workspace, error) {
	w := ws.Workspace{Permissions: []string{}}
	e := row.Scan(&w.ID, &w.Name, &w.Slug, &w.Description, &w.Version, &w.CreatedAt, &w.PreviousSlugs)
	if errors.Is(e, pgx.ErrNoRows) {
		e = auth.ErrNotFound
	}
	return w, e
}
func (t workspaceTx) Lookup(ctx context.Context, ref string, bySlug bool) (ws.Workspace, error) {
	if bySlug {
		return scanWorkspace(t.tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces w JOIN workspace_slugs s ON s.workspace_id=w.id WHERE s.slug=$1`, strings.ToLower(ref)))
	}
	return scanWorkspace(t.tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces w WHERE w.id::text=$1`, ref))
}
func (t workspaceTx) Access(ctx context.Context, a accounts.Account, id string) (ws.Access, error) {
	if a.IsAgent() {
		if a.Owner == nil || !a.Owner.Available() {
			return ws.Access{Permissions: []string{}}, nil
		}
		owner, e := t.Access(ctx, *a.Owner, id)
		if e != nil {
			return ws.Access{}, e
		}
		var all, selected, inherit bool
		var grants []string
		e = t.tx.QueryRow(ctx, `SELECT COALESCE((SELECT all_workspaces FROM agent_workspace_access WHERE account_id=$1),true),COALESCE(p.selected,false),COALESCE(p.inherit,true),COALESCE(p.permissions,'{}') FROM (SELECT 1) seed LEFT JOIN agent_workspace_policies p ON p.account_id=$1 AND p.workspace_id=$2`, a.ID, id).Scan(&all, &selected, &inherit, &grants)
		return ws.ResolveAgent(owner, all, selected, inherit, grants), e
	}
	var member bool
	var direct []string
	groups := []ws.GroupGrant{}
	e := t.tx.QueryRow(ctx, `SELECT m.account_id IS NOT NULL,COALESCE(m.permissions,'{}') FROM (SELECT 1) seed LEFT JOIN workspace_members m ON m.account_id=$1 AND m.workspace_id=$2`, a.ID, id).Scan(&member, &direct)
	if e != nil {
		return ws.Access{}, e
	}
	rows, e := t.tx.Query(ctx, `SELECT g.id::text,g.name,w.permissions FROM group_memberships m JOIN permission_groups g ON g.id=m.group_id JOIN workspace_group_grants w ON w.group_id=g.id AND w.workspace_id=$2 WHERE m.account_id=$1 ORDER BY lower(g.name),g.id`, a.ID, id)
	if e != nil {
		return ws.Access{}, e
	}
	for rows.Next() {
		var g ws.GroupGrant
		if e = rows.Scan(&g.ID, &g.Name, &g.Permissions); e != nil {
			rows.Close()
			return ws.Access{}, e
		}
		groups = append(groups, g)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return ws.Access{}, e
	}
	return ws.Resolve(a, member, direct, groups), nil
}
func (t workspaceTx) List(ctx context.Context, a accounts.Account, query string, offset int) ([]ws.Workspace, bool, error) {
	owner := a
	agent := ""
	if a.IsAgent() {
		if a.Owner == nil {
			return []ws.Workspace{}, false, nil
		}
		owner = *a.Owner
		agent = a.ID
	}
	// Candidate filtering matches membership and agent-access policy. Resolve the
	// complete capability set through Access for each returned workspace.
	limit := 51
	if offset < 0 {
		offset = 0
		limit = 0
	}
	rows, e := t.tx.Query(ctx, `SELECT `+workspaceColumns+` FROM workspaces w LEFT JOIN workspace_visits v ON v.workspace_id=w.id AND v.account_id=$1
 WHERE ($2 OR EXISTS(SELECT 1 FROM workspace_members m WHERE m.workspace_id=w.id AND m.account_id=$3))
 AND ($4='' OR COALESCE((SELECT all_workspaces FROM agent_workspace_access WHERE account_id::text=$4),true) OR EXISTS(SELECT 1 FROM agent_workspace_policies p WHERE p.account_id::text=$4 AND p.workspace_id=w.id AND p.selected))
 AND ($5='' OR position(lower($5) in lower(w.name))>0 OR position(lower($5) in w.slug)>0)
 ORDER BY v.visited_at DESC NULLS LAST,lower(w.name),w.id LIMIT NULLIF($6,0) OFFSET $7`, a.ID, accounts.CheckPermission(owner, accounts.Superuser), owner.ID, agent, query, limit, offset)
	if e != nil {
		return nil, false, e
	}
	out := []ws.Workspace{}
	for rows.Next() {
		w, e := scanWorkspace(rows)
		if e != nil {
			rows.Close()
			return nil, false, e
		}
		out = append(out, w)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, false, e
	}
	more := limit > 0 && len(out) > 50
	if more {
		out = out[:50]
	}
	for i := range out {
		access, e := t.Access(ctx, a, out[i].ID)
		if e != nil {
			return nil, false, e
		}
		out[i].Permissions = access.Permissions
	}
	return out, more, nil
}
func workspaceError(e error) error {
	var pg *pgconn.PgError
	if errors.As(e, &pg) && pg.Code == "23505" && (pg.ConstraintName == "workspaces_slug_key" || pg.ConstraintName == "workspace_slugs_pkey") {
		return &accounts.FieldError{Field: "slug", Message: "That slug is in use or reserved. Choose another."}
	}
	return e
}
func (t workspaceTx) Create(ctx context.Context, w ws.Workspace, creator string) error {
	_, e := t.tx.Exec(ctx, `INSERT INTO workspaces(id,name,slug,description,created_at) VALUES($1,$2,$3,$4,$5)`, w.ID, w.Name, w.Slug, w.Description, w.CreatedAt)
	if e != nil {
		return workspaceError(e)
	}
	_, e = t.tx.Exec(ctx, `INSERT INTO workspace_members(workspace_id,account_id,permissions) VALUES($1,$2,$3)`, w.ID, creator, ws.All())
	if e != nil {
		return e
	}
	_, e = t.tx.Exec(ctx, `INSERT INTO workspace_events(workspace_id,actor_id,kind) VALUES($1,$2,'created')`, w.ID, t.actor.ID)
	return e
}
func (t workspaceTx) Profile(ctx context.Context, w ws.Workspace) error {
	_, e := t.tx.Exec(ctx, `UPDATE workspaces SET name=$2,slug=$3,description=$4 WHERE id=$1`, w.ID, w.Name, w.Slug, w.Description)
	return workspaceError(e)
}
func (t workspaceTx) Visit(ctx context.Context, id string, now time.Time) error {
	_, e := t.tx.Exec(ctx, `INSERT INTO workspace_visits(account_id,workspace_id,visited_at) VALUES($1,$2,$3) ON CONFLICT(account_id,workspace_id) DO UPDATE SET visited_at=EXCLUDED.visited_at`, t.actor.ID, id, now)
	return e
}
func (t workspaceTx) Members(ctx context.Context, id, query string, offset int, candidates bool) ([]accounts.Account, bool, error) {
	rows, e := t.tx.Query(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.parent_id IS NULL AND (EXISTS(SELECT 1 FROM workspace_members m WHERE m.workspace_id=$1 AND m.account_id=a.id) <> $4) AND (NOT $4 OR a.disabled_at IS NULL) AND ($2='' OR position(lower($2) in a.username)>0 OR position(lower($2) in lower(COALESCE(a.display_name,'')))>0) ORDER BY a.username,a.id LIMIT 51 OFFSET $3`, id, query, offset, candidates)
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
func (t workspaceTx) GroupGrantInfo(ctx context.Context, id, group string) (ws.GroupGrant, error) {
	var g ws.GroupGrant
	e := t.tx.QueryRow(ctx, `SELECT g.id::text,g.name,COALESCE(w.permissions,'{}') FROM permission_groups g LEFT JOIN workspace_group_grants w ON w.group_id=g.id AND w.workspace_id=$1 WHERE g.id=$2`, id, group).Scan(&g.ID, &g.Name, &g.Permissions)
	if errors.Is(e, pgx.ErrNoRows) {
		e = auth.ErrNotFound
	}
	return g, e
}
func (t workspaceTx) GroupGrants(ctx context.Context, id, query string, offset int) ([]ws.GroupGrant, bool, error) {
	limit := 51
	if offset < 0 {
		offset = 0
		limit = 0
	}
	rows, e := t.tx.Query(ctx, `SELECT g.id::text,g.name,COALESCE(w.permissions,'{}') FROM permission_groups g LEFT JOIN workspace_group_grants w ON w.group_id=g.id AND w.workspace_id=$1 WHERE $2='' OR position(lower($2) in lower(g.name))>0 ORDER BY lower(g.name),g.id LIMIT NULLIF($3,0) OFFSET $4`, id, query, limit, offset)
	if e != nil {
		return nil, false, e
	}
	defer rows.Close()
	out := []ws.GroupGrant{}
	for rows.Next() {
		var g ws.GroupGrant
		if e = rows.Scan(&g.ID, &g.Name, &g.Permissions); e != nil {
			return nil, false, e
		}
		out = append(out, g)
	}
	if e = rows.Err(); e != nil {
		return nil, false, e
	}
	more := limit > 0 && len(out) > 50
	if more {
		out = out[:50]
	}
	return out, more, nil
}
func (t workspaceTx) Membership(ctx context.Context, id, account string, member bool) error {
	if member {
		_, e := t.tx.Exec(ctx, `INSERT INTO workspace_members(workspace_id,account_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, account)
		return e
	}
	_, e := t.tx.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id=$1 AND account_id=$2`, id, account)
	return e
}
func (t workspaceTx) MemberGrants(ctx context.Context, id, account string, grants []string) error {
	tag, e := t.tx.Exec(ctx, `UPDATE workspace_members SET permissions=$3 WHERE workspace_id=$1 AND account_id=$2`, id, account, grants)
	if e == nil && tag.RowsAffected() != 1 {
		return auth.ErrNotFound
	}
	return e
}
func (t workspaceTx) GroupGrant(ctx context.Context, id, group string, grants []string) error {
	_, e := t.tx.Exec(ctx, `INSERT INTO workspace_group_grants(workspace_id,group_id,permissions) VALUES($1,$2,$3) ON CONFLICT(workspace_id,group_id) DO UPDATE SET permissions=EXCLUDED.permissions`, id, group, grants)
	return e
}
func (t workspaceTx) Changed(ctx context.Context, id, kind string) error {
	if _, e := t.tx.Exec(ctx, `UPDATE workspaces SET version=version+1 WHERE id=$1`, id); e != nil {
		return e
	}
	if e := pruneWorkspaceAgentGrants(ctx, t.tx, "", id); e != nil {
		return e
	}
	_, e := t.tx.Exec(ctx, `INSERT INTO workspace_events(workspace_id,actor_id,kind) VALUES($1,$2,$3)`, id, t.actor.ID, kind)
	return e
}
func agentWorkspaceVersion(ctx context.Context, tx pgx.Tx, id string) (bool, int64, error) {
	var all bool
	var v int64
	e := tx.QueryRow(ctx, `SELECT COALESCE((SELECT all_workspaces FROM agent_workspace_access WHERE account_id=$1),true),COALESCE((SELECT version FROM agent_workspace_access WHERE account_id=$1),1)`, id).Scan(&all, &v)
	return all, v, e
}
func (t workspaceTx) AgentSettings(ctx context.Context, a accounts.Account) (ws.AgentAccess, error) {
	all, version, e := agentWorkspaceVersion(ctx, t.tx, a.ID)
	if e != nil {
		return ws.AgentAccess{}, e
	}
	out := ws.AgentAccess{All: all, Version: version, Workspaces: []ws.AgentPolicy{}}
	list, _, e := t.List(ctx, t.actor, "", -1)
	if e != nil {
		return out, e
	}
	for _, w := range list {
		p := ws.AgentPolicy{Workspace: w, OwnerPermissions: w.Permissions}
		e = t.tx.QueryRow(ctx, `SELECT COALESCE(p.selected,false),COALESCE(p.inherit,true),COALESCE(p.permissions,'{}') FROM (SELECT 1) seed LEFT JOIN agent_workspace_policies p ON p.account_id=$1 AND p.workspace_id=$2`, a.ID, w.ID).Scan(&p.Selected, &p.Inherit, &p.Grants)
		if e != nil {
			return out, e
		}
		out.Workspaces = append(out.Workspaces, p)
	}
	return out, nil
}
func bumpAgentWorkspaceVersion(ctx context.Context, tx pgx.Tx, id string, version int64) error {
	_, v, e := agentWorkspaceVersion(ctx, tx, id)
	if e != nil {
		return e
	}
	if v != version {
		return auth.ErrPermissionsChanged
	}
	_, e = tx.Exec(ctx, `INSERT INTO agent_workspace_access(account_id,version) VALUES($1,2) ON CONFLICT(account_id) DO UPDATE SET version=agent_workspace_access.version+1`, id)
	return e
}
func (t workspaceTx) SaveAgentAccess(ctx context.Context, a accounts.Account, version int64, all bool, selected []string) error {
	if e := bumpAgentWorkspaceVersion(ctx, t.tx, a.ID, version); e != nil {
		return e
	}
	if _, e := t.tx.Exec(ctx, `UPDATE agent_workspace_access SET all_workspaces=$2 WHERE account_id=$1`, a.ID, all); e != nil {
		return e
	}
	if _, e := t.tx.Exec(ctx, `UPDATE agent_workspace_policies SET selected=false WHERE account_id=$1`, a.ID); e != nil {
		return e
	}
	for _, id := range selected {
		if _, e := t.tx.Exec(ctx, `INSERT INTO agent_workspace_policies(account_id,workspace_id,selected) VALUES($1,$2,true) ON CONFLICT(account_id,workspace_id) DO UPDATE SET selected=true`, a.ID, id); e != nil {
			return e
		}
	}
	return nil
}
func (t workspaceTx) SaveAgentPolicy(ctx context.Context, a accounts.Account, id string, version int64, inherit bool, grants []string) error {
	if e := bumpAgentWorkspaceVersion(ctx, t.tx, a.ID, version); e != nil {
		return e
	}
	_, e := t.tx.Exec(ctx, `INSERT INTO agent_workspace_policies(account_id,workspace_id,inherit,permissions) VALUES($1,$2,$3,$4) ON CONFLICT(account_id,workspace_id) DO UPDATE SET inherit=EXCLUDED.inherit,permissions=EXCLUDED.permissions`, a.ID, id, inherit, grants)
	return e
}

// Called inside all affected site/workspace access mutations. Inherited policy
// is deliberately not materialized. Explicit grants are permanently pruned.
func pruneWorkspaceAgentGrants(ctx context.Context, tx pgx.Tx, owner, workspace string) error {
	rows, e := tx.Query(ctx, `SELECT p.account_id::text,p.workspace_id::text,p.permissions FROM agent_workspace_policies p JOIN accounts a ON a.id=p.account_id WHERE NOT p.inherit AND ($1='' OR a.parent_id::text=$1) AND ($2='' OR p.workspace_id::text=$2) ORDER BY p.account_id,p.workspace_id`, owner, workspace)
	if e != nil {
		return e
	}
	type row struct {
		agent, workspace string
		grants           []string
	}
	items := []row{}
	for rows.Next() {
		var i row
		if e = rows.Scan(&i.agent, &i.workspace, &i.grants); e != nil {
			rows.Close()
			return e
		}
		items = append(items, i)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	t := workspaceTx{tx, accounts.Account{}}
	for _, i := range items {
		a, e := t.Account(ctx, i.agent)
		if e != nil {
			return e
		}
		access, e := t.Access(ctx, *a.Owner, i.workspace)
		if e != nil {
			return e
		}
		after := ws.Intersect(i.grants, access.Permissions)
		if len(after) == len(i.grants) {
			continue
		}
		if _, e = tx.Exec(ctx, `UPDATE agent_workspace_policies SET permissions=$3 WHERE account_id=$1 AND workspace_id=$2`, i.agent, i.workspace, after); e != nil {
			return e
		}
		_, v, e := agentWorkspaceVersion(ctx, tx, i.agent)
		if e != nil {
			return e
		}
		if e = bumpAgentWorkspaceVersion(ctx, tx, i.agent, v); e != nil {
			return e
		}
	}
	return nil
}
