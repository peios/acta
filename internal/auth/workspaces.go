package auth

import (
	"context"
	"time"

	"acta/internal/accounts"
	ws "acta/internal/workspaces"
	"github.com/google/uuid"
)

// Reads use a consistent snapshot; writes serialize with site access changes.
// Implementations validate the current session, owner status and MFA policy
// inside the transaction before invoking the callback. No callback may escape it.
type WorkspaceStore interface {
	WithWorkspaces(context.Context, string, []byte, time.Time, bool, func(WorkspaceTx) error) error
}
type WorkspaceTx interface {
	DocumentTx
	GuideTx
	MemoryTx
	TaskTx
	Actor() accounts.Account
	Account(context.Context, string) (accounts.Account, error)
	Lookup(context.Context, string, bool) (ws.Workspace, error)
	List(context.Context, accounts.Account, string, int) ([]ws.Workspace, bool, error)
	Access(context.Context, accounts.Account, string) (ws.Access, error)
	Create(context.Context, ws.Workspace, string) error
	Profile(context.Context, ws.Workspace) error
	Visit(context.Context, string, time.Time) error
	Members(context.Context, string, string, int, bool) ([]accounts.Account, bool, error)
	GroupGrantInfo(context.Context, string, string) (ws.GroupGrant, error)
	GroupGrants(context.Context, string, string, int) ([]ws.GroupGrant, bool, error)
	Membership(context.Context, string, string, bool) error
	MemberGrants(context.Context, string, string, []string) error
	GroupGrant(context.Context, string, string, []string) error
	Changed(context.Context, string, string) error
	AgentSettings(context.Context, accounts.Account) (ws.AgentAccess, error)
	SaveAgentAccess(context.Context, accounts.Account, int64, bool, []string) error
	SaveAgentPolicy(context.Context, accounts.Account, string, int64, bool, []string) error
}

