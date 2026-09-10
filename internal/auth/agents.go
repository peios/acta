package auth

import (
	"acta2/internal/accounts"
	"context"
	"github.com/google/uuid"
	"time"
)

type AgentStore interface {
	CreateAgent(context.Context, string, []byte, accounts.Account, time.Time) (accounts.Account, error)
	ListAgents(context.Context, string, []byte, time.Time) ([]accounts.Account, error)
	// Serialize owner management with permission/group changes. Recheck the human
	// owner, session and immutable ownership before exposing the target transaction.
	WithAgent(context.Context, string, []byte, string, time.Time, func(*SecurityRecord, ManagedTx) error) error
}

func (m *Management) agentOwner(ctx context.Context, token string) (accounts.Account, error) {
	a, e := m.auth.Current(ctx, token)
	if e == nil && a.IsAgent() {
		e = ErrForbidden
	}
	return a, e
}
func (m *Management) Agents(ctx context.Context, token string) ([]accounts.Account, error) {
	a, e := m.agentOwner(ctx, token)
	if e != nil {
		return nil, e
	}
	return m.store.ListAgents(ctx, a.ID, Digest(token), m.auth.now())
}
func (m *Management) CreateAgent(ctx context.Context, token, username, display string) (accounts.Account, error) {
	owner, e := m.agentOwner(ctx, token)
	if e != nil {
		return accounts.Account{}, e
	}
	name, label, e := accounts.ValidateProfile(username, display)
	if e != nil {
		return accounts.Account{}, e
	}
	a := accounts.Account{ID: uuid.NewString(), Username: name, DisplayName: label, ParentID: &owner.ID}
	a, e = m.store.CreateAgent(ctx, owner.ID, Digest(token), a, m.auth.now())
	return a, profileError(e)
}
func (m *Management) agent(ctx context.Context, token, id string, fn func(*SecurityRecord, ManagedTx) error) error {
	if _, e := uuid.Parse(id); e != nil {
		return ErrNotFound
	}
	owner, e := m.agentOwner(ctx, token)
	if e != nil {
		return e
	}
	return m.store.WithAgent(ctx, owner.ID, Digest(token), id, m.auth.now(), fn)
}
func (m *Management) Agent(ctx context.Context, token, id string) (accounts.Account, error) {
	var a accounts.Account
	e := m.agent(ctx, token, id, func(r *SecurityRecord, _ ManagedTx) error { a = r.Account; return nil })
	return a, e
}
func (m *Management) UpdateAgent(ctx context.Context, token, id string, version int64, username, display string) (accounts.Account, error) {
	name, label, e := accounts.ValidateProfile(username, display)
	if e != nil {
		return accounts.Account{}, e
	}
	var a accounts.Account
	e = m.agent(ctx, token, id, func(_ *SecurityRecord, tx ManagedTx) error {
		var err error
		a, err = tx.SetProfile(ctx, version, name, label)
		return err
	})
	return a, profileError(e)
}
func (m *Management) AgentPermissions(ctx context.Context, token, id string, version int64, grants []string) (accounts.Account, error) {
	var a accounts.Account
	if grants == nil {
		grants = []string{}
	}
	e := m.agent(ctx, token, id, func(r *SecurityRecord, tx ManagedTx) error {
		if e := accounts.AgentGrantsAllowed(tx.Actor(), grants); e != nil {
			return e
		}
		if e := tx.SetPermissions(ctx, version, grants, false, m.auth.now()); e != nil {
			return e
		}
		a = r.Account
		a.DirectPermissions = grants
		a.PermissionsVersion++
		return nil
	})
	return a, e
}
func (m *Management) DisableAgent(ctx context.Context, token, id string, disabled bool) error {
	return m.agent(ctx, token, id, func(r *SecurityRecord, tx ManagedTx) error {
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
func (m *Management) AgentSessions(ctx context.Context, token, id string) ([]SecuritySession, error) {
	var sessions []SecuritySession
	e := m.agent(ctx, token, id, func(_ *SecurityRecord, tx ManagedTx) error {
		var e error
		sessions, e = tx.Sessions(ctx, m.auth.now())
		return e
	})
	return sessions, e
}
func (m *Management) RevokeAgent(ctx context.Context, token, id, session string) error {
	if session != "" {
		if _, e := uuid.Parse(session); e != nil {
			return ErrNotFound
		}
	}
	return m.agent(ctx, token, id, func(_ *SecurityRecord, tx ManagedTx) error { return tx.RevokeSessions(ctx, []byte{}, session) })
}

// Both native authorization protocols select the identity through this single
// boundary. The human's live browser session authorizes an owned, active agent;
// the returned transaction is scoped to the selected subject, never the caller.
func (s *Security) connectionIdentity(ctx context.Context, r *SecurityRecord, tx SecurityTx, token, subject string) (accounts.Account, SecuritySession, SecurityTx, error) {
	empty := accounts.Account{}
	var proof SecuritySession
	if r.Account.IsAgent() {
		return empty, proof, nil, ErrForbidden
	}
	if accounts.RequiresMFASetup(r.Account) {
		return empty, proof, nil, ErrMFARequired
	}
	browser, e := tx.Session(ctx, Digest(token), s.now())
	if e != nil {
		return empty, proof, nil, e
	}
	if browser.Kind != "browser" {
		return empty, proof, nil, ErrForbidden
	}
	if subject == "" || subject == r.Account.ID {
		return r.Account, browser, tx, nil
	}
	if _, e = uuid.Parse(subject); e != nil {
		return empty, proof, nil, ErrNotFound
	}
	agent, agentTx, e := tx.OwnedAgent(ctx, subject)
	if e != nil {
		return empty, proof, nil, e
	}
	if !agent.Available() {
		return empty, proof, nil, ErrUnauthenticated
	}
	return agent, browser, agentTx, nil
}
