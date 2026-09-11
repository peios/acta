package auth

import (
	"acta/internal/accounts"
	"context"
	"errors"
	"github.com/google/uuid"
	"time"
)

const AccountLinkLifetime = 24 * time.Hour

type AccountLink struct {
	AccountID      string
	Digest         []byte
	Purpose        string
	ExpiresAt      time.Time
	Version        int64
	DisableMFA     bool
	RemovePasskeys bool
}
type ManagedTx interface {
	SecurityTx
	Actor() accounts.Account
	SetPermissions(context.Context, int64, []string, bool, time.Time) error
	SetProfile(context.Context, int64, string, *string) (accounts.Account, error)
	SetDisabled(context.Context, bool, time.Time) error
	SaveAccountLink(context.Context, AccountLink) error
}
type ManagementStore interface {
	MigrationStore
	WorkspaceStore
	AgentStore
	GroupStore
	CreatePending(context.Context, string, []byte, accounts.Account, AccountLink, time.Time) (accounts.Account, error)
	// WithManaged locks installation administration, then actor and target before
	// sessions/flows. It rechecks an active actor, permission and session inside the
	// transaction. Disabled and pending targets may be managed.
	WithManaged(context.Context, string, []byte, string, string, time.Time, func(*SecurityRecord, ManagedTx) error) error
	ListManaged(context.Context, string, []byte, string, string, int, time.Time) ([]accounts.Account, bool, error)
	ReadAccountLink(context.Context, []byte, time.Time) (AccountLink, accounts.Account, error)
	RedeemAccountLink(context.Context, []byte, time.Time, func(*SecurityRecord, AccountLink, ManagedTx) error) error
}
type Management struct {
	auth     *Service
	security *Security
	store    ManagementStore
}

