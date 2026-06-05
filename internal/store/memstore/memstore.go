// Package memstore is an in-memory store.Store implementation. It backs unit
// and HTTP-level tests without a database, and doubles as a reference for what
// the interface requires.
package memstore

import (
	"bytes"
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/peios/acta/internal/id"
	"github.com/peios/acta/internal/store"
)

type Store struct {
	mu          sync.Mutex
	users       map[string]store.User
	sessions    map[string]store.Session
	credentials map[string]store.Credential
	challenges  map[string]store.Challenge
	workspaces  map[string]store.Workspace
	itemSeq     map[string]int // per-workspace monotonic counter for item ref numbers
	boards      map[string]store.Board
	statuses    map[string]store.Status
	items       map[string]store.Item
	comments    map[string]store.Comment
	apiTokens   map[string]store.APIToken
	settings    map[string]string
	mcpPrompts  map[string]store.MCPPrompt
	events      map[string]store.Event
	notifs      map[string]store.Notification
	pushSubs    map[string]store.PushSubscription // keyed by endpoint
	projects    map[string]store.Project
	subs        map[string]store.Subscription // keyed by id
}

func New() *Store {
	return &Store{
		users:       map[string]store.User{},
		sessions:    map[string]store.Session{},
		credentials: map[string]store.Credential{},
		challenges:  map[string]store.Challenge{},
		workspaces:  map[string]store.Workspace{},
		itemSeq:     map[string]int{},
		boards:      map[string]store.Board{},
		statuses:    map[string]store.Status{},
		items:       map[string]store.Item{},
		comments:    map[string]store.Comment{},
		apiTokens:   map[string]store.APIToken{},
		settings:    map[string]string{},
		mcpPrompts:  map[string]store.MCPPrompt{},
		events:      map[string]store.Event{},
		notifs:      map[string]store.Notification{},
		pushSubs:    map[string]store.PushSubscription{},
		projects:    map[string]store.Project{},
		subs:        map[string]store.Subscription{},
	}
}

func (s *Store) CreateUser(_ context.Context, u store.NewUser) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ex := range s.users {
		if ex.Username == u.Username {
			return store.User{}, store.ErrUsernameTaken
		}
	}
	nu := store.User{
		ID:           newID(),
		Username:     u.Username,
		Display:      u.Display,
		PasswordHash: u.PasswordHash,
		AgentOfID:    u.AgentOfID,
		CreatedAt:    time.Now(),
	}
	s.users[nu.ID] = nu
	return nu, nil
}

func (s *Store) UserByUsername(_ context.Context, username string) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Username == username {
			return u, nil
		}
	}
	return store.User{}, store.ErrUserNotFound
}

func (s *Store) UserByID(_ context.Context, id string) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return store.User{}, store.ErrUserNotFound
	}
	return u, nil
}

func (s *Store) SetUserPassword(_ context.Context, id, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return store.ErrUserNotFound
	}
	u.PasswordHash = passwordHash
	s.users[id] = u
	return nil
}

func (s *Store) SetUserDisabled(_ context.Context, id string, disabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return store.ErrUserNotFound
	}
	if disabled {
		now := time.Now().UTC()
		u.DisabledAt = &now
	} else {
		u.DisabledAt = nil
	}
	s.users[id] = u
	return nil
}

func (s *Store) ListUsers(_ context.Context) ([]store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		if strings.EqualFold(out[i].Display, out[j].Display) {
			return out[i].Username < out[j].Username
		}
		return strings.ToLower(out[i].Display) < strings.ToLower(out[j].Display)
	})
	return out, nil
}

