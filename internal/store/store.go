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
	ErrUserNotFound         = errors.New("store: user not found")
	ErrSessionNotFound      = errors.New("store: session not found")
	ErrUsernameTaken        = errors.New("store: username already taken")
	ErrCredentialNotFound   = errors.New("store: credential not found")
	ErrAPITokenNotFound     = errors.New("store: api token not found")
	ErrChallengeNotFound    = errors.New("store: challenge not found")
	ErrWorkspaceNotFound    = errors.New("store: workspace not found")
	ErrWorkspaceNameTaken   = errors.New("store: workspace name already taken")
	ErrWorkspaceSlugTaken   = errors.New("store: workspace slug already taken")
	ErrWorkspacePrefixTaken = errors.New("store: workspace item prefix already taken")
	ErrBoardNotFound        = errors.New("store: board not found")
	ErrBoardViewNotFound    = errors.New("store: board view not found")
	ErrStatusNotFound       = errors.New("store: status not found")
	ErrItemNotFound         = errors.New("store: item not found")
	ErrMCPPromptNotFound    = errors.New("store: mcp prompt not found")
	ErrMCPPromptNameTaken   = errors.New("store: mcp prompt name already taken")
	ErrCommentNotFound      = errors.New("store: comment not found")
	ErrDocumentNotFound     = errors.New("store: document not found")
	ErrProjectNotFound      = errors.New("store: project not found")
	ErrProjectSlugTaken     = errors.New("store: project slug already taken")
	ErrFactNotFound         = errors.New("store: fact not found")
	ErrFactTitleTaken       = errors.New("store: fact title already taken")
	ErrReleaseNotFound      = errors.New("store: release not found")
	ErrReleaseNameTaken     = errors.New("store: release name already taken")
	ErrMemoryNotFound       = errors.New("store: memory not found")
	ErrAgentSessionNotFound = errors.New("store: agent session not found")
)