func NewManagement(a *Service, s *Security, store ManagementStore) *Management {
	return &Management{a, s, store}
}
func (m *Management) actor(ctx context.Context, token, permission string) (accounts.Account, error) {
	a, e := m.auth.Current(ctx, token)
	if e != nil {
		return a, e
	}
	if permission != "link" && !accounts.CheckPermission(a, permission) {
		return a, ErrForbidden
	}
	return a, nil
}
func (m *Management) List(ctx context.Context, token, status, query string, offset int) ([]accounts.Account, bool, error) {
	a, e := m.actor(ctx, token, accounts.ViewUsers)
	if e != nil {
		return nil, false, e
	}
	if status != "" && status != "active" && status != "pending" && status != "disabled" {
		return nil, false, &accounts.FieldError{Field: "status", Message: "Choose a valid account status."}
	}
	if offset < 0 || offset > 1000000 {
		return nil, false, &accounts.FieldError{Field: "offset", Message: "Invalid page."}
	}
	return m.store.ListManaged(ctx, a.ID, Digest(token), status, query, offset, m.auth.now())
}
func profileError(e error) error {
	if errors.Is(e, accounts.ErrNameUnavailable) {
		return &accounts.FieldError{Field: "username", Message: "That username is in use or reserved. Choose another."}
	}
	return e
}
func (m *Management) newLink(id, purpose string, version int64, disable, remove bool) (AccountLink, string, error) {
	token, e := Token()
	return AccountLink{AccountID: id, Digest: Digest(token), Purpose: purpose, Version: version, ExpiresAt: m.auth.now().Add(AccountLinkLifetime), DisableMFA: disable, RemovePasskeys: remove}, token, e
}
func (m *Management) Create(ctx context.Context, token, username, display string) (accounts.Account, string, error) {
	actor, e := m.actor(ctx, token, accounts.CreateUsers)
	if e != nil {
		return accounts.Account{}, "", e
	}
	name, label, e := accounts.ValidateProfile(username, display)
	if e != nil {
		return accounts.Account{}, "", e
	}
	a := accounts.Account{ID: uuid.NewString(), Username: name, DisplayName: label, Pending: true, ProfileVersion: 1}
	grant, secret, e := m.newLink(a.ID, "invite", 1, false, false)
	if e != nil {
		return a, "", e
	}
	a, e = m.store.CreatePending(ctx, actor.ID, Digest(token), a, grant, m.auth.now())
	return a, secret, profileError(e)
}
func (m *Management) manage(ctx context.Context, token, id, permission string, fn func(*SecurityRecord, ManagedTx) error) error {
	if _, e := uuid.Parse(id); e != nil {
		return ErrNotFound
	}
	a, e := m.actor(ctx, token, permission)
	if e != nil {
		return e
	}
	return m.store.WithManaged(ctx, a.ID, Digest(token), id, permission, m.auth.now(), fn)
}
func (m *Management) Get(ctx context.Context, token, id string) (accounts.Account, error) {
	var a accounts.Account
	e := m.manage(ctx, token, id, accounts.ViewUsers, func(r *SecurityRecord, _ ManagedTx) error { a = r.Account; return nil })
	return a, e
}
func (m *Management) Update(ctx context.Context, token, id string, version int64, username, display string) (accounts.Account, error) {
	name, label, e := accounts.ValidateProfile(username, display)
	if e != nil {
		return accounts.Account{}, e
	}
	var a accounts.Account
	e = m.manage(ctx, token, id, accounts.EditUsers, func(r *SecurityRecord, tx ManagedTx) error {
		if tx.Actor().ID == r.Account.ID {
			if !accounts.CanChangeProfile(r.Account, name, label) {
				return ErrForbidden
			}
		}
		var err error
		a, err = tx.SetProfile(ctx, version, name, label)
		return err
	})
	return a, profileError(e)
}
func (m *Management) Link(ctx context.Context, token, id string, disable, remove bool) (string, time.Time, error) {
	var secret string
	var expires time.Time
	e := m.manage(ctx, token, id, "link", func(r *SecurityRecord, tx ManagedTx) error {
		if r.Account.DisabledAt != nil {
			return &accounts.FieldError{Field: "account", Message: "Re-enable this account before creating a link."}
		}
		purpose := "reset"
		if r.Account.Pending {
			purpose = "invite"
			disable = false
			remove = false
		}
		grant, value, e := m.newLink(id, purpose, r.Version, disable, remove)
		if e != nil {
			return e
		}
		secret = value
		expires = grant.ExpiresAt
		if e = tx.SaveAccountLink(ctx, grant); e != nil {
			return e
		}
		return tx.Event(ctx, purpose+"_link_created", m.auth.now())
	})
	return secret, expires, e
}
func (m *Management) Disable(ctx context.Context, token, id string, disabled bool) error {
	return m.manage(ctx, token, id, accounts.DisableUsers, func(r *SecurityRecord, tx ManagedTx) error {
		if (r.Account.DisabledAt != nil) == disabled {
			return nil
		}
		if e := tx.SetDisabled(ctx, disabled, m.auth.now()); e != nil {
			return e
		}
		r.Version++
		r.ChangedAt = m.auth.now()
		return nil
	})
}
func (m *Management) InspectLink(ctx context.Context, token, address string) (AccountLink, accounts.Account, error) {
	if !validToken(token) {
		return AccountLink{}, accounts.Account{}, ErrAccountLink
	}
	if e := m.auth.throttle(ctx, "account-link:"+address, 60, 5*time.Minute); e != nil {
		return AccountLink{}, accounts.Account{}, e
	}
	return m.store.ReadAccountLink(ctx, Digest(token), m.auth.now())
}
func (m *Management) Redeem(ctx context.Context, token, password, display, address string, profileVersion int64) error {
	grant, _, e := m.InspectLink(ctx, token, address)
	if e != nil {
		return e
	}
	hash, e := m.auth.NewPasswordHash(password)
	if e != nil {
		return e
	}
	var label *string
	if grant.Purpose == "invite" {
		label, e = accounts.DisplayName(display)
		if e != nil {
			return &accounts.FieldError{Field: "display_name", Message: e.Error()}
		}
	}
	return m.store.RedeemAccountLink(ctx, Digest(token), m.auth.now(), func(r *SecurityRecord, g AccountLink, tx ManagedTx) error {
		if !m.auth.now().Before(g.ExpiresAt) {
			return ErrAccountLink
		}
		d, e := m.security.loadData(r)
		if e != nil {
			return e
		}
		if g.Purpose == "invite" && accounts.CheckPermission(r.Account, accounts.ChangeDisplayName) {
			if _, e = tx.SetProfile(ctx, profileVersion, r.Account.Username, label); e != nil {
				return e
			}
		}
		if g.DisableMFA {
			d.Secret = ""
			d.LastStep = 0
			d.Recovery = nil
			d.ExtraCode = false
		}
		if g.RemovePasskeys {
			for _, k := range d.Passkeys {
				if e = tx.ReleasePasskey(ctx, k.Credential.ID); e != nil {
					return e
				}
			}
			d.Passkeys = nil
		}
		r.PasswordHash = hash
		r.Version++
		r.ChangedAt = m.auth.now()
		if e = m.security.saveData(r, d); e != nil {
			return e
		}
		if e = tx.RevokeSessions(ctx, []byte{}, ""); e != nil {
			return e
		}
		return tx.Event(ctx, g.Purpose+"_redeemed", m.auth.now())
	})
}

// UpdatePermissions checks a complete proposed set against the locked current
// actor and target. Changes to unowned grants are forbidden, unchanged grants
// can be preserved. Optimistic revision checks prevent accidental lost edits.
func (m *Management) UpdatePermissions(ctx context.Context, token, id string, version int64, grants []string, requireMFA bool) (accounts.Account, error) {
	var out accounts.Account
	if grants == nil {
		grants = []string{}
	}
	err := m.manage(ctx, token, id, accounts.ManagePermissions, func(r *SecurityRecord, tx ManagedTx) error {
		actor := tx.Actor()
		if err := accounts.GrantChangesAllowed(actor, r.Account.DirectPermissions, grants); err != nil {
			return err
		}
		if err := tx.SetPermissions(ctx, version, grants, requireMFA, m.auth.now()); err != nil {
			return err
		}
		r.Account.DirectPermissions = grants
		r.Account.RequireMFA = requireMFA
		r.Account.PermissionsVersion++
		// Refresh the non-secret enrolment projection from authoritative encrypted data.
		d, err := m.security.loadData(r)
		if err != nil {
			return err
		}
		if err = m.security.saveData(r, d); err != nil {
			return err
		}
		out = r.Account
		return nil
	})
	return out, err
}
