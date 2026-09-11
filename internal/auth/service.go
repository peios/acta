package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"acta/internal/accounts"
	"github.com/google/uuid"
)

const SessionIdle = 7 * 24 * time.Hour
const SessionLifetime = 30 * 24 * time.Hour
const SetupLifetime = 15 * time.Minute

type Service struct {
	store        Store
	setupDigest  []byte
	setupExpires time.Time
	dummyHash    string
	hashSlots    chan struct{}
	now          func() time.Time
}

// New creates process-local setup proof only for an uninitialised installation.
// The returned code is for the operator's terminal only; never an HTTP response.
func New(ctx context.Context, store Store) (*Service, string, error) {
	dummy, err := HashPassword("dummy credential, never a real account")
	if err != nil {
		return nil, "", err
	}
	s := &Service{store: store, dummyHash: dummy, hashSlots: make(chan struct{}, 2), now: time.Now}
	complete, err := store.SetupComplete(ctx)
	if err != nil {
		return nil, "", err
	}
	if complete {
		return s, "", nil
	}
	code, err := newSetupCode()
	if err != nil {
		return nil, "", err
	}
	s.setupDigest = Digest(code)
	s.setupExpires = s.now().Add(time.Hour)
	return s, code, nil
}

const setupCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newSetupCode() (string, error) {
	// Five random bytes encode to eight symbols (40 bits), without padding or
	// visually ambiguous I/O/0/1. This is only the short-lived operator proof.
	raw := make([]byte, 5)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.NewEncoding(setupCodeAlphabet).WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

func Token() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func Digest(token string) []byte { sum := sha256.Sum256([]byte(token)); return sum[:] }
func validToken(token string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "cli_"))
	return err == nil && len(raw) == 32
}

func (s *Service) State(ctx context.Context, grant string) (complete, unlocked bool, err error) {
	complete, err = s.store.SetupComplete(ctx)
	if err != nil || complete || !validToken(grant) {
		return
	}
	unlocked, err = s.store.SetupGrantValid(ctx, Digest(grant), s.now())
	return
}

func (s *Service) throttle(ctx context.Context, key string, limit int, window time.Duration) error {
	ok, err := s.store.Attempt(ctx, Digest(key), limit, window, s.now())
	if err != nil {
		return err
	}
	if !ok {
		return ErrRateLimited
	}
	return nil
}

