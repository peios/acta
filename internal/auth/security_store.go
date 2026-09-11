package auth

import (
	"context"
	"time"

	"acta/internal/accounts"
)

// SecurityStore serializes security changes and session issuance by locking the
// active account before flows/sessions. The callback and all its writes commit
// together, or all roll back. Implementations must use this order everywhere.
type SecurityStore interface {
	DeviceStore
	OAuthStore
	WithSecurity(context.Context, string, func(*SecurityRecord, SecurityTx) error) error
	// WithLoginSecurity also loads disabled accounts solely for credential
	// verification. The caller must reject disabled/pending accounts before
	// issuing any session or continuing a settings action.
	WithLoginSecurity(context.Context, string, func(*SecurityRecord, SecurityTx) error) error
	CreateFlow(context.Context, StoredFlow) error
	ReadFlow(context.Context, []byte) (StoredFlow, error)
	DeleteFlow(context.Context, []byte) error
	EnsureSecurityKey(context.Context, []byte) error
}
type SecurityRecord struct {
	Account      accounts.Account
	Version      int64
	ChangedAt    time.Time
	PasswordHash string
	Encrypted    []byte
}
type StoredFlow struct {
	Digest    []byte
	AccountID *string
	ExpiresAt time.Time
	Encrypted []byte
}
type Proof struct {
	Method       string    `json:"method"`
	CredentialID string    `json:"credential_id,omitempty"`
	At           time.Time `json:"at"`
	MFA          bool      `json:"mfa"`
	Version      int64     `json:"version"`
}
type SecuritySession struct {
	ID           string    `json:"id"`
	Digest       []byte    `json:"-"`
	AccountID    string    `json:"-"`
	AuthorizedBy string    `json:"-"`
	Description  string    `json:"description"`
	Kind         string    `json:"kind"`
	Tools        []string  `json:"tool_grants,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	Current      bool      `json:"current"`
	Proof        Proof     `json:"-"`
}
type SecurityTx interface {
	OwnedAgent(context.Context, string) (accounts.Account, SecurityTx, error)
	OAuthTx
	Flow(context.Context, []byte) (StoredFlow, error)
	InsertFlow(context.Context, StoredFlow) error
	SaveFlow(context.Context, StoredFlow) error
	DeleteFlow(context.Context, []byte) error
	Session(context.Context, []byte, time.Time) (SecuritySession, error)
	Sessions(context.Context, time.Time) ([]SecuritySession, error)
	PutSession(context.Context, SecuritySession) error
	// Update only a live MCP session belonging to this transaction account. Compare
	// the previous grant set under the session lock; never recreate revoked sessions.
	SetMCPGrants(context.Context, string, []string, []string, time.Time) error
	RevokeSessions(context.Context, []byte, string) error // keep digest; optional public session UUID
	// ClaimPasskey reserves a credential ID globally; false means already owned.
	ClaimPasskey(context.Context, []byte) (bool, error)
	ReleasePasskey(context.Context, []byte) error
	Event(context.Context, string, time.Time) error
}