func (s *Store) AgentsByOwner(_ context.Context, ownerID string) ([]store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.User
	for _, u := range s.users {
		if u.AgentOfID == ownerID {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

// DeleteUser mirrors the Postgres FK cascade: owned agents, credentials,
// tokens, and sessions go with the user; items they created keep their history
// with a null creator.
func (s *Store) DeleteUser(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, id)
	for uid, u := range s.users {
		if u.AgentOfID == id { // owned agents cascade
			delete(s.users, uid)
		}
	}
	for sid, sess := range s.sessions {
		if sess.UserID == id {
			delete(s.sessions, sid)
		}
	}
	for cid, c := range s.credentials {
		if c.UserID == id {
			delete(s.credentials, cid)
		}
	}
	for tid, t := range s.apiTokens {
		if t.UserID == id {
			delete(s.apiTokens, tid)
		}
	}
	for iid, it := range s.items {
		if it.CreatedBy == id {
			it.CreatedBy = ""
			s.items[iid] = it
		}
	}
	return nil
}

func (s *Store) CreateSession(_ context.Context, sess store.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess.PublicID == "" {
		sess.PublicID = newID()
	}
	s.sessions[sess.ID] = sess
	return nil
}

func (s *Store) SessionsByUserID(_ context.Context, userID string, now time.Time) ([]store.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Session
	for _, sess := range s.sessions {
		if sess.UserID == userID && sess.ExpiresAt.After(now) {
			out = append(out, sess)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out, nil
}

func (s *Store) DeleteUserSession(_ context.Context, publicID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.sessions {
		if sess.PublicID == publicID && sess.UserID == userID {
			delete(s.sessions, id)
		}
	}
	return nil
}

func (s *Store) DeleteOtherSessions(_ context.Context, userID, keepID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for id, sess := range s.sessions {
		if sess.UserID == userID && id != keepID {
			delete(s.sessions, id)
			n++
		}
	}
	return n, nil
}

func (s *Store) SessionByID(_ context.Context, id string) (store.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return store.Session{}, store.ErrSessionNotFound
	}
	return sess, nil
}

func (s *Store) TouchSession(_ context.Context, id string, lastSeen time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return store.ErrSessionNotFound
	}
	sess.LastSeen = lastSeen
	s.sessions[id] = sess
	return nil
}

func (s *Store) DeleteSession(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

func (s *Store) DeleteExpiredSessions(_ context.Context, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for id, sess := range s.sessions {
		if sess.ExpiresAt.Before(now) {
			delete(s.sessions, id)
			n++
		}
	}
	return n, nil
}

// --- api tokens ---

func (s *Store) CreateAPIToken(_ context.Context, t store.APIToken) (store.APIToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ex := range s.apiTokens {
		if bytes.Equal(ex.Hash, t.Hash) {
			return store.APIToken{}, store.ErrAPITokenNotFound // hash collision; vanishingly unlikely
		}
	}
	t.ID = newID()
	t.CreatedAt = time.Now()
	s.apiTokens[t.ID] = t
	return t, nil
}

func (s *Store) APITokensByUserID(_ context.Context, userID string) ([]store.APIToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.APIToken
	for _, t := range s.apiTokens {
		if t.UserID == userID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) APITokenByHash(_ context.Context, hash []byte) (store.APIToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.apiTokens {
		if bytes.Equal(t.Hash, hash) {
			return t, nil
		}
	}
	return store.APIToken{}, store.ErrAPITokenNotFound
}

func (s *Store) TouchAPIToken(_ context.Context, id string, lastUsed time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.apiTokens[id]
	if !ok {
		return store.ErrAPITokenNotFound
	}
	t.LastUsedAt = &lastUsed
	s.apiTokens[id] = t
	return nil
}

func (s *Store) DeleteAPIToken(_ context.Context, id, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.apiTokens[id]
	if !ok || t.UserID != userID {
		return store.ErrAPITokenNotFound
	}
	delete(s.apiTokens, id)
	return nil
}

// --- credentials ---

func (s *Store) CreateCredential(_ context.Context, c store.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == "" {
		c.ID = newID()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	s.credentials[c.ID] = c
	return nil
}

func (s *Store) CredentialsByUserID(_ context.Context, userID string) ([]store.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Credential
	for _, c := range s.credentials {
		if c.UserID == userID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *Store) CredentialByCredentialID(_ context.Context, credentialID []byte) (store.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.credentials {
		if bytes.Equal(c.CredentialID, credentialID) {
			return c, nil
		}
	}
	return store.Credential{}, store.ErrCredentialNotFound
}

func (s *Store) TouchCredential(_ context.Context, credentialID []byte, signCount uint32, lastUsed time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, c := range s.credentials {
		if bytes.Equal(c.CredentialID, credentialID) {
			c.SignCount = signCount
			c.LastUsedAt = &lastUsed
			s.credentials[id] = c
			return nil
		}
	}
	return store.ErrCredentialNotFound
}

func (s *Store) DeleteCredential(_ context.Context, id, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.credentials[id]; !ok || c.UserID != userID {
		return store.ErrCredentialNotFound
	}
	delete(s.credentials, id)
	return nil
}

// --- webauthn challenges ---

func (s *Store) CreateChallenge(_ context.Context, c store.Challenge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.challenges[c.ID] = c
	return nil
}

func (s *Store) ConsumeChallenge(_ context.Context, id string) (store.Challenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.challenges[id]
	if !ok {
		return store.Challenge{}, store.ErrChallengeNotFound
	}
	delete(s.challenges, id)
	return c, nil
}

// --- workspaces ---

func (s *Store) CreateWorkspace(_ context.Context, w store.Workspace) (store.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ex := range s.workspaces {
		if strings.EqualFold(ex.Name, w.Name) {
			return store.Workspace{}, store.ErrWorkspaceNameTaken
		}
		if ex.Slug == w.Slug {
			return store.Workspace{}, store.ErrWorkspaceSlugTaken
		}
		if w.ItemPrefix != "" && strings.EqualFold(ex.ItemPrefix, w.ItemPrefix) {
			return store.Workspace{}, store.ErrWorkspacePrefixTaken
		}
	}
	if w.ID == "" {
		w.ID = newID()
	}
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now()
	}
	s.workspaces[w.ID] = w
	return w, nil
}

func (s *Store) ListWorkspaces(_ context.Context) ([]store.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.Workspace, 0, len(s.workspaces))
	for _, w := range s.workspaces {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) WorkspaceByID(_ context.Context, id string) (store.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workspaces[id]
	if !ok {
		return store.Workspace{}, store.ErrWorkspaceNotFound
	}
	return w, nil
}

func (s *Store) WorkspaceBySlug(_ context.Context, slug string) (store.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.workspaces {
		if w.Slug == slug {
			return w, nil
		}
	}
	return store.Workspace{}, store.ErrWorkspaceNotFound
}

func (s *Store) RenameWorkspace(_ context.Context, id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workspaces[id]
	if !ok {
		return store.ErrWorkspaceNotFound
	}
	for oid, ex := range s.workspaces {
		if oid != id && strings.EqualFold(ex.Name, name) {
			return store.ErrWorkspaceNameTaken
		}
	}
	w.Name = name
	s.workspaces[id] = w
	return nil
}

func (s *Store) UpdateWorkspace(_ context.Context, id, name, slug, prefix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workspaces[id]
	if !ok {
		return store.ErrWorkspaceNotFound
	}
	for oid, ex := range s.workspaces {
		if oid == id {
			continue
		}
		if strings.EqualFold(ex.Name, name) {
			return store.ErrWorkspaceNameTaken
		}
		if ex.Slug == slug {
			return store.ErrWorkspaceSlugTaken
		}
		if prefix != "" && strings.EqualFold(ex.ItemPrefix, prefix) {
			return store.ErrWorkspacePrefixTaken
		}
	}
	w.Name = name
	w.Slug = slug
	w.ItemPrefix = prefix
	s.workspaces[id] = w
	return nil
}

func (s *Store) WorkspaceByPrefix(_ context.Context, prefix string) (store.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.workspaces {
		if w.ItemPrefix != "" && strings.EqualFold(w.ItemPrefix, prefix) {
			return w, nil
		}
	}
	return store.Workspace{}, store.ErrWorkspaceNotFound
}

func (s *Store) DeleteWorkspace(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.workspaces[id]; !ok {
		return store.ErrWorkspaceNotFound
	}
	delete(s.workspaces, id)
	return nil
}

func (s *Store) CountWorkspaces(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.workspaces), nil
}

// --- boards ---

func (s *Store) CreateBoard(_ context.Context, b store.Board) (store.Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b.ID == "" {
		b.ID = newID()
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now()
	}
	for _, ex := range s.boards {
		if ex.WorkspaceID == b.WorkspaceID && ex.Slug == b.Slug {
			return store.Board{}, store.ErrWorkspaceSlugTaken
		}
	}
	s.boards[b.ID] = b
	return b, nil
}

func (s *Store) BoardsByWorkspace(_ context.Context, workspaceID string) ([]store.Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Board
	for _, b := range s.boards {
		if b.WorkspaceID == workspaceID {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}

func (s *Store) BoardByID(_ context.Context, id string) (store.Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.boards[id]
	if !ok {
		return store.Board{}, store.ErrBoardNotFound
	}
	return b, nil
}

func (s *Store) BoardBySlug(_ context.Context, workspaceID, slug string) (store.Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.boards {
		if b.WorkspaceID == workspaceID && b.Slug == slug {
			return b, nil
		}
	}
	return store.Board{}, store.ErrBoardNotFound
}

// --- board: statuses ---

func (s *Store) CreateStatus(_ context.Context, st store.Status) (store.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.ID == "" {
		st.ID = newID()
	}
	if st.CreatedAt.IsZero() {
		st.CreatedAt = time.Now()
	}
	s.statuses[st.ID] = st
	return st, nil
}

func (s *Store) StatusesByWorkspace(_ context.Context, workspaceID string) ([]store.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Status
	for _, st := range s.statuses {
		if st.WorkspaceID == workspaceID {
			out = append(out, st)
		}
	}
	// Board first (default board leads), then lane position within it — lanes
	// share a 0-based sequence per board, so position alone would interleave.
	sort.Slice(out, func(i, j int) bool {
		if bi, bj := s.boards[out[i].BoardID].Position, s.boards[out[j].BoardID].Position; bi != bj {
			return bi < bj
		}
		return out[i].Position < out[j].Position
	})
	return out, nil
}

func (s *Store) StatusesByBoard(_ context.Context, boardID string) ([]store.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Status
	for _, st := range s.statuses {
		if st.BoardID == boardID {
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}

func (s *Store) StatusByID(_ context.Context, id string) (store.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.statuses[id]
	if !ok {
		return store.Status{}, store.ErrStatusNotFound
	}
	return st, nil
}

func (s *Store) RenameStatus(_ context.Context, id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.statuses[id]
	if !ok {
		return store.ErrStatusNotFound
	}
	st.Name = name
	s.statuses[id] = st
	return nil
}

func (s *Store) SetStatusColor(_ context.Context, id, color string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.statuses[id]
	if !ok {
		return store.ErrStatusNotFound
	}
	st.Color = color
	s.statuses[id] = st
	return nil
}

func (s *Store) ReorderStatuses(_ context.Context, workspaceID string, orderedIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, id := range orderedIDs {
		if st, ok := s.statuses[id]; ok && st.WorkspaceID == workspaceID {
			st.Position = i
			s.statuses[id] = st
		}
	}
	return nil
}

func (s *Store) DeleteStatus(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.statuses[id]; !ok {
		return store.ErrStatusNotFound
	}
	delete(s.statuses, id)
	return nil
}

// --- board: items ---

func (s *Store) CreateItem(_ context.Context, it store.Item) (store.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if it.ID == "" {
		it.ID = newID()
	}
	if it.CreatedAt.IsZero() {
		it.CreatedAt = time.Now()
	}
	s.itemSeq[it.WorkspaceID]++
	it.RefNum = s.itemSeq[it.WorkspaceID]
	s.items[it.ID] = it
	return it, nil
}

func (s *Store) ItemByRef(_ context.Context, workspaceID string, refNum int) (store.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.items {
		if it.WorkspaceID == workspaceID && it.RefNum == refNum {
			return it, nil
		}
	}
	return store.Item{}, store.ErrItemNotFound
}

func (s *Store) ItemsByWorkspace(_ context.Context, workspaceID string) ([]store.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.filterItems(func(it store.Item) bool {
		return it.WorkspaceID == workspaceID && it.ParentID == "" && it.ArchivedAt == nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}

func (s *Store) AllItemsByWorkspace(_ context.Context, workspaceID string) ([]store.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.filterItems(func(it store.Item) bool {
		return it.WorkspaceID == workspaceID && it.ArchivedAt == nil
	})
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title) })
	return out, nil
}

func (s *Store) ItemsByStatus(_ context.Context, statusID string) ([]store.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.filterItems(func(it store.Item) bool {
		return it.StatusID == statusID && it.ParentID == "" && it.ArchivedAt == nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}

func (s *Store) ArchivedItemsByWorkspace(_ context.Context, workspaceID string) ([]store.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Archived subtree roots: archived, and either top-level or with a parent
	// that isn't itself archived.
	out := s.filterItems(func(it store.Item) bool {
		if it.WorkspaceID != workspaceID || it.ArchivedAt == nil {
			return false
		}
		if it.ParentID == "" {
			return true
		}
		parent, ok := s.items[it.ParentID]
		return !ok || parent.ArchivedAt == nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ArchivedAt.After(*out[j].ArchivedAt) })
	return out, nil
}

func (s *Store) ChildrenByParent(_ context.Context, parentID string, includeArchived bool) ([]store.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.filterItems(func(it store.Item) bool {
		return it.ParentID == parentID && (includeArchived || it.ArchivedAt == nil)
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}

func (s *Store) SubtaskCountsByWorkspace(_ context.Context, workspaceID, doneStatusID string) (map[string]store.SubtaskCount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]store.SubtaskCount{}
	for _, it := range s.items {
		if it.WorkspaceID != workspaceID || it.ParentID == "" || it.ArchivedAt != nil {
			continue
		}
		c := out[it.ParentID]
		c.Total++
		if doneStatusID != "" && it.StatusID == doneStatusID {
			c.Done++
		}
		out[it.ParentID] = c
	}
	return out, nil
}

// filterItems collects matching items (unordered). Caller holds the lock and
// sorts as needed.
func (s *Store) filterItems(keep func(store.Item) bool) []store.Item {
	var out []store.Item
	for _, it := range s.items {
		if keep(it) {
			out = append(out, it)
		}
	}
	return out
}

func (s *Store) ItemByID(_ context.Context, id string) (store.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	if !ok {
		return store.Item{}, store.ErrItemNotFound
	}
	return it, nil
}

func (s *Store) RenameItem(_ context.Context, id, title string) error {
	return s.mutateItem(id, func(it *store.Item) { it.Title = title })
}

func (s *Store) UpdateItemDescription(_ context.Context, id, description string) error {
	return s.mutateItem(id, func(it *store.Item) { it.Description = description })
}

func (s *Store) SetItemAssignee(_ context.Context, id, assigneeID string) error {
	return s.mutateItem(id, func(it *store.Item) { it.AssigneeID = assigneeID })
}

func (s *Store) SetItemStatus(_ context.Context, id, statusID string) error {
	return s.mutateItem(id, func(it *store.Item) { it.StatusID = statusID })
}

func (s *Store) SetItemParent(_ context.Context, id, parentID string) error {
	return s.mutateItem(id, func(it *store.Item) { it.ParentID = parentID })
}

func (s *Store) SetItemMilestone(_ context.Context, id string, isMilestone bool) error {
	return s.mutateItem(id, func(it *store.Item) { it.IsMilestone = isMilestone })
}

func (s *Store) ReorderMilestones(_ context.Context, workspaceID string, orderedIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, id := range orderedIDs {
		if it, ok := s.items[id]; ok && it.WorkspaceID == workspaceID && it.IsMilestone {
			it.MSPosition = i
			s.items[id] = it
		}
	}
	return nil
}

func (s *Store) SetItemPositions(_ context.Context, orderedIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, id := range orderedIDs {
		if it, ok := s.items[id]; ok {
			it.Position = i
			s.items[id] = it
		}
	}
	return nil
}

func (s *Store) ArchiveItem(_ context.Context, id string) error {
	now := time.Now()
	return s.mutateItem(id, func(it *store.Item) { it.ArchivedAt = &now })
}

func (s *Store) UnarchiveItem(_ context.Context, id string) error {
	return s.mutateItem(id, func(it *store.Item) { it.ArchivedAt = nil })
}

// mutateItem applies fn to a stored item, or returns ErrItemNotFound.
func (s *Store) mutateItem(id string, fn func(*store.Item)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	if !ok {
		return store.ErrItemNotFound
	}
	fn(&it)
	s.items[id] = it
	return nil
}

func (s *Store) ReorderItems(_ context.Context, statusID string, orderedIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, id := range orderedIDs {
		if it, ok := s.items[id]; ok {
			it.StatusID = statusID
			it.Position = i
			s.items[id] = it
		}
	}
	return nil
}

func (s *Store) DeleteItem(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return store.ErrItemNotFound
	}
	delete(s.items, id)
	return nil
}

// --- projects ---

func (s *Store) CreateProject(_ context.Context, pr store.Project) (store.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ex := range s.projects {
		if ex.WorkspaceID == pr.WorkspaceID && ex.Slug == pr.Slug {
			return store.Project{}, store.ErrProjectSlugTaken
		}
	}
	if pr.ID == "" {
		pr.ID = newID()
	}
	if pr.CreatedAt.IsZero() {
		pr.CreatedAt = time.Now()
	}
	s.projects[pr.ID] = pr
	return pr, nil
}

func (s *Store) ProjectsByWorkspace(_ context.Context, workspaceID string, includeArchived bool) ([]store.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Project
	for _, pr := range s.projects {
		if pr.WorkspaceID != workspaceID {
			continue
		}
		if !includeArchived && pr.ArchivedAt != nil {
			continue
		}
		out = append(out, pr)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (s *Store) ProjectByID(_ context.Context, id string) (store.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pr, ok := s.projects[id]
	if !ok {
		return store.Project{}, store.ErrProjectNotFound
	}
	return pr, nil
}

func (s *Store) ProjectBySlug(_ context.Context, workspaceID, slug string) (store.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pr := range s.projects {
		if pr.WorkspaceID == workspaceID && pr.Slug == slug {
			return pr, nil
		}
	}
	return store.Project{}, store.ErrProjectNotFound
}

func (s *Store) UpdateProject(_ context.Context, pr store.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.projects[pr.ID]
	if !ok {
		return store.ErrProjectNotFound
	}
	for oid, ex := range s.projects {
		if oid != pr.ID && ex.WorkspaceID == cur.WorkspaceID && ex.Slug == pr.Slug {
			return store.ErrProjectSlugTaken
		}
	}
	cur.Slug = pr.Slug
	cur.Name = pr.Name
	cur.Brief = pr.Brief
	cur.LeadID = pr.LeadID
	cur.Status = pr.Status
	cur.Color = pr.Color
	s.projects[pr.ID] = cur
	return nil
}

func (s *Store) SetProjectArchived(_ context.Context, id string, archived bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pr, ok := s.projects[id]
	if !ok {
		return store.ErrProjectNotFound
	}
	if archived {
		now := time.Now()
		pr.ArchivedAt = &now
	} else {
		pr.ArchivedAt = nil
	}
	s.projects[id] = pr
	return nil
}

func (s *Store) SetItemProject(_ context.Context, id, projectID string) error {
	return s.mutateItem(id, func(it *store.Item) { it.ProjectID = projectID })
}

func (s *Store) ItemsByProject(_ context.Context, projectID string) ([]store.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.filterItems(func(it store.Item) bool {
		return it.ProjectID == projectID && it.ParentID == "" && it.ArchivedAt == nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) ProjectItemCounts(_ context.Context, workspaceID string, doneStatusIDs []string) (map[string]store.SubtaskCount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	done := make(map[string]bool, len(doneStatusIDs))
	for _, id := range doneStatusIDs {
		done[id] = true
	}
	out := map[string]store.SubtaskCount{}
	for _, it := range s.items {
		if it.WorkspaceID != workspaceID || it.ParentID != "" || it.ArchivedAt != nil || it.ProjectID == "" {
			continue
		}
		c := out[it.ProjectID]
		c.Total++
		if done[it.StatusID] {
			c.Done++
		}
		out[it.ProjectID] = c
	}
	return out, nil
}

// --- comments ---

func (s *Store) CreateComment(_ context.Context, c store.Comment) (store.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == "" {
		c.ID = newID()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	s.comments[c.ID] = c
	return c, nil
}

func (s *Store) CommentsByItem(_ context.Context, itemID string) ([]store.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Comment
	for _, c := range s.comments {
		if c.ItemID == itemID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) CommentByID(_ context.Context, id string) (store.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.comments[id]
	if !ok {
		return store.Comment{}, store.ErrCommentNotFound
	}
	return c, nil
}

func (s *Store) UpdateComment(_ context.Context, id, body string, editedAt time.Time) (store.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.comments[id]
	if !ok {
		return store.Comment{}, store.ErrCommentNotFound
	}
	c.Body = body
	c.EditedAt = &editedAt
	s.comments[id] = c
	return c, nil
}

func (s *Store) SoftDeleteComment(_ context.Context, id string, deletedAt time.Time) (store.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.comments[id]
	if !ok {
		return store.Comment{}, store.ErrCommentNotFound
	}
	c.DeletedAt = &deletedAt
	s.comments[id] = c
	return c, nil
}

// --- activity log ---

func (s *Store) RecordEvent(_ context.Context, e store.Event) (store.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.ID == "" {
		e.ID = newID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	if e.Data == nil {
		e.Data = map[string]string{}
	}
	s.events[e.ID] = e
	return e, nil
}

func (s *Store) EventsByItem(_ context.Context, itemID string, limit int) ([]store.Event, error) {
	return s.recentEvents(func(e store.Event) bool { return e.ItemID == itemID }, limit), nil
}

func (s *Store) EventsByWorkspace(_ context.Context, workspaceID string, limit int) ([]store.Event, error) {
	return s.recentEvents(func(e store.Event) bool { return e.WorkspaceID == workspaceID }, limit), nil
}

func (s *Store) EventsByBoard(_ context.Context, boardID string, limit int) ([]store.Event, error) {
	return s.recentEvents(func(e store.Event) bool { return e.BoardID == boardID }, limit), nil
}

func (s *Store) LatestEventForActor(_ context.Context, itemID, actorID, verb string, since time.Time) (store.Event, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest store.Event
	found := false
	for _, e := range s.events {
		if e.ItemID != itemID || e.ActorID != actorID || e.Verb != verb || e.CreatedAt.Before(since) {
			continue
		}
		if !found || e.CreatedAt.After(latest.CreatedAt) {
			latest, found = e, true
		}
	}
	return latest, found, nil
}

func (s *Store) TouchEvent(_ context.Context, id string, at time.Time, data map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.events[id]
	if !ok {
		return store.ErrItemNotFound
	}
	if data == nil {
		data = map[string]string{}
	}
	e.CreatedAt = at
	e.Data = data
	s.events[id] = e
	return nil
}

// recentEvents returns the events matching pred newest-first, capped at limit.
func (s *Store) recentEvents(pred func(store.Event) bool, limit int) []store.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Event
	for _, e := range s.events {
		if pred(e) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit = clampLimit(limit); len(out) > limit {
		out = out[:limit]
	}
	return out
}

func clampLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 200
	}
	return limit
}

// --- notifications ---

func (s *Store) CreateNotification(_ context.Context, n store.Notification) (store.Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n.ID == "" {
		n.ID = newID()
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	s.notifs[n.ID] = n
	return n, nil
}

func (s *Store) NotificationsByRecipient(_ context.Context, recipientID string, limit int) ([]store.Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Notification
	for _, n := range s.notifs {
		if n.RecipientID == recipientID {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit = clampLimit(limit); len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) UnreadNotificationsByRecipient(_ context.Context, recipientID string, limit int) ([]store.Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Notification
	for _, n := range s.notifs {
		if n.RecipientID == recipientID && n.ReadAt == nil {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit = clampLimit(limit); len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) UnreadNotificationCount(_ context.Context, recipientID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, x := range s.notifs {
		if x.RecipientID == recipientID && x.ReadAt == nil {
			n++
		}
	}
	return n, nil
}

func (s *Store) MarkNotificationRead(_ context.Context, id, recipientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.notifs[id]
	if !ok || n.RecipientID != recipientID || n.ReadAt != nil {
		return nil
	}
	now := time.Now()
	n.ReadAt = &now
	s.notifs[id] = n
	return nil
}

func (s *Store) MarkAllNotificationsRead(_ context.Context, recipientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, n := range s.notifs {
		if n.RecipientID == recipientID && n.ReadAt == nil {
			n.ReadAt = &now
			s.notifs[id] = n
		}
	}
	return nil
}

// --- subscriptions ---

// findSub returns the subscription matching the unique triple, if present. The
// caller holds s.mu.
func (s *Store) findSub(subscriberID, subjectType, subjectID string) (store.Subscription, bool) {
	for _, sub := range s.subs {
		if sub.SubscriberID == subscriberID && sub.SubjectType == subjectType && sub.SubjectID == subjectID {
			return sub, true
		}
	}
	return store.Subscription{}, false
}

func (s *Store) EnsureSubscription(_ context.Context, sub store.Subscription) (store.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.findSub(sub.SubscriberID, sub.SubjectType, sub.SubjectID); ok {
		return existing, nil // sticky: keep the existing filter untouched
	}
	if sub.ID == "" {
		sub.ID = newID()
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now()
	}
	s.subs[sub.ID] = sub
	return sub, nil
}

func (s *Store) SetSubscriptionEvents(_ context.Context, subscriberID, subjectType, subjectID string, events []string) (store.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.findSub(subscriberID, subjectType, subjectID); ok {
		existing.Events = events
		s.subs[existing.ID] = existing
		return existing, nil
	}
	sub := store.Subscription{
		ID: newID(), SubscriberID: subscriberID, SubjectType: subjectType,
		SubjectID: subjectID, Events: events, CreatedAt: time.Now(),
	}
	s.subs[sub.ID] = sub
	return sub, nil
}

func (s *Store) DeleteSubscription(_ context.Context, subscriberID, subjectType, subjectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.findSub(subscriberID, subjectType, subjectID); ok {
		delete(s.subs, existing.ID)
	}
	return nil
}

func (s *Store) SubscriptionFor(_ context.Context, subscriberID, subjectType, subjectID string) (store.Subscription, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.findSub(subscriberID, subjectType, subjectID)
	return sub, ok, nil
}

func (s *Store) SubscriptionsBySubscriber(_ context.Context, subscriberID, subjectType string) ([]store.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Subscription
	for _, sub := range s.subs {
		if sub.SubscriberID == subscriberID && (subjectType == "" || sub.SubjectType == subjectType) {
			out = append(out, sub)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *Store) SubscribersForEvent(_ context.Context, itemID, projectID, actorID string) ([]store.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Subscription
	for _, sub := range s.subs {
		match := (sub.SubjectType == store.SubjectItem && sub.SubjectID == itemID) ||
			(sub.SubjectType == store.SubjectProject && projectID != "" && sub.SubjectID == projectID) ||
			(sub.SubjectType == store.SubjectPrincipal && actorID != "" && sub.SubjectID == actorID)
		if match {
			out = append(out, sub)
		}
	}
	return out, nil
}

// --- web push subscriptions ---

func (s *Store) CreatePushSubscription(_ context.Context, sub store.PushSubscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now()
	}
	s.pushSubs[sub.Endpoint] = sub // upsert on endpoint
	return nil
}

func (s *Store) PushSubscriptionsByUser(_ context.Context, userID string) ([]store.PushSubscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.PushSubscription
	for _, sub := range s.pushSubs {
		if sub.UserID == userID {
			out = append(out, sub)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) DeletePushSubscription(_ context.Context, endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pushSubs, endpoint)
	return nil
}

// --- app settings ---

func (s *Store) AppSetting(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings[key], nil
}

func (s *Store) SetAppSetting(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings[key] = value
	return nil
}

// --- mcp prompts ---

func (s *Store) CreateMCPPrompt(_ context.Context, p store.MCPPrompt) (store.MCPPrompt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ex := range s.mcpPrompts {
		if strings.EqualFold(ex.Name, p.Name) {
			return store.MCPPrompt{}, store.ErrMCPPromptNameTaken
		}
	}
	if p.ID == "" {
		p.ID = newID()
	}
	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	s.mcpPrompts[p.ID] = p
	return p, nil
}

func (s *Store) ListMCPPrompts(_ context.Context) ([]store.MCPPrompt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.MCPPrompt, 0, len(s.mcpPrompts))
	for _, p := range s.mcpPrompts {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (s *Store) MCPPromptByID(_ context.Context, id string) (store.MCPPrompt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.mcpPrompts[id]
	if !ok {
		return store.MCPPrompt{}, store.ErrMCPPromptNotFound
	}
	return p, nil
}

func (s *Store) UpdateMCPPrompt(_ context.Context, p store.MCPPrompt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.mcpPrompts[p.ID]
	if !ok {
		return store.ErrMCPPromptNotFound
	}
	for oid, ex := range s.mcpPrompts {
		if oid != p.ID && strings.EqualFold(ex.Name, p.Name) {
			return store.ErrMCPPromptNameTaken
		}
	}
	cur.Name = p.Name
	cur.Title = p.Title
	cur.Description = p.Description
	cur.Body = p.Body
	cur.Arguments = p.Arguments
	cur.Position = p.Position
	cur.UpdatedAt = time.Now()
	s.mcpPrompts[p.ID] = cur
	return nil
}

func (s *Store) DeleteMCPPrompt(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.mcpPrompts[id]; !ok {
		return store.ErrMCPPromptNotFound
	}
	delete(s.mcpPrompts, id)
	return nil
}

func (s *Store) Close() {}

// SessionCount is a test helper for asserting server-side invalidation.
func (s *Store) SessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

func newID() string { return id.New() }

var _ store.Store = (*Store)(nil)