func (s *Service) Unlock(ctx context.Context, code, address string) (string, error) {
	if err := s.throttle(ctx, "setup:"+address, 10, 5*time.Minute); err != nil {
		return "", err
	}
	complete, err := s.store.SetupComplete(ctx)
	if err != nil {
		return "", err
	}
	if complete {
		return "", ErrSetupComplete
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if !s.now().Before(s.setupExpires) || subtle.ConstantTimeCompare(s.setupDigest, Digest(code)) != 1 {
		return "", &accounts.FieldError{Field: "code", Message: "That setup code is incorrect or has expired. Check the server terminal."}
	}
	token, err := Token()
	if err != nil {
		return "", err
	}
	if err = s.store.GrantSetup(ctx, Digest(token), s.now().Add(SetupLifetime)); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Service) Complete(ctx context.Context, grant, rawUsername, rawPassword, rawDisplayName string) (accounts.Account, error) {
	var empty accounts.Account
	complete, unlocked, err := s.State(ctx, grant)
	if err != nil {
		return empty, err
	}
	if complete {
		return empty, ErrSetupComplete
	}
	if !unlocked {
		return empty, ErrSetupGrant
	}
	username, err := accounts.Username(rawUsername)
	if err != nil {
		return empty, &accounts.FieldError{Field: "username", Message: err.Error()}
	}
	password, err := ValidatePassword(rawPassword)
	if err != nil {
		return empty, &accounts.FieldError{Field: "password", Message: err.Error()}
	}
	displayName, err := accounts.DisplayName(rawDisplayName)
	if err != nil {
		return empty, &accounts.FieldError{Field: "display_name", Message: err.Error()}
	}
	if err = s.throttle(ctx, "setup-grant:"+grant, 10, 5*time.Minute); err != nil {
		return empty, err
	}
	if !s.acquire() {
		return empty, ErrBusy
	}
	defer s.release()
	hash, err := HashPassword(password)
	if err != nil {
		return empty, err
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return empty, err
	}
	account := accounts.Account{ID: id.String(), Username: username, DisplayName: displayName, DirectPermissions: []string{accounts.Superuser}}
	err = s.store.CompleteSetup(ctx, Digest(grant), account, hash, s.now())
	return account, err
}

func (s *Service) VerifyCredentials(ctx context.Context, rawUsername, rawPassword, address string) (Credential, error) {
	if err := s.throttle(ctx, "login-ip:"+address, 60, 5*time.Minute); err != nil {
		return Credential{}, err
	}
	username, nameErr := accounts.Username(rawUsername)
	if nameErr == nil {
		if err := s.throttle(ctx, "login-account:"+username, 10, 5*time.Minute); err != nil {
			return Credential{}, err
		}
	}
	var err error
	credential := Credential{PasswordHash: s.dummyHash}
	found := false
	if nameErr == nil {
		credential, err = s.store.CredentialByUsername(ctx, username)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return Credential{}, err
		}
		found = err == nil
		if !found {
			credential.PasswordHash = s.dummyHash
		}
	}
	matches, err := s.CheckPassword(rawPassword, credential.PasswordHash)
	if err != nil {
		return Credential{}, err
	}
	if !found || !matches {
		return Credential{}, ErrCredentials
	}
	if credential.Account.DisabledAt != nil {
		return Credential{}, ErrAccountDisabled
	}
	if credential.Account.Pending {
		return Credential{}, ErrCredentials
	}
	return credential, nil
}

func (s *Service) Current(ctx context.Context, token string) (accounts.Account, error) {
	a, err := s.Authenticated(ctx, token)
	if err == nil && accounts.RequiresMFASetup(a) {
		return a, ErrMFARequired
	}
	return a, err
}

// Authenticated permits only account identity, logout and required MFA setup.
// Ordinary features must use Current, which also enforces account policy.
func (s *Service) Authenticated(ctx context.Context, token string) (accounts.Account, error) {
	if !validToken(token) {
		return accounts.Account{}, ErrUnauthenticated
	}
	now := s.now()
	account, err := s.store.UseSession(ctx, Digest(token), now, now.Add(-SessionIdle))
	if errors.Is(err, ErrNotFound) {
		return accounts.Account{}, ErrUnauthenticated
	}
	return account, err
}
func (s *Service) Logout(ctx context.Context, token string) error {
	if !validToken(token) {
		return nil
	}
	return s.store.DeleteSession(ctx, Digest(token))
}
func (s *Service) Cleanup(ctx context.Context) error {
	now := s.now()
	if err := s.store.Cleanup(ctx, now, now.Add(-SessionIdle)); err != nil {
		return fmt.Errorf("clean expired authentication state: %w", err)
	}
	return nil
}
func (s *Service) acquire() bool {
	select {
	case s.hashSlots <- struct{}{}:
		return true
	default:
		return false
	}
}
func (s *Service) release() { <-s.hashSlots }

func (s *Service) CheckPassword(raw, hash string) (bool, error) {
	if !s.acquire() {
		return false, ErrBusy
	}
	defer s.release()
	password, err := NormalizePassword(raw)
	if err != nil || utf8.RuneCountInString(password) > PasswordMax {
		password = ""
	}
	return VerifyPassword(password, hash), nil
}
func (s *Service) NewPasswordHash(raw string) (string, error) {
	password, err := ValidatePassword(raw)
	if err != nil {
		return "", &accounts.FieldError{Field: "new_password", Message: err.Error()}
	}
	if !s.acquire() {
		return "", ErrBusy
	}
	defer s.release()
	return HashPassword(password)
}
