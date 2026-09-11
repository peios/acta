package auth

import (
	"acta/internal/accounts"
	"context"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"net/url"
	"time"
)

var ErrFlow = errors.New("this security flow has expired or your security settings changed; start again")

const FreshLifetime = 5 * time.Minute
const FlowLifetime = 10 * time.Minute

type Passkey struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	CreatedAt  time.Time           `json:"created_at"`
	LastUsedAt *time.Time          `json:"last_used_at"`
	Credential webauthn.Credential `json:"credential"`
}
type SecurityData struct {
	Secret    string
	LastStep  int64
	ExtraCode bool
	Recovery  []string
	Passkeys  []Passkey
}
type Flow struct {
	Token                       string `json:"-"`
	Binding                     string
	SessionHash                 []byte
	AccountID                   string
	Version                     int64
	CreatedAt, ExpiresAt        time.Time
	Purpose, Step, Target, Name string
	Enabled                     bool
	Proof                       Proof
	PasswordHash                string
	Secret, SecretURL           string
	LastStep                    int64
	Codes, Hashes               []string
	WebSession                  *webauthn.SessionData
	Options                     json.RawMessage
}
type FlowResult struct {
	ID           string          `json:"id,omitempty"`
	Purpose      string          `json:"purpose"`
	Step         string          `json:"step"`
	Method       string          `json:"method,omitempty"`
	AccountID    string          `json:"account_id,omitempty"`
	Options      json.RawMessage `json:"options,omitempty"`
	Secret       string          `json:"secret,omitempty"`
	QR           string          `json:"qr,omitempty"`
	Codes        []string        `json:"codes,omitempty"`
	SessionToken string          `json:"-"`
}
type FlowInput struct {
	ID          string          `json:"id"`
	Action      string          `json:"action"`
	Password    string          `json:"password"`
	NewPassword string          `json:"new_password"`
	Code        string          `json:"code"`
	Recovery    bool            `json:"recovery"`
	Name        string          `json:"name"`
	Credential  json.RawMessage `json:"credential"`
}
type Security struct {
	auth   *Service
	store  SecurityStore
	cipher cipher.AEAD
	web    *webauthn.WebAuthn
	now    func() time.Time
}

func NewSecurity(ctx context.Context, a *Service, store SecurityStore, publicURL string, origins []string, key []byte) (*Security, error) {
	u, err := url.Parse(publicURL)
	if err != nil {
		return nil, err
	}
	w, err := webauthn.New(&webauthn.Config{RPDisplayName: "Acta", RPID: u.Hostname(), RPOrigins: origins, AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired, ResidentKey: protocol.ResidentKeyRequirementRequired}})
	if err != nil {
		return nil, err
	}
	c, err := newCipher(key)
	if err != nil {
		return nil, err
	}
	if err = store.EnsureSecurityKey(ctx, Digest(string(key))); err != nil {
		return nil, err
	}
	return &Security{auth: a, store: store, cipher: c, web: w, now: time.Now}, nil
}
func (s *Security) loadData(r *SecurityRecord) (SecurityData, error) {
	d := SecurityData{Passkeys: []Passkey{}, Recovery: []string{}}
	if len(r.Encrypted) == 0 {
		return d, nil
	}
	err := unseal(s.cipher, "account:"+r.Account.ID, r.Encrypted, &d)
	return d, err
}
func (s *Security) saveData(r *SecurityRecord, d SecurityData) error {
	raw, err := seal(s.cipher, "account:"+r.Account.ID, d)
	r.Encrypted = raw
	r.Account.MFAEnrolled = d.Secret != ""
	return err
}

type webUser struct {
	account accounts.Account
	keys    []Passkey
}

func (u webUser) WebAuthnID() []byte   { id, _ := uuid.Parse(u.account.ID); return id[:] }
func (u webUser) WebAuthnName() string { return u.account.Handle() }
func (u webUser) WebAuthnDisplayName() string {
	if u.account.DisplayName != nil {
		return *u.account.DisplayName
	}
	return u.account.Handle()
}
func (u webUser) WebAuthnCredentials() []webauthn.Credential {
	out := []webauthn.Credential{}
	for _, k := range u.keys {
		out = append(out, k.Credential)
	}
	return out
}
func (u webUser) WebAuthnCredentialsDescriptors() []protocol.CredentialDescriptor {
	out := []protocol.CredentialDescriptor{}
	for _, k := range u.keys {
		out = append(out, k.Credential.Descriptor())
	}
	return out
}
