package auth

import (
	"acta/internal/accounts"
	"context"
	"github.com/pquerna/otp/totp"
	"slices"
	"strings"
	"time"
)

func needsCode(d SecurityData, method string) bool {
	return d.Secret != "" && (method == "password" || d.ExtraCode)
}
func proofValid(d SecurityData, p Proof, version int64, now time.Time) bool {
	if p.Version != version || p.At.IsZero() || p.At.After(now) || !now.Before(p.At.Add(FreshLifetime)) {
		return false
	}
	if p.Method != "password" && p.Method != "passkey" {
		return false
	}
	if p.Method == "passkey" && !slices.ContainsFunc(d.Passkeys, func(k Passkey) bool { return k.ID == p.CredentialID }) {
		return false
	}
	return !needsCode(d, p.Method) || p.MFA
}
func (s *Security) prepare(f *Flow, r *SecurityRecord, d SecurityData) error {
	f.Options = nil
	f.WebSession = nil
	switch f.Purpose {
	case "passkey_add":
		f.Step = "passkey_name"
	case "password":
		f.Step = "commit"
	case "mfa_setup":
		key, err := totp.Generate(totp.GenerateOpts{Issuer: "Acta", AccountName: r.Account.Handle(), SecretSize: 20})
		if err != nil {
			return err
		}
		f.Secret = key.Secret()
		f.SecretURL = key.URL()
		f.Step = "totp_setup"
	case "recovery":
		if d.Secret == "" {
			return ErrFlow
		}
		codes, hashes, err := recoveryCodes()
		if err != nil {
			return err
		}
		f.Codes = codes
		f.Hashes = hashes
		f.Step = "recovery_codes"
	case "mfa_disable", "mfa_policy":
		if d.Secret == "" {
			return ErrFlow
		}
		f.Step = "confirm"
	case "passkey_remove":
		if !slices.ContainsFunc(d.Passkeys, func(k Passkey) bool { return k.ID == f.Target }) {
			return ErrFlow
		}
		f.Step = "confirm"
	default:
		return ErrFlow
	}
	return nil
}
func (s *Security) consumeCode(d *SecurityData, code string, recovery bool) error {
	if d.Secret == "" {
		return ErrFlow
	}
	if recovery {
		hash := recoveryDigest(code)
		i := slices.Index(d.Recovery, hash)
		if i < 0 {
			return &accounts.FieldError{Field: "code", Message: "That recovery code is invalid or has already been used."}
		}
		d.Recovery = append(d.Recovery[:i], d.Recovery[i+1:]...)
		return nil
	}
	step, ok := validTOTPStep(d.Secret, strings.TrimSpace(code), s.now(), d.LastStep)
	if !ok {
		return &accounts.FieldError{Field: "code", Message: "That code is incorrect, expired or already used. Try a new code."}
	}
	d.LastStep = step
	return nil
}
func (s *Security) commit(ctx context.Context, tx SecurityTx, r *SecurityRecord, d SecurityData, f Flow, active SecuritySession) (FlowResult, error) {
	if !proofValid(d, f.Proof, r.Version, s.now()) {
		return FlowResult{}, ErrFlow
	}
	revoke := true
	switch f.Purpose {
	case "password":
		if f.PasswordHash == "" {
			return FlowResult{}, ErrFlow
		}
		r.PasswordHash = f.PasswordHash
	case "passkey_add":
		revoke = false
	case "passkey_remove":
		for _, key := range d.Passkeys {
			if key.ID == f.Target {
				if err := tx.ReleasePasskey(ctx, key.Credential.ID); err != nil {
					return FlowResult{}, err
				}
			}
		}
		d.Passkeys = slices.DeleteFunc(d.Passkeys, func(k Passkey) bool { return k.ID == f.Target })
	case "mfa_setup":
		d.Secret = f.Secret
		d.LastStep = f.LastStep
		d.Recovery = f.Hashes
	case "mfa_disable":
		d.Secret = ""
		d.LastStep = 0
		d.Recovery = nil
		d.ExtraCode = false
	case "mfa_policy":
		d.ExtraCode = f.Enabled
	case "recovery":
		d.Recovery = f.Hashes
		revoke = false
	default:
		return FlowResult{}, ErrFlow
	}
	r.Version++
	r.ChangedAt = s.now()
	active.Proof = Proof{}
	if err := tx.PutSession(ctx, active); err != nil {
		return FlowResult{}, err
	}
	if revoke {
		if err := tx.RevokeSessions(ctx, active.Digest, ""); err != nil {
			return FlowResult{}, err
		}
	}
	if err := s.saveData(r, d); err != nil {
		return FlowResult{}, err
	}
	if err := tx.DeleteFlow(ctx, Digest(f.Token)); err != nil {
		return FlowResult{}, err
	}
	if err := tx.Event(ctx, f.Purpose, r.ChangedAt); err != nil {
		return FlowResult{}, err
	}
	return FlowResult{Purpose: f.Purpose, Step: "done", AccountID: r.Account.ID}, nil
}

// Required enrolment reuses the same flow as voluntary enrolment. This guard
// runs both when starting and advancing, under the account lock.
func securityActionAllowed(a accounts.Account, purpose string) error {
	if a.IsAgent() {
		return ErrForbidden
	}
	if accounts.RequiresMFASetup(a) && purpose != "mfa_setup" {
		return ErrMFARequired
	}
	if accounts.RequiresMFA(a) && purpose == "mfa_disable" {
		return ErrForbidden
	}
	return nil
}
