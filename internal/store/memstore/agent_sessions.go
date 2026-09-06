package memstore

import (
	"context"
	"sort"
	"time"

	"github.com/peios/acta/internal/store"
)

// --- agent sessions ---

func (s *Store) CreateAgentSession(_ context.Context, as store.AgentSession) (store.AgentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if as.ID == "" {
		as.ID = newID()
	}
	if as.CreatedAt.IsZero() {
		as.CreatedAt = time.Now()
	}
	if as.UpdatedAt.IsZero() {
		as.UpdatedAt = as.CreatedAt
	}
	if as.Options == nil {
		as.Options = map[string]any{}
	}
	s.agentSessions[as.ID] = as
	return as, nil
}

func (s *Store) AgentSessionByID(_ context.Context, id string) (store.AgentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	as, ok := s.agentSessions[id]
	if !ok {
		return store.AgentSession{}, store.ErrAgentSessionNotFound
	}
	return as, nil
}

func (s *Store) AgentSessionsByOwner(_ context.Context, ownerID string) ([]store.AgentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.AgentSession
	for _, as := range s.agentSessions {
		if as.OwnerID == ownerID {
			out = append(out, as)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *Store) AgentSessionSizes(_ context.Context, ownerID string) (map[string]store.AgentSessionSize, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]store.AgentSessionSize{}
	for _, e := range s.agentEvents {
		as, ok := s.agentSessions[e.SessionID]
		if !ok || as.OwnerID != ownerID {
			continue
		}
		sz := out[e.SessionID]
		sz.Frames++
		sz.Bytes += int64(len(e.Payload))
		out[e.SessionID] = sz
	}
	return out, nil
}

func (s *Store) UpdateAgentSessionTitle(_ context.Context, id, title string, updatedAt time.Time) (store.AgentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	as, ok := s.agentSessions[id]
	if !ok {
		return store.AgentSession{}, store.ErrAgentSessionNotFound
	}
	as.Title = title
	as.UpdatedAt = updatedAt
	s.agentSessions[id] = as
	return as, nil
}

func (s *Store) UpdateAgentSessionOptions(_ context.Context, id string, options map[string]any, updatedAt time.Time) (store.AgentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	as, ok := s.agentSessions[id]
	if !ok {
		return store.AgentSession{}, store.ErrAgentSessionNotFound
	}
	as.Options = options
	as.UpdatedAt = updatedAt
	s.agentSessions[id] = as
	return as, nil
}

func (s *Store) DeleteAgentSession(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.agentSessions[id]; !ok {
		return store.ErrAgentSessionNotFound
	}
	delete(s.agentSessions, id)
	kept := s.agentEvents[:0]
	for _, e := range s.agentEvents {
		if e.SessionID != id {
			kept = append(kept, e)
		}
	}
	s.agentEvents = kept
	return nil
}

func (s *Store) AppendAgentSessionEvent(_ context.Context, e store.AgentSessionEvent) (store.AgentSessionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	as, ok := s.agentSessions[e.SessionID]
	if !ok {
		return store.AgentSessionEvent{}, store.ErrAgentSessionNotFound
	}
	s.agentEventSeq++
	e.Seq = s.agentEventSeq
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	s.agentEvents = append(s.agentEvents, e)
	as.UpdatedAt = e.CreatedAt
	s.agentSessions[as.ID] = as
	return e, nil
}

func (s *Store) AppendAgentSessionEvents(_ context.Context, events []store.AgentSessionEvent) ([]store.AgentSessionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.AgentSessionEvent, 0, len(events))
	for _, e := range events {
		as, ok := s.agentSessions[e.SessionID]
		if !ok {
			return nil, store.ErrAgentSessionNotFound
		}
		s.agentEventSeq++
		e.Seq = s.agentEventSeq
		if e.CreatedAt.IsZero() {
			e.CreatedAt = time.Now()
		}
		s.agentEvents = append(s.agentEvents, e)
		if e.CreatedAt.After(as.UpdatedAt) {
			as.UpdatedAt = e.CreatedAt
			s.agentSessions[as.ID] = as
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) AgentSessionEvents(_ context.Context, sessionID string, afterSeq int64, limit int) ([]store.AgentSessionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.agentSessions[sessionID]; !ok {
		return nil, store.ErrAgentSessionNotFound
	}
	var out []store.AgentSessionEvent
	for _, e := range s.agentEvents {
		if e.SessionID == sessionID && e.Seq > afterSeq {
			out = append(out, e)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
