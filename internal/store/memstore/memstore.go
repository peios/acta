// Package memstore is an in-memory store.Store implementation. It backs unit
// and HTTP-level tests without a database, and doubles as a reference for what
// the interface requires.
package memstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/peios/acta/internal/store"
)

type Store struct {
	mu          sync.Mutex
	users       map[string]store.User
	sessions    map[string]store.Session
	credentials map[string]store.Credential
	challenges  map[string]store.Challenge
	workspaces  map[string]store.Workspace
	statuses    map[string]store.Status
	items       map[string]store.Item
	comments    map[string]store.Comment
}

func New() *Store {
	return &Store{
		users:       map[string]store.User{},
		sessions:    map[string]store.Session{},
		credentials: map[string]store.Credential{},
		challenges:  map[string]store.Challenge{},
		workspaces:  map[string]store.Workspace{},
		statuses:    map[string]store.Status{},
		items:       map[string]store.Item{},
		comments:    map[string]store.Comment{},
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

func (s *Store) CreateSession(_ context.Context, sess store.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
	return nil
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
	s.items[it.ID] = it
	return it, nil
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

func (s *Store) Close() {}

// SessionCount is a test helper for asserting server-side invalidation.
func (s *Store) SessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var _ store.Store = (*Store)(nil)
