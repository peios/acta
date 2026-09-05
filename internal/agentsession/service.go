package agentsession

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peios/acta/internal/store"
)

// Service is the persistence-facing half: it mints session ids, creates and
// lists session records, and appends transcript frames. The live routing lives
// in Hub.
type Service struct {
	store store.Store
	now   func() time.Time
}

func New(st store.Store) *Service { return &Service{store: st, now: time.Now} }

var ErrNotFound = store.ErrAgentSessionNotFound

// Create records a new session owned by ownerID and mints its id — a UUID, so
// it can be handed to the backend as its own --session-id with no mapping.
func (s *Service) Create(ctx context.Context, ownerID, backend, cwd, title string, options map[string]any) (store.AgentSession, error) {
	if options == nil {
		options = map[string]any{}
	}
	return s.store.CreateAgentSession(ctx, store.AgentSession{
		ID:      uuid.NewString(),
		OwnerID: ownerID,
		Backend: backend,
		Cwd:     cwd,
		Title:   title,
		Options: options,
	})
}

// Get returns a session, checking it belongs to ownerID.
func (s *Service) Get(ctx context.Context, id, ownerID string) (store.AgentSession, error) {
	as, err := s.store.AgentSessionByID(ctx, id)
	if err != nil {
		return store.AgentSession{}, err
	}
	if as.OwnerID != ownerID {
		return store.AgentSession{}, ErrNotFound
	}
	return as, nil
}

// List returns ownerID's sessions, most recently updated first.
func (s *Service) List(ctx context.Context, ownerID string) ([]store.AgentSession, error) {
	return s.store.AgentSessionsByOwner(ctx, ownerID)
}

// Delete removes a session (and its transcript) after an ownership check.
func (s *Service) Delete(ctx context.Context, id, ownerID string) error {
	if _, err := s.Get(ctx, id, ownerID); err != nil {
		return err
	}
	return s.store.DeleteAgentSession(ctx, id)
}

// SetTitle renames a session after an ownership check.
func (s *Service) SetTitle(ctx context.Context, id, ownerID, title string) (store.AgentSession, error) {
	if _, err := s.Get(ctx, id, ownerID); err != nil {
		return store.AgentSession{}, err
	}
	return s.store.UpdateAgentSessionTitle(ctx, id, title, s.now())
}

// SetOption folds one key into a session's options (a resume starts with
// them). An empty value drops the key.
func (s *Service) SetOption(ctx context.Context, id, key string, value any) error {
	as, err := s.store.AgentSessionByID(ctx, id)
	if err != nil {
		return err
	}
	opts := map[string]any{}
	for k, v := range as.Options {
		opts[k] = v
	}
	if value == nil || value == "" {
		delete(opts, key)
	} else {
		opts[key] = value
	}
	_, err = s.store.UpdateAgentSessionOptions(ctx, id, opts, s.now())
	return err
}

// Append stores one transcript frame verbatim. payload is raw JSON; kind is the
// coarse filter label ("event"|"input"|"state").
func (s *Service) Append(ctx context.Context, sessionID, kind string, payload json.RawMessage) (store.AgentSessionEvent, error) {
	if len(payload) == 0 {
		payload = json.RawMessage("null")
	}
	return s.store.AppendAgentSessionEvent(ctx, store.AgentSessionEvent{
		SessionID: sessionID,
		Kind:      kind,
		Payload:   payload,
	})
}

// Events returns a session's transcript frames with seq > afterSeq.
func (s *Service) Events(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]store.AgentSessionEvent, error) {
	return s.store.AgentSessionEvents(ctx, sessionID, afterSeq, limit)
}

// OwnerOf returns a session's owner id, or ErrNotFound.
func (s *Service) OwnerOf(ctx context.Context, id string) (string, error) {
	as, err := s.store.AgentSessionByID(ctx, id)
	if errors.Is(err, store.ErrAgentSessionNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return as.OwnerID, nil
}