func (m *Management) workspaceTx(ctx context.Context, token string, write bool, fn func(WorkspaceTx) error) error {
	if a, ok := ctx.Value(mcpAuthorityKey{}).(mcpAuthority); ok {
		return m.store.WithWorkspaces(ctx, a.account, a.digest, m.auth.now(), write, fn)
	}
	a, e := m.auth.Current(ctx, token)
	if e != nil {
		return e
	}
	return m.store.WithWorkspaces(ctx, a.ID, Digest(token), m.auth.now(), write, fn)
}
func (m *Management) WorkspaceList(ctx context.Context, token, query string, offset int) ([]ws.Workspace, bool, error) {
	if e := validOffset(offset); e != nil {
		return nil, false, e
	}
	var out []ws.Workspace
	var more bool
	e := m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		var e error
		out, more, e = tx.List(ctx, tx.Actor(), query, offset)
		return e
	})
	return out, more, e
}
func workspaceAccess(ctx context.Context, tx WorkspaceTx, ref string, bySlug bool) (ws.Workspace, ws.Access, error) {
	w, e := tx.Lookup(ctx, ref, bySlug)
	if e != nil {
		return w, ws.Access{}, e
	}
	access, e := tx.Access(ctx, tx.Actor(), w.ID)
	if e != nil {
		return w, access, e
	}
	if !access.Allowed {
		return ws.Workspace{}, access, ErrNotFound
	}
	w.Permissions = access.Permissions
	return w, access, nil
}
func workspaceVersion(w ws.Workspace, v int64) error {
	if w.Version != v {
		return ErrPermissionsChanged
	}
	return nil
}
func (m *Management) Workspace(ctx context.Context, token, ref string, bySlug bool) (ws.Workspace, error) {
	var out ws.Workspace
	e := m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error { var e error; out, _, e = workspaceAccess(ctx, tx, ref, bySlug); return e })
	return out, e
}
func (m *Management) CreateWorkspace(ctx context.Context, token, name, slug, description string) (ws.Workspace, error) {
	name, slug, description, e := ws.Profile(name, slug, description)
	if e != nil {
		return ws.Workspace{}, e
	}
	w := ws.Workspace{ID: uuid.NewString(), Name: name, Slug: slug, Description: description, Version: 1, CreatedAt: m.auth.now(), Permissions: ws.All(), PreviousSlugs: []string{}}
	e = m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		a := tx.Actor()
		if !accounts.CheckPermission(a, accounts.CreateWorkspaces) {
			return ErrForbidden
		}
		// Agent creation policy is checked here, before any persistent mutation.
		if a.IsAgent() {
			return ErrForbidden
		}
		if e := tx.Create(ctx, w, a.ID); e != nil {
			return e
		}
		return tx.Visit(ctx, w.ID, m.auth.now())
	})
	return w, e
}
func (m *Management) EditWorkspace(ctx context.Context, token, id string, version int64, name, slug, description string) (ws.Workspace, error) {
	name, slug, description, e := ws.Profile(name, slug, description)
	if e != nil {
		return ws.Workspace{}, e
	}
	var out ws.Workspace
	e = m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		w, a, e := workspaceAccess(ctx, tx, id, false)
		if e != nil {
			return e
		}
		if !ws.Contains(a.Permissions, ws.Edit) {
			return ErrForbidden
		}
		if e = workspaceVersion(w, version); e != nil {
			return e
		}
		w.Name = name
		w.Slug = slug
		w.Description = description
		if e = tx.Profile(ctx, w); e != nil {
			return e
		}
		if e = tx.Changed(ctx, w.ID, "details_updated"); e != nil {
			return e
		}
		out, _, e = workspaceAccess(ctx, tx, id, false)
		return e
	})
	return out, e
}
func (m *Management) VisitWorkspace(ctx context.Context, token, id string) error {
	return m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		w, _, e := workspaceAccess(ctx, tx, id, false)
		if e != nil {
			return e
		}
		return tx.Visit(ctx, w.ID, m.auth.now())
	})
}
func (m *Management) WorkspaceMembers(ctx context.Context, token, id, query string, offset int, candidates bool) ([]ws.Member, bool, error) {
	if e := validOffset(offset); e != nil {
		return nil, false, e
	}
	out := []ws.Member{}
	var more bool
	e := m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		w, a, e := workspaceAccess(ctx, tx, id, false)
		if e != nil {
			return e
		}
		if !ws.Contains(a.Permissions, ws.Members) && (candidates || !ws.Contains(a.Permissions, ws.Permissions)) {
			return ErrForbidden
		}
		values, next, e := tx.Members(ctx, w.ID, query, offset, candidates)
		if e != nil {
			return e
		}
		more = next
		for _, v := range values {
			access, e := tx.Access(ctx, v, w.ID)
			if e != nil {
				return e
			}
			if candidates {
				access = ws.Access{Direct: []string{}, Groups: []ws.GroupGrant{}, Permissions: []string{}}
			}
			out = append(out, ws.Member{ID: v.ID, Username: v.Handle(), DisplayName: v.DisplayName, Status: v.Status(), Access: access, CanRemove: !candidates && ws.Contains(a.Permissions, ws.Members) && ws.Subset(access.Permissions, a.Permissions)})
		}
		return nil
	})
	return out, more, e
}
func (m *Management) WorkspaceMembership(ctx context.Context, token, id, account string, version int64, member bool) error {
	if _, e := uuid.Parse(account); e != nil {
		return ErrNotFound
	}
	return m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		w, a, e := workspaceAccess(ctx, tx, id, false)
		if e != nil {
			return e
		}
		if !ws.Contains(a.Permissions, ws.Members) {
			return ErrForbidden
		}
		if e = workspaceVersion(w, version); e != nil {
			return e
		}
		target, e := tx.Account(ctx, account)
		if e != nil {
			return e
		}
		if target.IsAgent() {
			return ErrForbidden
		}
		if !member {
			other, e := tx.Access(ctx, target, w.ID)
			if e != nil {
				return e
			}
			if !ws.Subset(other.Permissions, a.Permissions) {
				return ErrForbidden
			}
		}
		if e = tx.Membership(ctx, w.ID, account, member); e != nil {
			return e
		}
		return tx.Changed(ctx, w.ID, "membership_updated")
	})
}
func (m *Management) WorkspaceGroups(ctx context.Context, token, id, query string, offset int) ([]ws.GroupGrant, bool, error) {
	if e := validOffset(offset); e != nil {
		return nil, false, e
	}
	var out []ws.GroupGrant
	var more bool
	e := m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		w, a, e := workspaceAccess(ctx, tx, id, false)
		if e != nil {
			return e
		}
		if !ws.Contains(a.Permissions, ws.Permissions) {
			return ErrForbidden
		}
		out, more, e = tx.GroupGrants(ctx, w.ID, query, offset)
		return e
	})
	return out, more, e
}
func (m *Management) WorkspaceGrants(ctx context.Context, token, id, target string, version int64, group bool, grants []string) error {
	if _, e := uuid.Parse(target); e != nil {
		return ErrNotFound
	}
	if grants == nil {
		grants = []string{}
	}
	return m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		w, a, e := workspaceAccess(ctx, tx, id, false)
		if e != nil {
			return e
		}
		if !ws.Contains(a.Permissions, ws.Permissions) {
			return ErrForbidden
		}
		if e = workspaceVersion(w, version); e != nil {
			return e
		}
		var before []string
		if group {
			g, e := tx.GroupGrantInfo(ctx, w.ID, target)
			if e != nil {
				return e
			}
			before = g.Permissions
		} else {
			v, e := tx.Account(ctx, target)
			if e != nil {
				return e
			}
			if v.IsAgent() {
				return ErrForbidden
			}
			access, e := tx.Access(ctx, v, w.ID)
			if e != nil {
				return e
			}
			before = access.Direct
		}
		if e = ws.ValidateGrants(before, grants, a.Permissions); e != nil {
			return e
		}
		if group {
			e = tx.GroupGrant(ctx, w.ID, target, grants)
		} else {
			e = tx.MemberGrants(ctx, w.ID, target, grants)
		}
		if e != nil {
			return e
		}
		return tx.Changed(ctx, w.ID, "permissions_updated")
	})
}
func ownedWorkspaceAgent(ctx context.Context, tx WorkspaceTx, id string) (accounts.Account, error) {
	if _, e := uuid.Parse(id); e != nil {
		return accounts.Account{}, ErrNotFound
	}
	a, e := tx.Account(ctx, id)
	if e != nil {
		return a, e
	}
	if tx.Actor().IsAgent() || a.ParentID == nil || *a.ParentID != tx.Actor().ID {
		return accounts.Account{}, ErrNotFound
	}
	return a, nil
}
func (m *Management) AgentWorkspaces(ctx context.Context, token, id string) (ws.AgentAccess, error) {
	var out ws.AgentAccess
	e := m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		a, e := ownedWorkspaceAgent(ctx, tx, id)
		if e != nil {
			return e
		}
		out, e = tx.AgentSettings(ctx, a)
		return e
	})
	return out, e
}
func (m *Management) SetAgentWorkspaces(ctx context.Context, token, id string, version int64, all bool, selected []string) error {
	for _, id := range selected {
		if _, e := uuid.Parse(id); e != nil {
			return ErrNotFound
		}
	}
	return m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		a, e := ownedWorkspaceAgent(ctx, tx, id)
		if e != nil {
			return e
		}
		for _, id := range selected {
			if _, _, e = workspaceAccess(ctx, tx, id, false); e != nil {
				return e
			}
		}
		return tx.SaveAgentAccess(ctx, a, version, all, selected)
	})
}
func (m *Management) SetAgentWorkspacePolicy(ctx context.Context, token, id, workspace string, version int64, inherit bool, grants []string) error {
	if grants == nil {
		grants = []string{}
	}
	return m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		a, e := ownedWorkspaceAgent(ctx, tx, id)
		if e != nil {
			return e
		}
		w, owner, e := workspaceAccess(ctx, tx, workspace, false)
		if e != nil {
			return e
		}
		if inherit {
			grants = []string{}
		} else {
			if e = ws.ValidateGrants(nil, grants, owner.Permissions); e != nil {
				return e
			}
		}
		return tx.SaveAgentPolicy(ctx, a, w.ID, version, inherit, grants)
	})
}