// MCPPrompt is a user-defined Model Context Protocol prompt: a named, optionally
// parameterised template that MCP clients surface as a slash command
// (/mcp__acta__<Name>). Body is the message text, with {{arg}} placeholders
// filled from Arguments at invocation. Prompts are instance-global and ordered
// by Position (then CreatedAt).
type MCPPrompt struct {
	ID          string
	Name        string
	Title       string
	Description string
	Body        string
	Arguments   []MCPPromptArg
	Position    int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// MCPPromptArg declares one argument a prompt accepts. Name is the placeholder
// key used in the body as {{Name}}; Required marks it as mandatory to the client.
type MCPPromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Event verbs. Each names a recorded mutation; an Event's Data map carries the
// verb-specific, already-resolved strings (status names, assignee names, old
// title) so the activity log renders without joins and survives the referenced
// rows changing or being deleted.
const (
	EventItemCreated      = "item.created"        // data: status
	EventItemRenamed      = "item.renamed"        // data: from, to
	EventItemStatusChange = "item.status_changed" // data: from, to
	EventItemStatusForced = "item.status_forced"  // data: to, unmet (forced past an unmet checklist)
	EventItemAssigned     = "item.assigned"       // data: from, to ("" = unassigned)
	EventItemDescribed    = "item.described"      // (no data)
	EventItemArchived     = "item.archived"       // (no data)
	EventItemUnarchived   = "item.unarchived"     // (no data)
	EventItemMilestone    = "item.milestone"      // data: on ("true"/"false")
	EventItemReparented   = "item.reparented"     // data: to ("" = top level)
	EventItemProject      = "item.project"        // data: to (project name, "" = cleared)
	EventItemRelease      = "item.release"        // data: to (release name, "" = cleared)
	EventItemPriority     = "item.priority"       // data: to (priority label, "" = none)
	EventItemType         = "item.type"           // data: to (type label, "" = none)
	EventItemSize         = "item.size"           // data: to (size label, "" = none)
	EventItemDue          = "item.due"            // data: to (YYYY-MM-DD, "" = cleared)
	EventCommentAdded     = "comment.added"       // data: excerpt
	EventDocumentAdded    = "document.added"      // data: title
	EventDocumentUpdated  = "document.updated"    // data: title
	EventDocumentRemoved  = "document.removed"    // data: title
)

// Event is one entry in the append-only activity log. ActorID/ActorName are ""
// for a system action (no principal in context); ItemTitle and ActorName are
// snapshots taken at write time.
type Event struct {
	ID          string
	WorkspaceID string
	// BoardID is the board the event happened on — snapshotted at write time
	// (like the rest of this row) so each board has its own activity feed. An
	// item's board is derived from its status, so this is resolved when the
	// event is recorded. Empty only for events that predate boards.
	BoardID   string
	ItemID    string
	ItemTitle string
	ActorID   string
	ActorName string
	Verb      string
	Data      map[string]string
	CreatedAt time.Time
}

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
// identifier used in /{slug}/… paths. It is stable across name changes but can
// be changed deliberately from workspace settings (which moves the workspace's
// URL). Name is the human label and is unique case-insensitively. CreatedBy is
// the id of the creating user and may be empty (e.g. the seeded default, or
// after the creator is deleted).
type Workspace struct {
	ID   string
	Slug string
	Name string
	// ItemPrefix is the editable, globally-unique label for this workspace's
	// human-readable item ids (prefix-N, e.g. ACTA-12). Empty means items show
	// as bare numbers until a prefix is set.
	ItemPrefix string
	CreatedBy  string
	CreatedAt  time.Time
}

// Board is one of a workspace's boards (v1: Tasks and Backlog). It groups
// statuses (lanes); an item belongs to whichever board its status does, so a
// board is never stored on the item. Slug is the URL segment, unique within the
// workspace; Position orders the boards (and the sidebar).
type Board struct {
	ID          string
	WorkspaceID string
	Name        string
	Slug        string
	Position    int
	CreatedAt   time.Time
}

// BoardView is a saved, named filter on a board: the filter-defining URL params
// (mode, status[], assignee[], project[], release[], q) captured as a normalised
// Query string, plus a display Name and Icon key. The five header tabs ship as
// seeded defaults; users add their own. Query "" is the All-items view. Slug is
// unique within the board; Position orders the strip. CreatedBy is "" for the
// seeded defaults.
type BoardView struct {
	ID          string
	WorkspaceID string
	BoardID     string
	Slug        string
	Name        string
	Icon        string
	Query       string
	Position    int
	CreatedAt   time.Time
	CreatedBy   string
}

// Status is a board lane: a named, ordered position within one board that items
// sit in. Statuses are user-defined per board.
type Status struct {
	ID          string
	WorkspaceID string
	// BoardID is the board this lane belongs to. An item's board is read off its
	// status's BoardID — board membership lives here, never on the item.
	BoardID  string
	Name     string
	Position int
	// Color is an explicit lane colour (a hex string from the board palette),
	// or "" to fall back to a palette colour derived from Position.
	Color string
	// IsEntry marks this lane as its board's entry lane: where new (and
	// cross-board) items land. Exactly one lane per board carries it.
	IsEntry   bool
	CreatedAt time.Time
}

// Item is a card on the board: a title living in exactly one status, ordered by
// Position within that lane. WorkspaceID is denormalised from the status for
// cheap workspace-wide queries and cascade integrity. AssigneeID is the
// optional owner ("" = unassigned); ArchivedAt is nil for active items and set
// when an item is archived (soft-deleted — hidden from the board, restorable).
type Item struct {
	ID string
	// RefNum is the per-workspace sequence number behind the human-readable id
	// (prefix-RefNum, e.g. ACTA-12). Immutable once assigned at item creation.
	RefNum      int
	WorkspaceID string
	StatusID    string
	ParentID    string // "" for a top-level (board) item; otherwise its parent
	Title       string
	Description string
	AssigneeID  string
	Position    int
	IsMilestone bool
	// MSPosition orders milestone columns in Milestone mode, independent of
	// Position (the lane index, which is shared with regular cards). Only
	// meaningful for root milestones.
	MSPosition int
	ArchivedAt *time.Time
	CreatedAt  time.Time
	// CreatedBy is the principal (human or agent) that created the item, or ""
	// if unrecorded (items predating authorship, or a since-deleted creator).
	CreatedBy string
	// ProjectID is the item's project (its initiative/area), or "" for none.
	// Flat and orthogonal to ParentID — it doesn't follow the parent tree, though
	// a new subtask defaults to its parent's project at creation. Cleared to "" if
	// the project is deleted (ON DELETE SET NULL).
	ProjectID string
	// PendingStatusID is a gated status the item is trying to enter but whose
	// checklist isn't satisfied yet — "" for no pending transition. The item stays
	// in StatusID until the gate is met (auto-move), forced, or cancelled.
	PendingStatusID string
	// Priority, Type and Size are small fixed enums (0 = unset). The value→label
	// vocabularies live in the board package (board.Priority/ItemType/Size); the
	// store treats them as opaque ints. Priority: 0 none…4 urgent. Type: 0 none,
	// 1 feature, 2 bug, 3 chore. Size: 0 none, 1 XS…5 XL.
	Priority int
	Type     int
	Size     int
	// DueDate is the item's optional target date (date-only; the time is always
	// midnight UTC), or nil for none. "Overdue" is past + not in a done status.
	DueDate *time.Time
}

// SubtaskCount is a parent's direct-child progress: Total active children and
// how many of them sit in the "done" status.
type SubtaskCount struct {
	Done  int
	Total int
}

// SizeCounts is a progress tally broken down by item size (the Item.Size enum,
// 0 for unset), so the caller can apply its own per-size weighting. The store
// counts; the board package decides what a size is worth.
type SizeCounts map[int]SubtaskCount

// ProgressSnapshot is how much of one subject — a release or a project — was
// done at the end of one day. Day is that date at UTC midnight.
//
// Points are size-weighted (the board package's weighting); Items are a plain
// head count of top-level items. Synthetic marks a row reconstructed from the
// event log rather than measured on the day, which the UI shows as approximate.
type ProgressSnapshot struct {
	SubjectType string // "release" | "project"
	SubjectID   string
	Day         time.Time
	DoneItems   int
	TotalItems  int
	DonePoints  int
	TotalPoints int
	Synthetic   bool
}

// Fact is one entry in a workspace's checklist vocabulary: a named truth an item
// can carry ("Provium tests pass"). A status may require a set of facts to gate
// entry, and an item carries each fact as ticked or not (see FactTick). Facts are
// identified by Title (unique, case-insensitively, per workspace); ID is an
// internal integer handle the join rows reference so a rename never breaks them.
type Fact struct {
	ID          int64
	WorkspaceID string
	Title       string
	Position    int
	CreatedAt   time.Time
}

// FactTick is an item's assertion that a fact holds for it. The row's existence
// is the tick; CheckedBy/CheckedAt are the audit trail (who asserted it, when).
type FactTick struct {
	FactID    int64
	CheckedBy string
	CheckedAt time.Time
}

// Project is a cross-cutting initiative within a workspace: a long-lived area
// that groups items (e.g. all "Peinit" work) without needing its own workspace.
// It is orthogonal to boards and the parent/child tree — an item carries an
// optional ProjectID regardless of which board, lane, or parent it sits under.
// Slug is the URL segment, unique within the workspace. Status is the project's
// lifecycle (planned / active / paused / done). Color is "" for an auto palette
// colour (by Position) or an explicit palette hex. LeadID and CreatedBy may be
// "". ArchivedAt is nil for an active project, set when archived (a soft delete).
type Project struct {
	ID          string
	WorkspaceID string
	Slug        string
	Name        string
	Brief       string
	LeadID      string
	Status      string
	Color       string
	Position    int
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	CreatedBy   string
}

// Release is a workspace's versioned cut-line: a point the project ships at.
// Unlike a Project (an open-ended theme), a release is stateful — Planned while
// it's a future target being scoped, Active while it accrues current work, then
// Shipped, which stamps ShippedAt and freezes it into a changelog entry. Several
// releases can be Planned or Active at once. Name is the version
// handle (e.g. "v0.27.0"), unique within the workspace case-insensitively. An
// item's membership lives in the item_releases join (many-to-many), not on the
// item — see ReleasesByItem / ItemsByRelease. Color is derived from Position.
type Release struct {
	ID          string
	WorkspaceID string
	Name        string
	Description string
	Status      string     // active|shipped
	ShippedAt   *time.Time // nil until shipped — the freeze marker
	// TargetDate is the day the release is aiming at (UTC midnight), or nil if
	// it ships when it's ready. Unlike ShippedAt it's set by hand, in advance —
	// it's what the burn-up's forecast is judged against.
	TargetDate *time.Time
	Position   int
	CreatedAt  time.Time
	CreatedBy  string
}

// Comment is a note by a user on an item. AuthorID may be empty if the author's
// account was later removed. EditedAt is non-nil once the author has edited it;
// DeletedAt is non-nil once soft-deleted — the row is kept (append-only audit)
// but presented as a tombstone and dropped from the default Comments view.
type Comment struct {
	ID        string
	ItemID    string
	AuthorID  string
	Body      string
	CreatedAt time.Time
	EditedAt  *time.Time
	DeletedAt *time.Time
}

// Document is a titled, long-form markdown artifact attached to an item (many
// per item): a compliance report, findings doc, runbook. Distinct from the
// item's single Description body and from conversational Comments — documents
// are named, edited in place, and not author-locked. AuthorID records the
// creator (decorative; cleared if the user is removed).
type Document struct {
	ID        string
	ItemID    string
	AuthorID  string
	Title     string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Memory scopes — the kind of owner a Memory belongs to, persisted in the scope
// column. The accompanying ScopeID names the specific owner: a User id for
// agent/user scope, a workspace/project/item id for those, "" for the single
// site scope. Like the subscription Subject* constants, these live here because
// they're persisted enum values shared across the store, service, and web layers.
const (
	ScopeAgent     = "agent"
	ScopeUser      = "user"
	ScopeSite      = "site"
	ScopeWorkspace = "workspace"
	ScopeProject   = "project"
	ScopeTask      = "task"
)

// Memory is an arbitrary markdown note accumulated under a scope: an agent's
// own notes, a user's, the site's, a workspace's, a project's, or a task's.
// Scope names the kind of owner (see the Scope* constants) and ScopeID is that
// owner's id — a User id for agent/user scope, "" for the single site scope.
// Stored inline (not as a file). Name is a short label/filename; Body is
// markdown, rendered through the same pipeline as descriptions and documents.
type Memory struct {
	ID      string
	Scope   string
	ScopeID string
	Name    string
	Summary string
	Body    string
	// CreatedBy / UpdatedBy are the principal ids that wrote and last touched the
	// memory (decorative provenance; "" if unrecorded or the user was removed).
	CreatedBy string
	UpdatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Notification kinds. Each names why a principal was notified; the row's
// snapshot fields carry the already-resolved context to render it.
const (
	NotificationMention  = "mention"  // someone @mentioned the recipient in a comment
	NotificationActivity = "activity" // a subscription the recipient holds matched an event
	NotificationSession  = "session"  // an agent session the recipient owns needs them, or stopped
)

// Subscription subject types — the kind of thing a principal can subscribe to.
// A subscription matches an event when its subject is the event's item
// (SubjectItem), the event item's project (SubjectProject), or the event's
// actor (SubjectPrincipal). Persisted as the subject_type column.
const (
	SubjectItem      = "item"
	SubjectProject   = "project"
	SubjectPrincipal = "principal"
)

// Notification is one entry in a principal's inbox: a per-recipient delivery
// record with read state, distinct from an Event (the global activity log).
// ActorName, ItemTitle, WorkspaceSlug and Excerpt are snapshots taken at write
// time so the bell renders without joins and survives the referenced rows being
// renamed or deleted — the same history-not-state stance as the activity log.
// ReadAt is nil while unread.
type Notification struct {
	ID            string
	RecipientID   string
	Kind          string
	WorkspaceID   string
	WorkspaceSlug string
	ItemID        string
	ItemTitle     string
	ActorID       string
	ActorName     string
	CommentID     string
	Excerpt       string
	// Verb and Summary carry an activity notification's payload: the event verb
	// (drives the glyph/semantics) and the already-rendered phrase ("moved from
	// To do to Doing"), snapshotted so the bell renders without re-resolving the
	// event. Both are "" for a mention.
	Verb      string
	Summary   string
	CreatedAt time.Time
	ReadAt    *time.Time
}

// Subscription is one principal's standing interest in a subject: when a
// matching event fires, the subscriber gets an activity notification. Events
// holds the category keys (see internal/board) the subscriber wants delivered —
// the configurable filter, seeded from a per-subject-type default. The triple
// (SubscriberID, SubjectType, SubjectID) is unique.
type Subscription struct {
	ID           string
	SubscriberID string
	SubjectType  string
	SubjectID    string
	Events       []string
	CreatedAt    time.Time
}

// PushSubscription is one browser's Web Push registration for a user: the push
// service Endpoint plus the keys (P256dh, Auth) used to encrypt payloads to it.
// Endpoint is the natural key — it identifies the device/browser uniquely — so
// re-subscribing the same browser upserts rather than duplicates. A user may
// hold several (laptop, phone, …).
type PushSubscription struct {
	Endpoint  string
	UserID    string
	P256dh    string
	Auth      string
	CreatedAt time.Time
}

// AgentSession is a browser-driven agent session — a Claude Code (or, later,
// another backend's) process that a harness on the owner's machine runs and
// that Acta relays a chat to. ID is a UUID minted by Acta at creation and
// passed to the backend as its own session id, so the two are one string and
// resume needs no mapping. Backend names the adapter ("claude-code"); Cwd is
// the directory the process runs in; Options is the backend-specific spawn
// configuration (permission mode, say) as a JSON object, stored verbatim.
type AgentSession struct {
	ID        string
	OwnerID   string
	Backend   string
	Cwd       string
	Title     string
	Options   map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AgentSessionEvent is one frame of a session's transcript: a wire message
// between the harness and Acta, stored verbatim so the browser can render
// exactly what was said and nothing is lost to a schema the renderer hasn't
// caught up with. Seq is the append order (monotonic per store, not per
// session). Kind is a coarse label for filtering ("event" for a backend event,
// "input" for a user message, "state" for a harness lifecycle notice).
type AgentSessionEvent struct {
	Seq       int64
	SessionID string
	Kind      string
	Payload   []byte // raw JSON
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

	// CreateWorkspace persists w (caller supplies a unique slug, name, and item
	// prefix); it returns ErrWorkspaceNameTaken / ErrWorkspaceSlugTaken /
	// ErrWorkspacePrefixTaken on collision. RenameWorkspace changes only the
	// name. UpdateWorkspace sets the name, slug, and item prefix (the settings
	// editor); it returns the same collision sentinels. WorkspaceByPrefix
	// resolves a workspace by its (case-insensitive) item prefix, for turning a
	// human id like ACTA-12 back into a workspace.
	CreateWorkspace(ctx context.Context, w Workspace) (Workspace, error)
	ListWorkspaces(ctx context.Context) ([]Workspace, error)
	WorkspaceByID(ctx context.Context, id string) (Workspace, error)
	WorkspaceBySlug(ctx context.Context, slug string) (Workspace, error)
	WorkspaceByPrefix(ctx context.Context, prefix string) (Workspace, error)
	RenameWorkspace(ctx context.Context, id, name string) error
	UpdateWorkspace(ctx context.Context, id, name, slug, prefix string) error
	DeleteWorkspace(ctx context.Context, id string) error
	CountWorkspaces(ctx context.Context) (int, error)

	// Boards. A workspace has one or more boards (v1: Tasks, then Backlog).
	// BoardsByWorkspace returns them ordered by position; BoardBySlug resolves
	// one within a workspace and returns ErrBoardNotFound on no match.
	CreateBoard(ctx context.Context, b Board) (Board, error)
	BoardsByWorkspace(ctx context.Context, workspaceID string) ([]Board, error)
	BoardByID(ctx context.Context, id string) (Board, error)
	BoardBySlug(ctx context.Context, workspaceID, slug string) (Board, error)

	// Statuses (board lanes). StatusesByWorkspace returns every lane in the
	// workspace (across all its boards); StatusesByBoard scopes to one board.
	// Both are ordered by position. ReorderStatuses sets each id's position to
	// its index in the given slice, atomically.
	CreateStatus(ctx context.Context, s Status) (Status, error)
	StatusesByWorkspace(ctx context.Context, workspaceID string) ([]Status, error)
	StatusesByBoard(ctx context.Context, boardID string) ([]Status, error)
	StatusByID(ctx context.Context, id string) (Status, error)
	RenameStatus(ctx context.Context, id, name string) error
	SetStatusColor(ctx context.Context, id, color string) error
	ReorderStatuses(ctx context.Context, workspaceID string, orderedIDs []string) error
	DeleteStatus(ctx context.Context, id string) error

	// Board views (saved filters). BoardViewsByBoard returns one board's views in
	// position order. ReorderBoardViews sets each id's position to its index in
	// the slice, atomically. RenameBoardView/DeleteBoardView return
	// ErrBoardViewNotFound when the id is unknown.
	CreateBoardView(ctx context.Context, v BoardView) (BoardView, error)
	BoardViewsByBoard(ctx context.Context, boardID string) ([]BoardView, error)
	BoardViewByID(ctx context.Context, id string) (BoardView, error)
	RenameBoardView(ctx context.Context, id, name string) error
	UpdateBoardViewQuery(ctx context.Context, id, query string) error
	ReorderBoardViews(ctx context.Context, boardID string, orderedIDs []string) error
	DeleteBoardView(ctx context.Context, id string) error

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
	// SearchItems returns items whose title or description contains query as a
	// case-insensitive substring (LIKE wildcards in query are matched literally),
	// at every nesting depth. boardID scopes to one board (membership read off an
	// item's status); "" searches every board. Results are ordered by relevance —
	// title matches before body-only matches, then newest first. includeArchived
	// false (the common case) omits archived items. The pg_trgm GIN indexes on
	// title/description back the scan.
	SearchItems(ctx context.Context, workspaceID, boardID, query string, includeArchived bool) ([]Item, error)
	ItemByID(ctx context.Context, id string) (Item, error)
	// ItemByRef resolves an item by its per-workspace ref number (the N in a
	// human id like ACTA-12); returns ErrItemNotFound if none matches.
	ItemByRef(ctx context.Context, workspaceID string, refNum int) (Item, error)
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
	// SetItemPriority/Type/Size set an item's enum attribute (0 clears it). The
	// store stores the int verbatim; the board package owns the value vocabulary
	// and validates the range before calling. SetItemDue sets (or clears, with
	// nil) the item's target date. All four return ErrItemNotFound on no row.
	SetItemPriority(ctx context.Context, id string, priority int) error
	SetItemType(ctx context.Context, id string, itemType int) error
	SetItemSize(ctx context.Context, id string, size int) error
	SetItemDue(ctx context.Context, id string, due *time.Time) error
	SetItemStatus(ctx context.Context, id, statusID string) error
	SetItemParent(ctx context.Context, id, parentID string) error
	SetItemMilestone(ctx context.Context, id string, isMilestone bool) error
	// ReorderMilestones sets each id's ms_position to its index in the slice,
	// scoped to root milestones in the workspace (ids that aren't milestones in
	// this workspace are ignored, so it can't disturb status-mode ordering).
	ReorderMilestones(ctx context.Context, workspaceID string, orderedIDs []string) error
	ArchiveItem(ctx context.Context, id string) error
	UnarchiveItem(ctx context.Context, id string) error
	ReorderItems(ctx context.Context, statusID string, orderedIDs []string) error
	SetItemPositions(ctx context.Context, orderedIDs []string) error
	DeleteItem(ctx context.Context, id string) error

	// Projects. A project is a workspace-scoped initiative that items may belong
	// to (Item.ProjectID). CreateProject returns ErrProjectSlugTaken on a
	// per-workspace slug collision. ProjectsByWorkspace lists them ordered by
	// position; includeArchived false omits archived projects. UpdateProject sets
	// p's mutable fields (slug, name, brief, lead, status, color) by p.ID and also
	// returns ErrProjectSlugTaken. SetProjectArchived toggles the soft-delete.
	// SetItemProject sets (or clears, with "") an item's project. ItemsByProject
	// returns a project's active top-level items, newest first. ProjectItemCounts
	// returns per-project top-level progress (Total, and Done = items whose status
	// is in doneStatusIDs) for the overview bars and a single project's rollup.
	CreateProject(ctx context.Context, p Project) (Project, error)
	ProjectsByWorkspace(ctx context.Context, workspaceID string, includeArchived bool) ([]Project, error)
	ProjectByID(ctx context.Context, id string) (Project, error)
	ProjectBySlug(ctx context.Context, workspaceID, slug string) (Project, error)
	UpdateProject(ctx context.Context, p Project) error
	SetProjectArchived(ctx context.Context, id string, archived bool) error
	SetItemProject(ctx context.Context, id, projectID string) error
	ItemsByProject(ctx context.Context, projectID string) ([]Item, error)
	ProjectSizeCounts(ctx context.Context, workspaceID string, doneStatusIDs []string) (map[string]SizeCounts, error)

	// Releases. A release is a workspace-scoped versioned cut-line items belong to
	// via the item_releases join (many-to-many). CreateRelease returns
	// ErrReleaseNameTaken on a per-workspace, case-insensitive name collision.
	// ReleasesByWorkspace lists every release (planned, active and shipped) ordered
	// by position then created_at. UpdateRelease sets a release's name,
	// description and target date by r.ID (also returns ErrReleaseNameTaken). SetReleaseStatus moves
	// a release to planned|active|shipped, stamping shipped_at on "shipped" and
	// clearing it otherwise. DeleteRelease removes a release
	// and its memberships (the join cascades). SetItemRelease replaces an item's
	// memberships with the single given release (or clears them with ""), the UI's
	// one-release-per-item write path. ReleasesByItem returns the releases an item
	// belongs to (position order); ItemsByRelease a release's active top-level
	// items, newest first. ReleaseLinksByWorkspace maps every linked item id in the
	// workspace to its release ids (for the board's chips and filter).
	// ReleaseSizeCounts returns per-release top-level progress broken down by item
	// size (Total, and Done = items whose status is in doneStatusIDs), which the
	// board package weights into the overview bars.
	CreateRelease(ctx context.Context, r Release) (Release, error)
	ReleasesByWorkspace(ctx context.Context, workspaceID string) ([]Release, error)
	ReleaseByID(ctx context.Context, id string) (Release, error)
	UpdateRelease(ctx context.Context, r Release) error
	SetReleaseStatus(ctx context.Context, id, status string) error
	DeleteRelease(ctx context.Context, id string) error
	SetItemRelease(ctx context.Context, itemID, releaseID string) error
	ReleasesByItem(ctx context.Context, itemID string) ([]Release, error)
	ItemsByRelease(ctx context.Context, releaseID string) ([]Item, error)
	ReleaseLinksByWorkspace(ctx context.Context, workspaceID string) (map[string][]string, error)
	ReleaseSizeCounts(ctx context.Context, workspaceID string, doneStatusIDs []string) (map[string]SizeCounts, error)

	// Progress snapshots: one row per subject ("release"/"project") per day, the
	// history behind burn-up charts and velocity. UpsertProgressSnapshots writes a
	// batch, replacing same-day rows — except that a measured row is never
	// overwritten by a synthetic (backfilled) one, so re-running a backfill can't
	// clobber real history. ProgressSnapshotsBySubjects returns each requested
	// subject's rows from since onward, oldest first, keyed by subject id (absent
	// for a subject with no rows). DeleteProgressSnapshots is the manual cascade
	// for a deleted subject — subject_id is polymorphic and carries no FK.
	UpsertProgressSnapshots(ctx context.Context, snaps []ProgressSnapshot) error
	ProgressSnapshotsBySubjects(ctx context.Context, subjectType string, subjectIDs []string, since time.Time) (map[string][]ProgressSnapshot, error)
	DeleteProgressSnapshots(ctx context.Context, subjectType, subjectID string) error

	// Comments on an item, returned oldest-first (including soft-deleted ones;
	// callers filter as they need). CommentByID returns ErrCommentNotFound when
	// absent. UpdateComment replaces the body and stamps editedAt; SoftDelete
	// stamps deletedAt. Both return the updated row.
	CreateComment(ctx context.Context, c Comment) (Comment, error)
	CommentsByItem(ctx context.Context, itemID string) ([]Comment, error)
	CommentByID(ctx context.Context, id string) (Comment, error)
	UpdateComment(ctx context.Context, id, body string, editedAt time.Time) (Comment, error)
	SoftDeleteComment(ctx context.Context, id string, deletedAt time.Time) (Comment, error)

	// Documents on an item — titled markdown artifacts, many per item, returned
	// oldest-first. DocumentByID returns ErrDocumentNotFound when absent.
	// UpdateDocument replaces title+body and stamps updatedAt; DeleteDocument is
	// a hard delete (ErrDocumentNotFound if the row is already gone).
	CreateDocument(ctx context.Context, d Document) (Document, error)
	DocumentsByItem(ctx context.Context, itemID string) ([]Document, error)
	DocumentByID(ctx context.Context, id string) (Document, error)
	UpdateDocument(ctx context.Context, id, title, body string, updatedAt time.Time) (Document, error)
	DeleteDocument(ctx context.Context, id string) error

	// Memories scoped to one owner — arbitrary markdown notes, returned
	// oldest-first. MemoriesByScope filters on (scope, scopeID). MemoryByID and
	// MemoryByScopeName return ErrMemoryNotFound when absent; the latter is the
	// name-addressed lookup behind upsert/edit. UpdateMemory replaces
	// name+summary+body and stamps updatedBy/updatedAt; DeleteMemory is a hard
	// delete (ErrMemoryNotFound if the row is already gone).
	CreateMemory(ctx context.Context, m Memory) (Memory, error)
	MemoriesByScope(ctx context.Context, scope, scopeID string) ([]Memory, error)
	MemoryByID(ctx context.Context, id string) (Memory, error)
	MemoryByScopeName(ctx context.Context, scope, scopeID, name string) (Memory, error)
	UpdateMemory(ctx context.Context, id, name, summary, body, updatedBy string, updatedAt time.Time) (Memory, error)
	DeleteMemory(ctx context.Context, id string) error
	// DeleteMemoriesByScope removes every memory under one owner — the
	// polymorphic cascade (scope_id carries no FK), used when an owner such as an
	// agent is deleted. Deleting zero rows is not an error.
	DeleteMemoriesByScope(ctx context.Context, scope, scopeID string) error

	// Agent sessions and their transcripts. CreateAgentSession requires the
	// caller to have set ID (a UUID). AgentSessionsByOwner returns most
	// recently updated first. AppendAgentSessionEvent stores one frame and
	// bumps the session's updated_at; AgentSessionEvents returns a session's
	// frames with seq > afterSeq in seq order (limit <= 0 means no cap).
	// DeleteAgentSession removes the session and its transcript.
	CreateAgentSession(ctx context.Context, s AgentSession) (AgentSession, error)
	AgentSessionByID(ctx context.Context, id string) (AgentSession, error)
	AgentSessionsByOwner(ctx context.Context, ownerID string) ([]AgentSession, error)
	UpdateAgentSessionTitle(ctx context.Context, id, title string, updatedAt time.Time) (AgentSession, error)
	// UpdateAgentSessionOptions replaces a session's options (the settings a
	// resume starts with: permission mode, model, effort, the backend's own
	// conversation id).
	UpdateAgentSessionOptions(ctx context.Context, id string, options map[string]any, updatedAt time.Time) (AgentSession, error)
	DeleteAgentSession(ctx context.Context, id string) error
	AppendAgentSessionEvent(ctx context.Context, e AgentSessionEvent) (AgentSessionEvent, error)
	AgentSessionEvents(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]AgentSessionEvent, error)

	// Activity log. RecordEvent appends an entry (assigning its id). The two
	// readers return newest-first, capped at limit (a non-positive limit is
	// clamped to a sane default).
	RecordEvent(ctx context.Context, e Event) (Event, error)
	EventsByItem(ctx context.Context, itemID string, limit int) ([]Event, error)
	EventsByWorkspace(ctx context.Context, workspaceID string, limit int) ([]Event, error)
	EventsByBoard(ctx context.Context, boardID string, limit int) ([]Event, error)
	// LatestEventForActor returns the most recent event of verb by actorID on
	// itemID recorded at or after since, and whether one exists. It backs the
	// activity log's coalescing: a burst of autosave-driven edits folds into a
	// single entry within a window rather than logging once per save.
	LatestEventForActor(ctx context.Context, itemID, actorID, verb string, since time.Time) (Event, bool, error)
	// TouchEvent advances an existing event's timestamp to at and replaces its
	// data, folding a later edit of a burst into the entry that opened it.
	TouchEvent(ctx context.Context, id string, at time.Time, data map[string]string) error

	// Notifications: a per-recipient inbox with read state. CreateNotification
	// appends an entry (assigning its id). NotificationsByRecipient returns a
	// recipient's rows newest-first, capped at limit (non-positive clamps to a
	// default); UnreadNotificationsByRecipient is the same but filtered to unread
	// rows, so an agent can drain its inbox without read rows crowding the window.
	// UnreadNotificationCount counts the recipient's unread rows.
	// MarkNotificationRead marks one read, scoped to the recipient so a caller
	// can't touch another principal's inbox; it is idempotent (a missing or
	// already-read row is not an error). MarkAllNotificationsRead clears the
	// recipient's whole unread set.
	CreateNotification(ctx context.Context, n Notification) (Notification, error)
	NotificationsByRecipient(ctx context.Context, recipientID string, limit int) ([]Notification, error)
	UnreadNotificationsByRecipient(ctx context.Context, recipientID string, limit int) ([]Notification, error)
	UnreadNotificationCount(ctx context.Context, recipientID string) (int, error)
	MarkNotificationRead(ctx context.Context, id, recipientID string) error
	MarkAllNotificationsRead(ctx context.Context, recipientID string) error
	// MarkNotificationsReadByItem clears the recipient's unread rows about one
	// item (an agent session's id, for a session notification): opening the
	// thing itself reads everything said about it.
	MarkNotificationsReadByItem(ctx context.Context, recipientID, itemID string) error

	// Subscriptions: a principal's standing interests, keyed by the unique triple
	// (subscriber, subject_type, subject_id). EnsureSubscription inserts with the
	// given Events if the triple is absent and otherwise leaves the existing row
	// untouched (the idempotent, sticky auto-subscribe — it never clobbers a
	// configured filter). SetSubscriptionEvents upserts with an explicit Events
	// filter (the manual subscribe/configure). DeleteSubscription removes one (a
	// missing row is not an error). SubscriptionFor returns one and whether it
	// exists. SubscriptionsBySubscriber lists a principal's subscriptions,
	// optionally filtered to a subject_type ("" = all), newest-first.
	// SubscribersForEvent returns every subscription matching an event by its
	// item, project, or actor — the fanout query (empty ids match nothing).
	EnsureSubscription(ctx context.Context, s Subscription) (Subscription, error)
	SetSubscriptionEvents(ctx context.Context, subscriberID, subjectType, subjectID string, events []string) (Subscription, error)
	DeleteSubscription(ctx context.Context, subscriberID, subjectType, subjectID string) error
	SubscriptionFor(ctx context.Context, subscriberID, subjectType, subjectID string) (Subscription, bool, error)
	SubscriptionsBySubscriber(ctx context.Context, subscriberID, subjectType string) ([]Subscription, error)
	SubscribersForEvent(ctx context.Context, itemID, projectID, actorID string) ([]Subscription, error)

	// Status checklists. A workspace owns a vocabulary of facts (CreateFact, with
	// ErrFactTitleTaken on a case-insensitive per-workspace title collision;
	// FactsByWorkspace ordered by position; RenameFact/DeleteFact by id). A status
	// declares which facts gate it: FactsByStatus returns its gating facts ordered,
	// SetStatusFacts replaces the whole set atomically (the Manage Checklist
	// editor). An item carries ticks: TicksByItem returns its asserted facts;
	// SetItemFact ticks (insert, refreshing checked_by/at) or unticks (delete) one.
	// SetItemPending records (or clears, with "") the gated status an item is
	// waiting to enter.
	CreateFact(ctx context.Context, workspaceID, title string) (Fact, error)
	FactsByWorkspace(ctx context.Context, workspaceID string) ([]Fact, error)
	FactByID(ctx context.Context, id int64) (Fact, error)
	RenameFact(ctx context.Context, id int64, title string) error
	DeleteFact(ctx context.Context, id int64) error
	FactsByStatus(ctx context.Context, statusID string) ([]Fact, error)
	SetStatusFacts(ctx context.Context, statusID string, factIDs []int64) error
	TicksByItem(ctx context.Context, itemID string) ([]FactTick, error)
	SetItemFact(ctx context.Context, itemID string, factID int64, checked bool, by string) error
	SetItemPending(ctx context.Context, itemID, statusID string) error

	// Web Push subscriptions, keyed by endpoint. CreatePushSubscription upserts
	// (re-subscribing a browser refreshes its keys and owner rather than
	// duplicating). PushSubscriptionsByUser lists a user's registrations.
	// DeletePushSubscription removes one by endpoint — used both when a user
	// turns notifications off and when the push service reports the endpoint
	// gone (410); a missing row is not an error.
	CreatePushSubscription(ctx context.Context, sub PushSubscription) error
	PushSubscriptionsByUser(ctx context.Context, userID string) ([]PushSubscription, error)
	DeletePushSubscription(ctx context.Context, endpoint string) error

	// AppSetting reads an instance-global key/value setting, returning "" (no
	// error) when the key is absent. SetAppSetting upserts it. The MCP guide
	// lives here under "mcp.guide".
	AppSetting(ctx context.Context, key string) (string, error)
	SetAppSetting(ctx context.Context, key, value string) error

	// User-defined MCP prompts, ordered by position then creation. Create assigns
	// the id; ErrMCPPromptNameTaken signals a name collision. Update replaces
	// every mutable field of the row identified by p.ID.
	CreateMCPPrompt(ctx context.Context, p MCPPrompt) (MCPPrompt, error)
	ListMCPPrompts(ctx context.Context) ([]MCPPrompt, error)
	MCPPromptByID(ctx context.Context, id string) (MCPPrompt, error)
	UpdateMCPPrompt(ctx context.Context, p MCPPrompt) error
	DeleteMCPPrompt(ctx context.Context, id string) error

	Close()
}
