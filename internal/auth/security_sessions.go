package auth

import (
	"acta2/internal/accounts"
	"bytes"
	"context"
	"time"
)

type PasskeyView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}
type SecurityView struct {
	MFA               bool              `json:"mfa"`
	ExtraCode         bool              `json:"extra_code"`
	RecoveryRemaining int               `json:"recovery_remaining"`
	Passkeys          []PasskeyView     `json:"passkeys"`
	Sessions          []SecuritySession `json:"sessions"`
}

func (s *Security) View(ctx context.Context, token string) (SecurityView, error) {
	out := SecurityView{Passkeys: []PasskeyView{}, Sessions: []SecuritySession{}}
	a, err := s.auth.Current(ctx, token)
	if err != nil {
		return out, err
	}
	err = s.store.WithSecurity(ctx, a.ID, func(r *SecurityRecord, tx SecurityTx) error {
		if accounts.RequiresMFASetup(r.Account) {
			return ErrMFARequired
		}
		if _, e := tx.Session(ctx, Digest(token), s.now()); e != nil {
			return e
		}
		d, e := s.loadData(r)
		if e != nil {
			return e
		}
		out.MFA = d.Secret != ""
		out.ExtraCode = d.ExtraCode
		out.RecoveryRemaining = len(d.Recovery)
		for _, k := range d.Passkeys {
			out.Passkeys = append(out.Passkeys, PasskeyView{k.ID, k.Name, k.CreatedAt, k.LastUsedAt})
		}
		out.Sessions, e = tx.Sessions(ctx, s.now())
		for i := range out.Sessions {
			out.Sessions[i].Current = bytes.Equal(out.Sessions[i].Digest, Digest(token))
		}
		return e
	})
	return out, err
}
func (s *Security) Revoke(ctx context.Context, token, id string) error {
	a, err := s.auth.Current(ctx, token)
	if err != nil {
		return err
	}
	return s.store.WithSecurity(ctx, a.ID, func(r *SecurityRecord, tx SecurityTx) error {
		if accounts.RequiresMFASetup(r.Account) {
			return ErrMFARequired
		}
		if _, e := tx.Session(ctx, Digest(token), s.now()); e != nil {
			return e
		}
		return tx.RevokeSessions(ctx, Digest(token), id)
	})
}
