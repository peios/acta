package auth

import (
	"acta2/internal/accounts"
	"context"
	"github.com/google/uuid"
	"time"
)

type GroupTx interface {
	Actor() accounts.Account
	SaveGroup(context.Context, accounts.Group) error
	MemberIDs(context.Context) ([]string, error)
	Members(context.Context, string, int) ([]accounts.Account, bool, error)
	SetMembership(context.Context, string, int64, bool) (bool, error)
	RefreshMember(context.Context, string, time.Time, func(*SecurityRecord) error) error
	Event(context.Context, string, *string, time.Time) error
}
type GroupStore interface {
	CreateGroup(context.Context, string, []byte, accounts.Group, time.Time) (accounts.Group, error)
	ListGroups(context.Context, string, []byte, string, int, time.Time) ([]accounts.Group, bool, error)
	// Serializes all access mutations with direct permissions and disabling,
	// rechecks actor/session under locks, and preserves an active Superuser.
	WithGroup(context.Context, string, []byte, string, time.Time, func(*accounts.Group, GroupTx) error) error
}

func (m *Management) CreateGroup(ctx context.Context, token, name, description string) (accounts.Group, error) {
	actor, e := m.actor(ctx, token, accounts.ManagePermissions)
	if e != nil {
		return accounts.Group{}, e
	}
	name, description, e = accounts.GroupProfile(name, description)
	if e != nil {
		return accounts.Group{}, e
	}
	return m.store.CreateGroup(ctx, actor.ID, Digest(token), accounts.Group{ID: uuid.NewString(), Name: name, Description: description, Permissions: []string{}, Version: 1}, m.auth.now())
}
func validOffset(offset int) error {
	if offset < 0 || offset > 1000000 {
		return &accounts.FieldError{Field: "offset", Message: "Invalid page."}
	}
	return nil
}
func (m *Management) ListGroups(ctx context.Context, token, query string, offset int) ([]accounts.Group, bool, error) {
	actor, e := m.actor(ctx, token, accounts.ManagePermissions)
	if e != nil {
		return nil, false, e
	}
	if e = validOffset(offset); e != nil {
		return nil, false, e
	}
	return m.store.ListGroups(ctx, actor.ID, Digest(token), query, offset, m.auth.now())
}
func (m *Management) group(ctx context.Context, token, id string, fn func(*accounts.Group, GroupTx) error) error {
	if _, e := uuid.Parse(id); e != nil {
		return ErrNotFound
	}
	actor, e := m.actor(ctx, token, accounts.ManagePermissions)
	if e != nil {
		return e
	}
	return m.store.WithGroup(ctx, actor.ID, Digest(token), id, m.auth.now(), fn)
}
func (m *Management) GetGroup(ctx context.Context, token, id string) (accounts.Group, error) {
	var out accounts.Group
	e := m.group(ctx, token, id, func(g *accounts.Group, _ GroupTx) error { out = *g; return nil })
	return out, e
}
func (m *Management) GroupMembers(ctx context.Context, token, id, query string, offset int) ([]accounts.Account, bool, error) {
	if e := validOffset(offset); e != nil {
		return nil, false, e
	}
	var out []accounts.Account
	var more bool
	e := m.group(ctx, token, id, func(_ *accounts.Group, tx GroupTx) error {
		var e error
		out, more, e = tx.Members(ctx, query, offset)
		return e
	})
	return out, more, e
}
func (m *Management) UpdateGroup(ctx context.Context, token, id string, version int64, name, description string) (accounts.Group, error) {
	name, description, e := accounts.GroupProfile(name, description)
	if e != nil {
		return accounts.Group{}, e
	}
	var out accounts.Group
	e = m.group(ctx, token, id, func(g *accounts.Group, tx GroupTx) error {
		if g.Version != version {
			return ErrPermissionsChanged
		}
		g.Name = name
		g.Description = description
		g.Version++
		if e := tx.SaveGroup(ctx, *g); e != nil {
			return e
		}
		out = *g
		return tx.Event(ctx, "profile_updated", nil, m.auth.now())
	})
	return out, e
}
func (m *Management) refreshGroupMember(ctx context.Context, tx GroupTx, id string) error {
	return tx.RefreshMember(ctx, id, m.auth.now(), func(r *SecurityRecord) error {
		d, e := m.security.loadData(r)
		if e != nil {
			return e
		}
		return m.security.saveData(r, d)
	})
}
func (m *Management) UpdateGroupPermissions(ctx context.Context, token, id string, version int64, grants []string, require bool) (accounts.Group, error) {
	if grants == nil {
		grants = []string{}
	}
	var out accounts.Group
	e := m.group(ctx, token, id, func(g *accounts.Group, tx GroupTx) error {
		if g.Version != version {
			return ErrPermissionsChanged
		}
		if e := accounts.GrantChangesAllowed(tx.Actor(), g.Permissions, grants); e != nil {
			return e
		}
		ids, e := tx.MemberIDs(ctx)
		if e != nil {
			return e
		}
		g.Permissions = grants
		g.RequireMFA = require
		g.Version++
		if e = tx.SaveGroup(ctx, *g); e != nil {
			return e
		}
		for _, id := range ids {
			if e = m.refreshGroupMember(ctx, tx, id); e != nil {
				return e
			}
		}
		out = *g
		return tx.Event(ctx, "permissions_updated", nil, m.auth.now())
	})
	return out, e
}
func (m *Management) SetGroupMembership(ctx context.Context, token, groupID, accountID string, groupVersion, accountVersion int64, member bool) error {
	if _, e := uuid.Parse(accountID); e != nil {
		return ErrNotFound
	}
	return m.group(ctx, token, groupID, func(g *accounts.Group, tx GroupTx) error {
		if g.Version != groupVersion {
			return ErrPermissionsChanged
		}
		if !accounts.CanAssignGroup(tx.Actor(), *g) {
			return ErrForbidden
		}
		changed, e := tx.SetMembership(ctx, accountID, accountVersion, member)
		if e != nil || !changed {
			return e
		}
		if e = m.refreshGroupMember(ctx, tx, accountID); e != nil {
			return e
		}
		kind := "member_removed"
		if member {
			kind = "member_added"
		}
		return tx.Event(ctx, kind, &accountID, m.auth.now())
	})
}
