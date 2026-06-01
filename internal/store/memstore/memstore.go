// Package memstore is an in-memory store.Store implementation. It backs unit
// and HTTP-level tests without a database, and doubles as a reference for what
// the interface requires.
package memstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/peios/acta/internal/store"
)

type Store struct {
	mu       sync.Mutex
	users    map[string]store.User
	sessions map[string]store.Session
}

func New() *Store {
	return &Store{
		users:    map[string]store.User{},
		sessions: map[string]store.Session{},
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
