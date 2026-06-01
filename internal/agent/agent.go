// Package agent manages agent principals: PAT-only accounts that act on behalf
// of a human. An agent is just a store.User with AgentOfID set and no password
// or passkey, so it reuses the whole identity/assignee/token machinery; its
// handle is "owner/agentname". This service owns the rules that aren't pure
// persistence — validating the name, composing the handle, and refusing to nest
// agents.
package agent

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/peios/acta/internal/store"
)

// MaxNameLen bounds the local part of an agent handle.
const MaxNameLen = 40

var (
	ErrInvalidName  = errors.New("agent: invalid name")
	ErrOwnerIsAgent = errors.New("agent: an agent cannot own agents")
	ErrNameTaken    = errors.New("agent: name already taken")
	ErrNotOwned     = errors.New("agent: not owned by this user")
)

// nameRe constrains an agent's local name to lowercase letters, digits, and
// single internal hyphens — no slash (the handle separator), spaces, or
// uppercase. Keeps handles unambiguous and URL-clean.
var nameRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Service struct {
	store store.Store
}

func New(st store.Store) *Service { return &Service{store: st} }

// Create makes a new agent owned by ownerID. name is the local part of the
// handle ("owner/name"); display is the friendly label (defaults to the handle
// when empty). The owner must be a human — agents can't own agents.
func (s *Service) Create(ctx context.Context, ownerID, name, display string) (store.User, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || len(name) > MaxNameLen || !nameRe.MatchString(name) {
		return store.User{}, ErrInvalidName
	}
	owner, err := s.store.UserByID(ctx, ownerID)
	if err != nil {
		return store.User{}, err
	}
	if owner.AgentOfID != "" {
		return store.User{}, ErrOwnerIsAgent
	}

	username := owner.Username + "/" + name
	display = strings.TrimSpace(display)
	if display == "" {
		display = username
	}
	u, err := s.store.CreateUser(ctx, store.NewUser{
		Username:  username,
		Display:   display,
		AgentOfID: ownerID,
	})
	if errors.Is(err, store.ErrUsernameTaken) {
		return store.User{}, ErrNameTaken
	}
	return u, err
}

// List returns ownerID's agents.
func (s *Service) List(ctx context.Context, ownerID string) ([]store.User, error) {
	return s.store.AgentsByOwner(ctx, ownerID)
}

// Get returns one of ownerID's agents by id, or ErrNotOwned if the id isn't an
// agent owned by ownerID. This is the ownership guard every agent-scoped action
// (token mint/revoke, delete) goes through.
func (s *Service) Get(ctx context.Context, agentID, ownerID string) (store.User, error) {
	a, err := s.store.UserByID(ctx, agentID)
	if err != nil {
		return store.User{}, err
	}
	if a.AgentOfID == "" || a.AgentOfID != ownerID {
		return store.User{}, ErrNotOwned
	}
	return a, nil
}

// Delete removes one of ownerID's agents; its tokens cascade away with it, and
// items it created keep their history with a null creator.
func (s *Service) Delete(ctx context.Context, agentID, ownerID string) error {
	if _, err := s.Get(ctx, agentID, ownerID); err != nil {
		return err
	}
	return s.store.DeleteUser(ctx, agentID)
}
