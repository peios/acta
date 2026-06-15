// Package memory manages "memories": arbitrary markdown notes accumulated under
// a scope (agent, user, site, workspace, project). Every scope reuses the same
// store and service by varying Scope/ScopeID; the web UI and the MCP tools are
// just different front doors onto these operations. The service owns the rules
// that aren't pure persistence: validating name/summary/body, stamping who
// touched a memory (provenance), and the name-addressed upsert/append/edit the
// MCP tools lean on so agents never juggle ids.
package memory

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/peios/acta/internal/store"
)

// Scope vocabulary (store.ScopeAgent, store.ScopeUser, …) lives in the store
// package alongside the persisted column; callers pass a scope + scopeID pair.

// MaxNameLen / MaxSummaryLen bound a memory's label and one-line hook;
// MaxBodyLen bounds its markdown (matching the documents ceiling — generous, but
// a guard against an unbounded write).
const (
	MaxNameLen    = 200
	MaxSummaryLen = 280
	MaxBodyLen    = 4_000_000
)

var (
	// ErrInvalid is returned when a memory's name is empty/too long, or its
	// summary or body is too long.
	ErrInvalid = errors.New("memory: invalid name, summary, or body")
	// ErrEditNoMatch / ErrEditNotUnique come from Edit when old_string isn't
	// found, or is found more than once without replace_all.
	ErrEditNoMatch   = errors.New("memory: edit text not found")
	ErrEditNotUnique = errors.New("memory: edit text is not unique (pass replace_all)")
)

// SaveReplace overwrites the body; SaveAppend concatenates to it (creating the
// memory if it doesn't exist yet).
const (
	SaveReplace = "replace"
	SaveAppend  = "append"
)

type Service struct {
	store store.Store
	now   func() time.Time
}

func New(st store.Store) *Service { return &Service{store: st, now: time.Now} }

// List returns the memories under one scope, oldest-first.
func (s *Service) List(ctx context.Context, scope, scopeID string) ([]store.Memory, error) {
	return s.store.MemoriesByScope(ctx, scope, scopeID)
}

// Get returns one memory by id, or store.ErrMemoryNotFound if absent. The caller
// owns the scope check (it knows which scope it's serving).
func (s *Service) Get(ctx context.Context, id string) (store.Memory, error) {
	return s.store.MemoryByID(ctx, id)
}

// ByName returns the memory addressed by (scope, scopeID, name) and whether it
// exists — the lookup behind the name-addressed MCP operations.
func (s *Service) ByName(ctx context.Context, scope, scopeID, name string) (store.Memory, bool, error) {
	m, err := s.store.MemoryByScopeName(ctx, scope, scopeID, strings.TrimSpace(name))
	if errors.Is(err, store.ErrMemoryNotFound) {
		return store.Memory{}, false, nil
	}
	if err != nil {
		return store.Memory{}, false, err
	}
	return m, true, nil
}

// Create adds a memory under a scope. name is required (trimmed, bounded);
// summary and body are optional (bounded). actorID is the principal creating it.
func (s *Service) Create(ctx context.Context, scope, scopeID, name, summary, body, actorID string) (store.Memory, error) {
	name = strings.TrimSpace(name)
	if !valid(name, summary, body) {
		return store.Memory{}, ErrInvalid
	}
	return s.store.CreateMemory(ctx, store.Memory{
		Scope:     scope,
		ScopeID:   scopeID,
		Name:      name,
		Summary:   summary,
		Body:      body,
		CreatedBy: actorID,
		UpdatedBy: actorID,
	})
}

// Update replaces a memory's name, summary, and body in place, stamping
// updatedBy/updatedAt.
func (s *Service) Update(ctx context.Context, id, name, summary, body, actorID string) (store.Memory, error) {
	name = strings.TrimSpace(name)
	if !valid(name, summary, body) {
		return store.Memory{}, ErrInvalid
	}
	return s.store.UpdateMemory(ctx, id, name, summary, body, actorID, s.now())
}

// Save upserts by (scope, scopeID, name): it creates the memory if absent, else
// overwrites (SaveReplace) or appends to (SaveAppend) its body. An empty summary
// keeps the existing one on update. Returns the resulting memory.
func (s *Service) Save(ctx context.Context, scope, scopeID, name, summary, body, mode, actorID string) (store.Memory, error) {
	existing, found, err := s.ByName(ctx, scope, scopeID, name)
	if err != nil {
		return store.Memory{}, err
	}
	if !found {
		return s.Create(ctx, scope, scopeID, name, summary, body, actorID)
	}
	newBody := body
	if mode == SaveAppend {
		newBody = existing.Body
		if newBody != "" && body != "" {
			newBody += "\n"
		}
		newBody += body
	}
	newSummary := summary
	if summary == "" {
		newSummary = existing.Summary
	}
	return s.Update(ctx, existing.ID, existing.Name, newSummary, newBody, actorID)
}

// Edit makes a surgical change to a memory's body: it replaces oldStr with newStr
// (the file-Edit pattern). oldStr must appear exactly once unless replaceAll is
// set. Returns ErrMemoryNotFound, ErrEditNoMatch, or ErrEditNotUnique on failure.
func (s *Service) Edit(ctx context.Context, scope, scopeID, name, oldStr, newStr string, replaceAll bool, actorID string) (store.Memory, error) {
	m, found, err := s.ByName(ctx, scope, scopeID, name)
	if err != nil {
		return store.Memory{}, err
	}
	if !found {
		return store.Memory{}, store.ErrMemoryNotFound
	}
	n := strings.Count(m.Body, oldStr)
	switch {
	case oldStr == "" || n == 0:
		return store.Memory{}, ErrEditNoMatch
	case n > 1 && !replaceAll:
		return store.Memory{}, ErrEditNotUnique
	}
	var body string
	if replaceAll {
		body = strings.ReplaceAll(m.Body, oldStr, newStr)
	} else {
		body = strings.Replace(m.Body, oldStr, newStr, 1)
	}
	if len([]rune(body)) > MaxBodyLen {
		return store.Memory{}, ErrInvalid
	}
	return s.Update(ctx, m.ID, m.Name, m.Summary, body, actorID)
}

// Delete removes a memory by id.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.store.DeleteMemory(ctx, id)
}

func valid(name, summary, body string) bool {
	return name != "" &&
		len([]rune(name)) <= MaxNameLen &&
		len([]rune(summary)) <= MaxSummaryLen &&
		len([]rune(body)) <= MaxBodyLen
}
