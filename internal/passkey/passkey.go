// Package passkey wraps go-webauthn into a small service the rest of the app
// can use without touching WebAuthn internals. It owns the ceremony plumbing
// (challenge persistence, the User adapter, begin/finish for both
// registration and usernameless login) and speaks in identity.Principal and
// store.Credential — never in raw protocol types.
//
// It's shared by two callers: the local auth provider (for login) and the
// web account handlers (for registration / listing / removal).
package passkey

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

// ErrChallengeExpired is returned when a ceremony is finished after its
// challenge's short TTL.
var ErrChallengeExpired = errors.New("passkey: challenge expired")

type Config struct {
	RPID     string
	RPOrigin string
	RPName   string
}

type Service struct {
	wa    *webauthn.WebAuthn
	store store.Store
	ttl   time.Duration
}

func New(st store.Store, cfg Config) (*Service, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.RPID,
		RPDisplayName: cfg.RPName,
		RPOrigins:     []string{cfg.RPOrigin},
	})
	if err != nil {
		return nil, err
	}
	return &Service{wa: wa, store: st, ttl: 5 * time.Minute}, nil
}

// --- registration ---

// BeginRegistration returns the JSON creation options for the browser and a
// challenge id the caller must round-trip (via a short cookie) to Finish.
func (s *Service) BeginRegistration(ctx context.Context, p identity.Principal) (options []byte, challengeID string, err error) {
	user, err := s.userAdapter(ctx, p.ID)
	if err != nil {
		return nil, "", err
	}
	// Resident (discoverable) key so the credential can be used for
	// usernameless login later.
	sel := protocol.AuthenticatorSelection{
		ResidentKey:      protocol.ResidentKeyRequirementRequired,
		UserVerification: protocol.VerificationPreferred,
	}
	creation, session, err := s.wa.BeginRegistration(user, webauthn.WithAuthenticatorSelection(sel))
	if err != nil {
		return nil, "", err
	}
	challengeID, err = s.persist(ctx, p.ID, session)
	if err != nil {
		return nil, "", err
	}
	options, err = json.Marshal(creation)
	if err != nil {
		return nil, "", err
	}
	return options, challengeID, nil
}

// FinishRegistration validates the attestation in r and stores the new
// credential under the principal, labelled name (auto-named if empty).
func (s *Service) FinishRegistration(ctx context.Context, p identity.Principal, challengeID string, r *http.Request, name string) error {
	session, err := s.consume(ctx, challengeID)
	if err != nil {
		return err
	}
	user, err := s.userAdapter(ctx, p.ID)
	if err != nil {
		return err
	}
	cred, err := s.wa.FinishRegistration(user, session, r)
	if err != nil {
		return err
	}
	return s.store.CreateCredential(ctx, fromWebauthn(cred, p.ID, name))
}

// --- usernameless login ---

// BeginLogin starts a discoverable (usernameless) assertion: no username, the
// browser offers whichever passkeys it holds for this RP.
func (s *Service) BeginLogin(ctx context.Context) (options []byte, challengeID string, err error) {
	assertion, session, err := s.wa.BeginDiscoverableLogin()
	if err != nil {
		return nil, "", err
	}
	challengeID, err = s.persist(ctx, "", session)
	if err != nil {
		return nil, "", err
	}
	options, err = json.Marshal(assertion)
	if err != nil {
		return nil, "", err
	}
	return options, challengeID, nil
}

