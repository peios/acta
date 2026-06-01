// Package apitoken issues and authenticates personal access tokens (PATs).
//
// A PAT authenticates as its owning user with full authority — there are no
// scopes in v1. The plaintext is shown to its owner exactly once, at creation;
// only its SHA-256 hash is persisted, so a database leak yields no usable
// credentials. Every token carries an "acta_pat_" prefix so it's recognisable
// in logs and secret scanners.
package apitoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

// Prefix marks every Acta personal access token.
const Prefix = "acta_pat_"

// displayChars is how many characters of the random body are kept (after the
// prefix) for non-secret display in the account UI.
const displayChars = 8

// touchInterval throttles last-used writes so an actively-used token doesn't
// issue a database write on every request.
const touchInterval = time.Minute

// ErrInvalidToken is returned when a presented token is malformed or unknown.
var ErrInvalidToken = errors.New("apitoken: invalid token")

type Service struct {
	store store.Store
	now   func() time.Time // injectable for tests
}

func New(st store.Store) *Service {
	return &Service{store: st, now: time.Now}
}

// Mint creates a new token for userID labelled name, persists only its hash,
// and returns the one-time plaintext alongside the stored record.
func (s *Service) Mint(ctx context.Context, userID, name string) (plaintext string, t store.APIToken, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", store.APIToken{}, err
	}
	body := base64.RawURLEncoding.EncodeToString(b)
	plaintext = Prefix + body
	sum := sha256.Sum256([]byte(plaintext))
	rec := store.APIToken{
		UserID: userID,
		Name:   capName(name),
		Hash:   sum[:],
		Prefix: Prefix + body[:displayChars],
	}
	t, err = s.store.CreateAPIToken(ctx, rec)
	if err != nil {
		return "", store.APIToken{}, err
	}
	return plaintext, t, nil
}

// maxNameLen bounds a token label server-side (the form also caps it).
const maxNameLen = 100

func capName(name string) string {
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > maxNameLen {
		return string(r[:maxNameLen])
	}
	return name
}

// List returns a user's tokens (without secrets) for the account UI.
func (s *Service) List(ctx context.Context, userID string) ([]store.APIToken, error) {
	return s.store.APITokensByUserID(ctx, userID)
}

// Revoke deletes one of the user's tokens.
func (s *Service) Revoke(ctx context.Context, id, userID string) error {
	return s.store.DeleteAPIToken(ctx, id, userID)
}

// Authenticate resolves a presented bearer token to its owning principal,
// refreshing the token's last-used stamp (throttled). It returns
// ErrInvalidToken for anything malformed, unknown, or orphaned — the caller
// must not distinguish these to the client.
func (s *Service) Authenticate(ctx context.Context, plaintext string) (identity.Principal, error) {
	if !strings.HasPrefix(plaintext, Prefix) {
		return identity.Principal{}, ErrInvalidToken
	}
	sum := sha256.Sum256([]byte(plaintext))
	tok, err := s.store.APITokenByHash(ctx, sum[:])
	if errors.Is(err, store.ErrAPITokenNotFound) {
		return identity.Principal{}, ErrInvalidToken
	}
	if err != nil {
		return identity.Principal{}, err
	}
	u, err := s.store.UserByID(ctx, tok.UserID)
	if errors.Is(err, store.ErrUserNotFound) {
		// Token outlived its user.
		return identity.Principal{}, ErrInvalidToken
	}
	if err != nil {
		return identity.Principal{}, err
	}
	now := s.now()
	if tok.LastUsedAt == nil || now.Sub(*tok.LastUsedAt) >= touchInterval {
		_ = s.store.TouchAPIToken(ctx, tok.ID, now)
	}
	return identity.Principal{ID: u.ID, Username: u.Username, Display: u.Display}, nil
}
