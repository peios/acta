// Package store is the persistence seam. Everything that touches the database
// goes through the Store interface, so the backend stays swappable and the
// rest of the app can be tested against an in-memory fake.
package store

import (
	"context"
	"errors"
	"time"
)

var (
	ErrUserNotFound       = errors.New("store: user not found")
	ErrSessionNotFound    = errors.New("store: session not found")
	ErrUsernameTaken      = errors.New("store: username already taken")
	ErrCredentialNotFound = errors.New("store: credential not found")
	ErrChallengeNotFound  = errors.New("store: challenge not found")
	ErrWorkspaceNotFound  = errors.New("store: workspace not found")
	ErrWorkspaceNameTaken = errors.New("store: workspace name already taken")
	ErrWorkspaceSlugTaken = errors.New("store: workspace slug already taken")
	ErrStatusNotFound     = errors.New("store: status not found")
	ErrItemNotFound       = errors.New("store: item not found")
)

// User is the persisted account record.
//
// PasswordHash is empty for accounts that authenticate by some non-password
// mechanism — a future passkey-only account, or a Peios kernel-mediated
// principal that has no local credential at all.
type User struct {
	ID           string
	Username     string
	Display      string
	PasswordHash string
	CreatedAt    time.Time
}

// NewUser is the input to CreateUser.
type NewUser struct {
	Username     string
	Display      string
	PasswordHash string
}

// Session is the persisted server-side session record. ID is the opaque token
// handed to the client in a cookie and is the only secret in the row.
type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time // absolute expiry — hard ceiling on session lifetime
	LastSeen  time.Time // for idle expiry — refreshed as the session is used
}

// Credential is a persisted WebAuthn (passkey) credential belonging to a user.
// CredentialID is the authenticator-assigned raw id; it's what an assertion
// references, so it carries a UNIQUE constraint.
type Credential struct {
	ID           string // our row id
	UserID       string
	CredentialID []byte
	PublicKey    []byte
	SignCount    uint32
	Transports   []string
	AAGUID       []byte
	Name         string
	CreatedAt    time.Time
	LastUsedAt   *time.Time // nil until first use
}

// Challenge holds the short-lived WebAuthn ceremony state (the go-webauthn
// SessionData, serialised) that must survive between the begin and finish
// requests. UserID is empty for pre-auth login ceremonies. ID is an opaque
// token also handed to the client in a short-lived cookie.
type Challenge struct {
	ID        string
	UserID    string // empty for usernameless login
	Data      []byte // serialised webauthn.SessionData
	ExpiresAt time.Time
}

// Workspace is a top-level container for work. Everything the user creates
// (boards, items — future slices) will belong to one. Slug is the URL-safe
// identifier used in /w/{slug}/… paths and is immutable once assigned, so
// links never break when a workspace is renamed. Name is the human label and
// is unique case-insensitively. CreatedBy is the id of the creating user and
// may be empty (e.g. the seeded default, or after the creator is deleted).
type Workspace struct {
	ID        string
	Slug      string
	Name      string
	CreatedBy string
	CreatedAt time.Time
}

// Status is a board lane: a named, ordered position within a workspace that
// items sit in. Statuses are user-defined per workspace.
type Status struct {
	ID          string
	WorkspaceID string
	Name        string
	Position    int
	CreatedAt   time.Time
}

// Item is a card on the board: a title living in exactly one status, ordered by
// Position within that lane. WorkspaceID is denormalised from the status for
// cheap workspace-wide queries and cascade integrity.
type Item struct {
	ID          string
	WorkspaceID string
	StatusID    string
	Title       string
	Position    int
	CreatedAt   time.Time
}

// Store is the persistence interface for Acta.
type Store interface {
	CreateUser(ctx context.Context, u NewUser) (User, error)
	UserByUsername(ctx context.Context, username string) (User, error)
	UserByID(ctx context.Context, id string) (User, error)

	CreateSession(ctx context.Context, s Session) error
	SessionByID(ctx context.Context, id string) (Session, error)
	TouchSession(ctx context.Context, id string, lastSeen time.Time) error
	DeleteSession(ctx context.Context, id string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error)

	CreateCredential(ctx context.Context, c Credential) error
	CredentialsByUserID(ctx context.Context, userID string) ([]Credential, error)
	CredentialByCredentialID(ctx context.Context, credentialID []byte) (Credential, error)
	TouchCredential(ctx context.Context, credentialID []byte, signCount uint32, lastUsed time.Time) error
	DeleteCredential(ctx context.Context, id, userID string) error

	// CreateChallenge stores ceremony state; ConsumeChallenge fetches and
	// deletes it in one shot (single-use).
	CreateChallenge(ctx context.Context, c Challenge) error
	ConsumeChallenge(ctx context.Context, id string) (Challenge, error)

	// CreateWorkspace persists w (caller supplies a unique slug and name);
	// it returns ErrWorkspaceNameTaken / ErrWorkspaceSlugTaken on collision.
	// RenameWorkspace changes only the name (the slug is immutable).
	CreateWorkspace(ctx context.Context, w Workspace) (Workspace, error)
	ListWorkspaces(ctx context.Context) ([]Workspace, error)
	WorkspaceByID(ctx context.Context, id string) (Workspace, error)
	WorkspaceBySlug(ctx context.Context, slug string) (Workspace, error)
	RenameWorkspace(ctx context.Context, id, name string) error
	DeleteWorkspace(ctx context.Context, id string) error
	CountWorkspaces(ctx context.Context) (int, error)

	// Statuses (board lanes). StatusesByWorkspace returns them ordered by
	// position. ReorderStatuses sets each id's position to its index in the
	// given slice, atomically.
	CreateStatus(ctx context.Context, s Status) (Status, error)
	StatusesByWorkspace(ctx context.Context, workspaceID string) ([]Status, error)
	StatusByID(ctx context.Context, id string) (Status, error)
	RenameStatus(ctx context.Context, id, name string) error
	ReorderStatuses(ctx context.Context, workspaceID string, orderedIDs []string) error
	DeleteStatus(ctx context.Context, id string) error

	// Items (board cards). ItemsByWorkspace and ItemsByStatus return items
	// ordered by position. ReorderItems sets each id's status to statusID and
	// its position to its index in the slice, atomically — this is how an item
	// both moves between lanes and gets ordered within one.
	CreateItem(ctx context.Context, i Item) (Item, error)
	ItemsByWorkspace(ctx context.Context, workspaceID string) ([]Item, error)
	ItemsByStatus(ctx context.Context, statusID string) ([]Item, error)
	ItemByID(ctx context.Context, id string) (Item, error)
	RenameItem(ctx context.Context, id, title string) error
	ReorderItems(ctx context.Context, statusID string, orderedIDs []string) error
	DeleteItem(ctx context.Context, id string) error

	Close()
}
