package auth

import (
	"acta/internal/accounts"
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
)

var ErrSessionGrantsChanged = errors.New("This connection’s access changed. Reload its current access before saving again.")

func validMCPGrants(grants []string) bool {
	for _, g := range grants {
		switch g {
		case MCPMigration, MCPIdentityGrant, MCPTasksRead, MCPTasksWrite, MCPMemoriesRead, MCPMemoriesWrite:
		default:
			return false
		}
	}
	return true
}

func canonicalGrants(grants []string) []string {
	out := append([]string{}, grants...)
	slices.Sort(out)
	return slices.Compact(out)
}

// AmendMCPGrants requires the same live human browser authority as initial
// consent. Subject may only be the human or one of their owned agent accounts.
// Existing tokens, identity, proof and expiry are unchanged.
func (s *Security) AmendMCPGrants(ctx context.Context, token, subject, session string, previous, grants []string) error {
	if _, e := uuid.Parse(session); e != nil {
		return ErrNotFound
	}
	if !validMCPGrants(grants) {
		return ErrForbidden
	}
	a, e := s.auth.Current(ctx, token)
	if e != nil {
		return e
	}
	return s.store.WithSecurity(ctx, a.ID, func(r *SecurityRecord, tx SecurityTx) error {
		if slices.Contains(grants, MCPMigration) && (r.Account.IsAgent() || !accounts.CheckPermission(r.Account, accounts.Superuser)) {
			return ErrForbidden
		}
		_, _, target, e := s.connectionIdentity(ctx, r, tx, token, subject)
		if e != nil {
			return e
		}
		if e = target.SetMCPGrants(ctx, session, canonicalGrants(previous), canonicalGrants(grants), s.now()); e != nil {
			return e
		}
		return target.Event(ctx, "mcp_access_changed", s.now())
	})
}
