package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"image/png"
	"slices"
	"time"
)

func (s *Security) stored(f Flow) (StoredFlow, error) {
	raw, err := seal(s.cipher, "flow:"+base64.RawURLEncoding.EncodeToString(Digest(f.Token)), f)
	var id *string
	if f.AccountID != "" {
		id = &f.AccountID
	}
	return StoredFlow{Digest: Digest(f.Token), AccountID: id, ExpiresAt: f.ExpiresAt, Encrypted: raw}, err
}
func (s *Security) decodeFlow(raw StoredFlow, token, binding string) (Flow, error) {
	var f Flow
	if !s.now().Before(raw.ExpiresAt) {
		return f, ErrFlow
	}
	err := unseal(s.cipher, "flow:"+base64.RawURLEncoding.EncodeToString(raw.Digest), raw.Encrypted, &f)
	if err != nil {
		return f, err
	}
	f.Token = token
	if f.Binding != base64.RawURLEncoding.EncodeToString(Digest(binding)) {
		return f, ErrFlow
	}
	return f, nil
}
func (s *Security) newFlow(purpose, binding string) (Flow, error) {
	token, err := Token()
	now := s.now()
	return Flow{Token: token, Binding: base64.RawURLEncoding.EncodeToString(Digest(binding)), Purpose: purpose, CreatedAt: now, ExpiresAt: now.Add(FlowLifetime), Step: "verify"}, err
}
func (s *Security) result(f Flow) (FlowResult, error) {
	r := FlowResult{ID: f.Token, Purpose: f.Purpose, Step: f.Step, Method: f.Proof.Method, AccountID: f.AccountID, Options: f.Options}
	if f.Step == "totp_setup" {
		r.Secret = f.Secret
		k, err := otp.NewKeyFromURL(f.SecretURL)
		if err != nil {
			return r, err
		}
		img, err := k.Image(240, 240)
		if err != nil {
			return r, err
		}
		var out bytes.Buffer
		if err = png.Encode(&out, img); err != nil {
			return r, err
		}
		r.QR = "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes())
	}
	if f.Step == "recovery_codes" {
		r.Codes = f.Codes
	}
	return r, nil
}
func (s *Security) persist(ctx context.Context, tx SecurityTx, r *SecurityRecord, d SecurityData, f Flow) (FlowResult, error) {
	if err := s.saveData(r, d); err != nil {
		return FlowResult{}, err
	}
	raw, err := s.stored(f)
	if err != nil {
		return FlowResult{}, err
	}
	if err = tx.SaveFlow(ctx, raw); err != nil {
		return FlowResult{}, err
	}
	return s.result(f)
}

