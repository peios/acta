package auth

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/google/uuid"

	"acta/internal/accounts"
)

func (s *Security) Advance(ctx context.Context, in FlowInput, session, binding, address, description string) (FlowResult, error) {
	if !validToken(in.ID) {
		return FlowResult{}, ErrFlow
	}
	raw, err := s.store.ReadFlow(ctx, Digest(in.ID))
	if err != nil {
		return FlowResult{}, ErrFlow
	}
	initial, err := s.decodeFlow(raw, in.ID, binding)
	if err != nil {
		return FlowResult{}, err
	}
	// Unknown tokens must not create rate-limit rows. Only a real, browser-bound
	// flow is entitled to its own attempt bucket.
	if err := s.auth.throttle(ctx, "security-flow:"+in.ID, 30, 5*time.Minute); err != nil {
		return FlowResult{}, err
	}
	id := initial.AccountID
	var assertion *protocol.ParsedCredentialAssertionData
	if in.Action == "passkey_finish" {
		assertion, err = protocol.ParseCredentialRequestResponseBytes(in.Credential)
		if err != nil {
			return FlowResult{}, &accounts.FieldError{
				Field:   "credential",
				Message: "That passkey response could not be verified. Try again.",
			}
		}
		if id == "" {
			u, e := uuid.FromBytes(assertion.Response.UserHandle)
			if e != nil {
				return FlowResult{}, ErrCredentials
			}
			id = u.String()
		}
	}
	if id == "" {
		return FlowResult{}, ErrFlow
	}
	if in.Action == "code" || in.Action == "setup_verify" {
		if err := s.auth.throttle(ctx, "security-code:"+id, 10, 5*time.Minute); err != nil {
			return FlowResult{}, err
		}
	}
	var checkedHash, newHash string
	if in.Action == "password" {
		if err = s.auth.throttle(ctx, "security-password:"+id, 10, 5*time.Minute); err != nil {
			return FlowResult{}, err
		}
		err = s.store.WithSecurity(
			ctx,
			id,
			func(r *SecurityRecord, tx SecurityTx) error { checkedHash = r.PasswordHash; return nil },
		)
		if err != nil {
			return FlowResult{}, err
		}
		ok, e := s.auth.CheckPassword(in.Password, checkedHash)
		if e != nil {
			return FlowResult{}, e
		}
		if !ok {
			return FlowResult{}, &accounts.FieldError{
				Field:   "password",
				Message: "Your current password is incorrect.",
			}
		}
		if initial.Purpose == "password" {
			newHash, e = s.auth.NewPasswordHash(in.NewPassword)
			if e != nil {
				return FlowResult{}, e
			}
		}
	}
	var result FlowResult
	withRecord := s.store.WithSecurity
	if initial.Purpose == "login" {
		withRecord = s.store.WithLoginSecurity
	}
	err = withRecord(ctx, id, func(r *SecurityRecord, tx SecurityTx) error {
		raw, e := tx.Flow(ctx, Digest(in.ID))
		if e != nil {
			return ErrFlow
		}
		f, e := s.decodeFlow(raw, in.ID, binding)
		if e != nil {
			return e
		}
		if f.AccountID != "" && f.AccountID != id {
			return ErrFlow
		}
		if f.AccountID == "" {
			if f.CreatedAt.Before(r.ChangedAt) {
				return ErrFlow
			}
			f.AccountID = id
			f.Version = r.Version
		}
		if r.Version != f.Version {
			return ErrFlow
		}
		d, e := s.loadData(r)
		if e != nil {
			return e
		}
		var active SecuritySession
		if f.Purpose != "login" {
			if e := securityActionAllowed(r.Account, f.Purpose); e != nil {
				return e
			}
			if !bytes.Equal(f.SessionHash, Digest(session)) {
				return ErrFlow
			}
			active, e = tx.Session(ctx, f.SessionHash, s.now())
			if e != nil {
				return e
			}
		}
		verified := false
		switch in.Action {
		case "password":
			if f.Step != "verify" && f.Step != "password" {
				return ErrFlow
			}
			if r.PasswordHash != checkedHash {
				return ErrFlow
			}
			f.PasswordHash = newHash
			f.Proof = Proof{Method: "password", At: s.now(), Version: r.Version}
			// The explicit current-password field does not discard valid recent
			// MFA. Preserve the original deadline so repeated password checks
			// cannot extend the second factor's freshness.
			if active.Proof.MFA && proofValid(d, active.Proof, r.Version, s.now()) {
				f.Proof.MFA = true
				f.Proof.At = active.Proof.At
			}
			verified = true
		case "passkey_begin":
			if e := s.beginPasskeyVerification(r, d, &f); e != nil {
				return e
			}
		case "passkey_finish":
			if e := s.finishPasskeyVerification(r, &d, &f, assertion); e != nil {
				return e
			}
			verified = true
		case "code":
			if f.Step != "code" || f.Proof.Method == "" || !s.now().Before(f.Proof.At.Add(FreshLifetime)) {
				return ErrFlow
			}
			if e = s.consumeCode(&d, in.Code, in.Recovery); e != nil {
				return e
			}
			f.Proof.MFA = true
			verified = true
		default:
			if e := s.advanceVerifiedAction(ctx, tx, r, &d, &f, in); e != nil {
				return e
			}
		}
		if verified {
			if r.Account.DisabledAt != nil {
				return ErrAccountDisabled
			}
			if r.Account.Pending {
				return ErrCredentials
			}
			if needsCode(d, f.Proof.Method) && !f.Proof.MFA {
				f.Step = "code"
			} else {
				if f.Purpose == "login" {
					result, e = s.loginComplete(ctx, tx, r, d, f, description)
					return e
				}
				active.Proof = f.Proof
				if e = tx.PutSession(ctx, active); e != nil {
					return e
				}
				if e = s.prepare(&f, r, d); e != nil {
					return e
				}
			}
		}
		if f.Step == "commit" {
			result, e = s.commit(ctx, tx, r, d, f, active)
			return e
		}
		result, e = s.persist(ctx, tx, r, d, f)
		return e
	})
	return result, err
}

// advanceVerifiedAction runs inside the caller's security transaction. Every
// settings action requires a fresh proof, including registration continuations.
func (s *Security) advanceVerifiedAction(ctx context.Context, tx SecurityTx, r *SecurityRecord, d *SecurityData, f *Flow, in FlowInput) error {
	if !proofValid(*d, f.Proof, r.Version, s.now()) {
		return ErrFlow
	}
	switch in.Action {
	case "registration_begin":
		return s.beginPasskeyRegistration(r, *d, f, in.Name)
	case "registration_finish":
		return s.finishPasskeyRegistration(ctx, tx, r, d, f, in.Credential)
	case "setup_verify":
		if f.Step != "totp_setup" {
			return ErrFlow
		}
		step, ok := validTOTPStep(f.Secret, strings.TrimSpace(in.Code), s.now(), 0)
		if !ok {
			return &accounts.FieldError{
				Field:   "code",
				Message: "That code is incorrect or expired. Try a new code.",
			}
		}
		f.LastStep = step
		var e error
		f.Codes, f.Hashes, e = recoveryCodes()
		if e != nil {
			return e
		}
		f.Step = "recovery_codes"
	case "acknowledge":
		if f.Step != "recovery_codes" {
			return ErrFlow
		}
		f.Step = "commit"
	case "confirm":
		if f.Step != "confirm" {
			return ErrFlow
		}
		f.Step = "commit"
	default:
		return ErrFlow
	}
	return nil
}