// FinishLogin validates the assertion in r, resolves it to a principal, and
// bumps the credential's sign counter / last-used.
func (s *Service) FinishLogin(ctx context.Context, challengeID string, r *http.Request) (identity.Principal, error) {
	session, err := s.consume(ctx, challengeID)
	if err != nil {
		return identity.Principal{}, err
	}
	// The user handle stored in the resident key is our user id.
	handler := func(_, userHandle []byte) (webauthn.User, error) {
		return s.userAdapter(ctx, string(userHandle))
	}
	user, cred, err := s.wa.FinishPasskeyLogin(handler, session, r)
	if err != nil {
		return identity.Principal{}, err
	}
	_ = s.store.TouchCredential(ctx, cred.ID, cred.Authenticator.SignCount, time.Now())

	wu, ok := user.(waUser)
	if !ok {
		return identity.Principal{}, errors.New("passkey: unexpected user type")
	}
	return identity.Principal{ID: wu.u.ID, Username: wu.u.Username, Display: wu.u.Display}, nil
}

// --- management ---

func (s *Service) List(ctx context.Context, userID string) ([]store.Credential, error) {
	return s.store.CredentialsByUserID(ctx, userID)
}

func (s *Service) Delete(ctx context.Context, id, userID string) error {
	return s.store.DeleteCredential(ctx, id, userID)
}

func (s *Service) HasCredentials(ctx context.Context, userID string) (bool, error) {
	creds, err := s.store.CredentialsByUserID(ctx, userID)
	return len(creds) > 0, err
}

// --- internals ---

func (s *Service) userAdapter(ctx context.Context, userID string) (waUser, error) {
	u, err := s.store.UserByID(ctx, userID)
	if err != nil {
		return waUser{}, err
	}
	creds, err := s.store.CredentialsByUserID(ctx, userID)
	if err != nil {
		return waUser{}, err
	}
	return waUser{u: u, creds: creds}, nil
}

func (s *Service) persist(ctx context.Context, userID string, session *webauthn.SessionData) (string, error) {
	data, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	id, err := randID()
	if err != nil {
		return "", err
	}
	ch := store.Challenge{ID: id, UserID: userID, Data: data, ExpiresAt: time.Now().Add(s.ttl)}
	if err := s.store.CreateChallenge(ctx, ch); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Service) consume(ctx context.Context, challengeID string) (webauthn.SessionData, error) {
	ch, err := s.store.ConsumeChallenge(ctx, challengeID)
	if err != nil {
		return webauthn.SessionData{}, err
	}
	if time.Now().After(ch.ExpiresAt) {
		return webauthn.SessionData{}, ErrChallengeExpired
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(ch.Data, &session); err != nil {
		return webauthn.SessionData{}, err
	}
	return session, nil
}

// waUser adapts a store user + its credentials to webauthn.User.
type waUser struct {
	u     store.User
	creds []store.Credential
}

func (w waUser) WebAuthnID() []byte          { return []byte(w.u.ID) }
func (w waUser) WebAuthnName() string        { return w.u.Username }
func (w waUser) WebAuthnDisplayName() string { return w.u.Display }
func (w waUser) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(w.creds))
	for _, c := range w.creds {
		out = append(out, toWebauthn(c))
	}
	return out
}

func toWebauthn(c store.Credential) webauthn.Credential {
	transports := make([]protocol.AuthenticatorTransport, len(c.Transports))
	for i, t := range c.Transports {
		transports[i] = protocol.AuthenticatorTransport(t)
	}
	return webauthn.Credential{
		ID:        c.CredentialID,
		PublicKey: c.PublicKey,
		Transport: transports,
		Authenticator: webauthn.Authenticator{
			AAGUID:    c.AAGUID,
			SignCount: c.SignCount,
		},
	}
}

func fromWebauthn(c *webauthn.Credential, userID, name string) store.Credential {
	transports := make([]string, len(c.Transport))
	for i, t := range c.Transport {
		transports[i] = string(t)
	}
	if name == "" {
		name = "Passkey · " + time.Now().Format("2006-01-02")
	}
	return store.Credential{
		UserID:       userID,
		CredentialID: c.ID,
		PublicKey:    c.PublicKey,
		SignCount:    c.Authenticator.SignCount,
		Transports:   transports,
		AAGUID:       c.Authenticator.AAGUID,
		Name:         name,
	}
}

func randID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
