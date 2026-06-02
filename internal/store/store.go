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
	ErrAPITokenNotFound   = errors.New("store: api token not found")
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
	// AgentOfID is the human this user is an agent of, or "" for a human. A user
	// is an agent exactly when AgentOfID != "". An agent's only credential is a
	// personal access token; it never has a password or passkey.
	AgentOfID string
	CreatedAt time.Time
	// DisabledAt is non-nil when the user has been disabled (a soft delete):
	// they keep all their data and attribution but can no longer authenticate.
	DisabledAt *time.Time
}

// NewUser is the input to CreateUser.
type NewUser struct {
	Username     string
	Display      string
	PasswordHash string
	AgentOfID    string // set to make the new user an agent of that human
}

// Session is the persisted server-side session record. ID is the opaque token
// handed to the client in a cookie and is the only secret in the row — it must
// never be exposed in a response. PublicID is a non-secret handle used to
// address a session in the account UI (list/revoke) so the token stays server-
// side. UserAgent is captured at establish time to label the session.
type Session struct {
	ID        string
	PublicID  string
	UserID    string
	UserAgent string
	CreatedAt time.Time
	ExpiresAt time.Time // absolute expiry — hard ceiling on session lifetime
	LastSeen  time.Time // for idle expiry — refreshed as the session is used
}

// APIToken is a personal access token: a credential that authenticates as its
// owning user with full authority (no scopes in v1). Hash is the SHA-256 of the
// plaintext — the plaintext itself is never persisted. Prefix is the leading,
// non-secret part of the token, kept only for display.
type APIToken struct {
	ID         string
	UserID     string
	Name       string
	Hash       []byte
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time // nil until first use
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
// cheap workspace-wide queries and cascade integrity. AssigneeID is the
// optional owner ("" = unassigned); ArchivedAt is nil for active items and set
// when an item is archived (soft-deleted — hidden from the board, restorable).
type Item struct {
	ID          string
	WorkspaceID string
	StatusID    string
	ParentID    string // "" for a top-level (board) item; otherwise its parent
	Title       string
	Description string
	AssigneeID  string
	Position    int
	IsMilestone bool
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	// CreatedBy is the principal (human or agent) that created the item, or ""
	// if unrecorded (items predating authorship, or a since-deleted creator).
	CreatedBy string
}

// SubtaskCount is a parent's direct-child progress: Total active children and
// how many of them sit in the "done" status.
type SubtaskCount struct {
	Done  int
	Total int
}

// Comment is an append-only note by a user on an item. AuthorID may be empty if
// the author's account was later removed.
type Comment struct {
	ID        string
	ItemID    string
	AuthorID  string
	Body      string
	CreatedAt time.Time
}

// Store is the persistence interface for Acta.
type Store interface {
	CreateUser(ctx context.Context, u NewUser) (User, error)
	UserByUsername(ctx context.Context, username string) (User, error)
	UserByID(ctx context.Context, id string) (User, error)
	ListUsers(ctx context.Context) ([]User, error)
	// AgentsByOwner returns the agents belonging to a human, ordered by handle.
	AgentsByOwner(ctx context.Context, ownerID string) ([]User, error)
	// DeleteUser removes a user. Owned agents and credentials/tokens/sessions
	// cascade away; items the user created keep their history with a null
	// creator. Currently used only for agent removal.
	DeleteUser(ctx context.Context, id string) error
	// SetUserPassword replaces a user's password hash. Returns ErrUserNotFound
	// if no such user exists.
	SetUserPassword(ctx context.Context, id, passwordHash string) error
	// SetUserDisabled toggles a user's disabled flag (true sets disabled_at to
	// now, false clears it). Returns ErrUserNotFound if no such user exists.
	SetUserDisabled(ctx context.Context, id string, disabled bool) error

	CreateSession(ctx context.Context, s Session) error
	SessionByID(ctx context.Context, id string) (Session, error)
	// SessionsByUserID returns a user's unexpired sessions, most-recently-seen
	// first, for the account UI. The opaque token (ID) is still populated but
	// must not be rendered — address sessions by PublicID.
	SessionsByUserID(ctx context.Context, userID string, now time.Time) ([]Session, error)
	TouchSession(ctx context.Context, id string, lastSeen time.Time) error
	DeleteSession(ctx context.Context, id string) error
	// DeleteUserSession revokes one of a user's sessions by its non-secret
	// PublicID; scoping to userID stops one user revoking another's session.
	DeleteUserSession(ctx context.Context, publicID, userID string) error
	// DeleteOtherSessions revokes all of a user's sessions except keepID (the
	// current session's token) — "sign out everywhere else".
	DeleteOtherSessions(ctx context.Context, userID, keepID string) (int64, error)
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error)

	// API tokens. CreateAPIToken persists a freshly minted token (the caller
	// supplies the hash). APITokenByHash resolves an incoming bearer token to
	// its row for authentication; it returns ErrAPITokenNotFound on no match.
	CreateAPIToken(ctx context.Context, t APIToken) (APIToken, error)
	APITokensByUserID(ctx context.Context, userID string) ([]APIToken, error)
	APITokenByHash(ctx context.Context, hash []byte) (APIToken, error)
	TouchAPIToken(ctx context.Context, id string, lastUsed time.Time) error
	DeleteAPIToken(ctx context.Context, id, userID string) error

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

	// Items (board cards). ItemsByWorkspace and ItemsByStatus return only
	// active (non-archived) items, ordered by position. ReorderItems sets each
	// id's status to statusID and its position to its index in the slice,
	// atomically — this is how an item both moves between lanes and gets
	// ordered within one. ItemByID returns an item regardless of archive state.
	CreateItem(ctx context.Context, i Item) (Item, error)
	ItemsByWorkspace(ctx context.Context, workspaceID string) ([]Item, error)
	// AllItemsByWorkspace returns every active item (top-level and nested), for
	// the reparent picker.
	AllItemsByWorkspace(ctx context.Context, workspaceID string) ([]Item, error)
	ItemsByStatus(ctx context.Context, statusID string) ([]Item, error)
	ArchivedItemsByWorkspace(ctx context.Context, workspaceID string) ([]Item, error)
	ItemByID(ctx context.Context, id string) (Item, error)
	// ChildrenByParent returns an item's direct children ordered by position;
	// includeArchived false omits archived ones (the modal list), true keeps
	// them (cascade walks).
	ChildrenByParent(ctx context.Context, parentID string, includeArchived bool) ([]Item, error)
	// SubtaskCountsByWorkspace returns per-parent direct-child progress for the
	// top-level board, counting children in doneStatusID as done.
	SubtaskCountsByWorkspace(ctx context.Context, workspaceID, doneStatusID string) (map[string]SubtaskCount, error)
	RenameItem(ctx context.Context, id, title string) error
	UpdateItemDescription(ctx context.Context, id, description string) error
	SetItemAssignee(ctx context.Context, id, assigneeID string) error
	SetItemStatus(ctx context.Context, id, statusID string) error
	SetItemParent(ctx context.Context, id, parentID string) error
	SetItemMilestone(ctx context.Context, id string, isMilestone bool) error
	ArchiveItem(ctx context.Context, id string) error
	UnarchiveItem(ctx context.Context, id string) error
	ReorderItems(ctx context.Context, statusID string, orderedIDs []string) error
	SetItemPositions(ctx context.Context, orderedIDs []string) error
	DeleteItem(ctx context.Context, id string) error

	// Comments on an item, returned oldest-first.
	CreateComment(ctx context.Context, c Comment) (Comment, error)
	CommentsByItem(ctx context.Context, itemID string) ([]Comment, error)

	Close()
}
