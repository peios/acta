package auth

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"acta/internal/accounts"
)

// These action helpers mutate the caller's flow and security data inside its
// existing security transaction. The caller owns persistence and rollback.
func (s *Security) beginPasskeyVerification(r *SecurityRecord, d SecurityData, f *Flow) error {
	if f.Step != "verify" && f.Step != "passkey_verify" {
		return ErrFlow
	}
	if f.Purpose == "password" {
		return ErrFlow
	}
	opts, ws, e := s.web.BeginLogin(
		webUser{r.Account, d.Passkeys},
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if e != nil {
		return &accounts.FieldError{
			Field:   "credential",
			Message: "No available passkey. Verify with your password.",
		}
	}
	f.WebSession = ws
	f.Options, _ = json.Marshal(opts)
	f.Step = "passkey_verify"
	return nil
}

func (s *Security) finishPasskeyVerification(r *SecurityRecord, d *SecurityData, f *Flow, assertion *protocol.ParsedCredentialAssertionData) error {
	if f.Step != "passkey_verify" || f.WebSession == nil {
		return ErrFlow
	}
	user := webUser{r.Account, d.Passkeys}
	var credential *webauthn.Credential
	var e error
	if len(f.WebSession.UserID) == 0 {
		credential, e = s.web.ValidateDiscoverableLogin(func(rawID, handle []byte) (webauthn.User, error) {
			if !bytes.Equal(handle, user.WebAuthnID()) {
				return nil, ErrCredentials
			}
			return user, nil
		}, *f.WebSession, assertion)
	} else {
		credential, e = s.web.ValidateLogin(user, *f.WebSession, assertion)
	}
	if e != nil || credential.Authenticator.CloneWarning {
		return &accounts.FieldError{
			Field:   "credential",
			Message: "That passkey could not be verified. Start again.",
		}
	}
	found := false
	for i := range d.Passkeys {
		if bytes.Equal(d.Passkeys[i].Credential.ID, credential.ID) {
			now := s.now()
			d.Passkeys[i].Credential = *credential
			d.Passkeys[i].LastUsedAt = &now
			f.Proof = Proof{Method: "passkey", CredentialID: d.Passkeys[i].ID, At: now, Version: r.Version}
			found = true
			break
		}
	}
	if !found {
		return ErrCredentials
	}
	f.WebSession = nil
	f.Options = nil
	return nil
}

func (s *Security) beginPasskeyRegistration(r *SecurityRecord, d SecurityData, f *Flow, inputName string) error {
	if f.Step != "passkey_name" && f.Step != "passkey_create" {
		return ErrFlow
	}
	name, e := accounts.DisplayName(inputName)
	if e != nil || name == nil {
		return &accounts.FieldError{
			Field:   "name",
			Message: "Give this passkey a name (up to 100 characters).",
		}
	}
	if len(d.Passkeys) >= 20 {
		return &accounts.FieldError{
			Field:   "name",
			Message: "Remove an unused passkey before adding another (limit 20).",
		}
	}
	f.Name = *name
	opts, ws, e := s.web.BeginRegistration(
		webUser{r.Account, d.Passkeys},
		webauthn.WithExclusions(webUser{r.Account, d.Passkeys}.WebAuthnCredentialsDescriptors()),
	)
	if e != nil {
		return e
	}
	f.WebSession = ws
	f.Options, _ = json.Marshal(opts)
	f.Step = "passkey_create"
	return nil
}

func (s *Security) finishPasskeyRegistration(ctx context.Context, tx SecurityTx, r *SecurityRecord, d *SecurityData, f *Flow, credentialJSON json.RawMessage) error {
	if f.Step != "passkey_create" || f.WebSession == nil {
		return ErrFlow
	}
	parsed, e := protocol.ParseCredentialCreationResponseBytes(credentialJSON)
	if e != nil {
		return &accounts.FieldError{
			Field:   "credential",
			Message: "The browser response could not be read. Try again.",
		}
	}
	credential, e := s.web.CreateCredential(webUser{r.Account, d.Passkeys}, *f.WebSession, parsed)
	if e != nil {
		return &accounts.FieldError{
			Field:   "credential",
			Message: "The passkey could not be registered. Try again.",
		}
	}
	claimed, e := tx.ClaimPasskey(ctx, credential.ID)
	if e != nil {
		return e
	}
	if !claimed {
		return &accounts.FieldError{
			Field:   "credential",
			Message: "That passkey is already registered.",
		}
	}
	d.Passkeys = append(
		d.Passkeys,
		Passkey{ID: uuid.NewString(), Name: f.Name, CreatedAt: s.now(), Credential: *credential},
	)
	f.Step = "commit"
	return nil
}
