// Package auth owns authentication policy and database-independent operations.
package auth

import (
	"context"
	"errors"
	"time"

	"acta2/internal/accounts"
)

var (
	ErrAccountDisabled = errors.New("account disabled")
	ErrForbidden       = accounts.ErrPermissionDenied
	ErrAccountLink     = errors.New("this invitation or recovery link is invalid or expired")
	ErrNotFound        = errors.New("not found")
	ErrSetupComplete   = errors.New("setup is already complete")
	ErrSetupGrant      = errors.New("setup authorisation has expired")
	ErrCredentials     = errors.New("username or password is incorrect")
	ErrUnauthenticated = errors.New("sign in required")
	ErrRateLimited     = errors.New("too many attempts")
	ErrBusy            = errors.New("authentication is busy")
)

type Credential struct {
	Account         accounts.Account
	PasswordHash    string
	SecurityVersion int64
}
type Session struct {
	Digest                           []byte
	AccountID                        string
	CreatedAt, LastSeenAt, ExpiresAt time.Time
}

// Store methods are behavioural contracts, not a generic CRUD interface. An
// implementation must enforce these guarantees under concurrent requests.
type Store interface {
	SetupComplete(context.Context) (bool, error)
	// GrantSetup must refuse to issue a grant after setup is complete.
	GrantSetup(context.Context, []byte, time.Time) error
	SetupGrantValid(context.Context, []byte, time.Time) (bool, error)
	// CompleteSetup consumes a valid, unexpired grant, creates the administrator
	// and credential, permanently closes setup, and invalidates all setup grants
	// in ONE transaction. At most one concurrent caller may succeed.
	CompleteSetup(context.Context, []byte, accounts.Account, string, time.Time) error
	CredentialByUsername(context.Context, string) (Credential, error)
	// UseSession atomically checks absolute/idle expiry and account status, then
	// records activity. Only the stored digest of the opaque token is supplied.
	UseSession(context.Context, []byte, time.Time, time.Time) (accounts.Account, error)
	DeleteSession(context.Context, []byte) error
	// Attempt coordinates fixed-window throttling across application instances.
	Attempt(context.Context, []byte, int, time.Duration, time.Time) (bool, error)
	Cleanup(context.Context, time.Time, time.Time) error
}

var ErrMFARequired = errors.New("Set up multi-factor authentication to continue.")

var ErrPermissionsChanged = errors.New("Permissions changed since they were loaded. Reopen the dialog to review the latest permissions.")