// PasswordLogin verifies the expensive password outside the account lock, then
// rechecks its security revision before creating a flow or issuing a session.
func (s *Security) PasswordLogin(ctx context.Context, username, password, binding, address, description string) (FlowResult, error) {
	f, err := s.newFlow("login", binding)
	if err != nil {
		return FlowResult{}, err
	}
	c, err := s.auth.VerifyCredentials(ctx, username, password, address)
	if err != nil {
		return FlowResult{}, err
	}
	f.AccountID = c.Account.ID
	f.Version = c.SecurityVersion
	f.Proof = Proof{Method: "password", At: s.now(), Version: c.SecurityVersion}
	raw, err := s.stored(f)
	if err != nil {
		return FlowResult{}, err
	}
	if err = s.store.CreateFlow(ctx, raw); err != nil {
		return FlowResult{}, err
	}
	var result FlowResult
	err = s.store.WithSecurity(ctx, f.AccountID, func(r *SecurityRecord, tx SecurityTx) error {
		if r.Version != f.Version || r.PasswordHash != c.PasswordHash {
			return ErrFlow
		}
		d, e := s.loadData(r)
		if e != nil {
			return e
		}
		if needsCode(d, "password") {
			f.Step = "code"
			result, e = s.persist(ctx, tx, r, d, f)
			return e
		}
		result, e = s.loginComplete(ctx, tx, r, d, f, description)
		return e
	})
	return result, err
}
func (s *Security) Begin(ctx context.Context, purpose, target string, enabled bool, session, binding, address string) (FlowResult, error) {
	if err := s.auth.throttle(ctx, "security-start:"+address, 40, 5*time.Minute); err != nil {
		return FlowResult{}, err
	}
	if !slices.Contains([]string{"login", "passkey_add", "passkey_remove", "password", "mfa_setup", "mfa_disable", "mfa_policy", "recovery"}, purpose) {
		return FlowResult{}, ErrFlow
	}
	f, err := s.newFlow(purpose, binding)
	if err != nil {
		return FlowResult{}, err
	}
	f.Target = target
	f.Enabled = enabled
	if purpose == "login" {
		opts, webSession, e := s.web.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
		if e != nil {
			return FlowResult{}, e
		}
		f.WebSession = webSession
		f.Options, _ = json.Marshal(opts)
		f.Step = "passkey_verify"
		raw, e := s.stored(f)
		if e != nil {
			return FlowResult{}, e
		}
		if e = s.store.CreateFlow(ctx, raw); e != nil {
			return FlowResult{}, e
		}
		return s.result(f)
	}
	a, err := s.auth.Authenticated(ctx, session)
	if err != nil {
		return FlowResult{}, err
	}
	f.AccountID = a.ID
	f.SessionHash = Digest(session)
	var result FlowResult
	err = s.store.WithSecurity(ctx, a.ID, func(r *SecurityRecord, tx SecurityTx) error {
		if e := securityActionAllowed(r.Account, purpose); e != nil {
			return e
		}
		active, e := tx.Session(ctx, f.SessionHash, s.now())
		if e != nil {
			return e
		}
		d, e := s.loadData(r)
		if e != nil {
			return e
		}
		f.Version = r.Version
		if purpose == "password" {
			f.Step = "password"
		} else if proofValid(d, active.Proof, r.Version, s.now()) {
			f.Proof = active.Proof
			if e = s.prepare(&f, r, d); e != nil {
				return e
			}
		}
		raw, e := s.stored(f)
		if e != nil {
			return e
		}
		if e = tx.InsertFlow(ctx, raw); e != nil {
			return e
		}
		result, e = s.result(f)
		return e
	})
	return result, err
}
func (s *Security) loginComplete(ctx context.Context, tx SecurityTx, r *SecurityRecord, d SecurityData, f Flow, description string) (FlowResult, error) {
	if !proofValid(d, f.Proof, r.Version, s.now()) {
		return FlowResult{}, ErrFlow
	}
	token, err := Token()
	if err != nil {
		return FlowResult{}, err
	}
	now := s.now()
	session := SecuritySession{ID: uuid.NewString(), Digest: Digest(token), AccountID: r.Account.ID, Description: description, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(SessionLifetime), Proof: f.Proof}
	if err = tx.PutSession(ctx, session); err != nil {
		return FlowResult{}, err
	}
	if err = tx.DeleteFlow(ctx, Digest(f.Token)); err != nil {
		return FlowResult{}, err
	}
	if err = s.saveData(r, d); err != nil {
		return FlowResult{}, err
	}
	if err = tx.Event(ctx, "signed_in", now); err != nil {
		return FlowResult{}, err
	}
	return FlowResult{Purpose: "login", Step: "done", Method: f.Proof.Method, AccountID: r.Account.ID, SessionToken: token}, nil
}
func (s *Security) Cancel(ctx context.Context, token, binding string) error {
	if !validToken(token) {
		return ErrFlow
	}
	raw, err := s.store.ReadFlow(ctx, Digest(token))
	if err != nil {
		return nil
	}
	if _, err = s.decodeFlow(raw, token, binding); err != nil {
		return err
	}
	return s.store.DeleteFlow(ctx, Digest(token))
}
