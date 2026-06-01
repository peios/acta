// Package board owns the board domain: user-defined statuses (lanes) and the
// items within them. It sits between the HTTP handlers and the store, holding
// the rules that aren't pure persistence — input validation, the
// delete-a-lane-only-when-empty guard, default-lane seeding, and the
// move/transition logic that keeps each lane's positions dense.
package board

import (
	"context"
	"errors"
	"strings"

	"github.com/peios/acta/internal/store"
)

const (
	MaxStatusNameLen = 40
	MaxItemTitleLen  = 200
)

var (
	ErrInvalidName    = errors.New("board: invalid status name")
	ErrInvalidTitle   = errors.New("board: invalid item title")
	ErrStatusNotEmpty = errors.New("board: status still has items")
	// ErrStatusMismatch is returned when a status doesn't belong to the
	// workspace it's being used in — a malformed or cross-workspace request.
	ErrStatusMismatch = errors.New("board: status not in this workspace")
)

// DefaultStatuses are the lanes seeded into a new workspace so its board is
// usable immediately.
var DefaultStatuses = []string{"To do", "Doing", "Done"}

type Service struct {
	store store.Store
}

func New(st store.Store) *Service { return &Service{store: st} }

// --- statuses ---

func (s *Service) Statuses(ctx context.Context, workspaceID string) ([]store.Status, error) {
	return s.store.StatusesByWorkspace(ctx, workspaceID)
}

func (s *Service) CreateStatus(ctx context.Context, workspaceID, name string) (store.Status, error) {
	name, err := cleanName(name)
	if err != nil {
		return store.Status{}, err
	}
	existing, err := s.store.StatusesByWorkspace(ctx, workspaceID)
	if err != nil {
		return store.Status{}, err
	}
	return s.store.CreateStatus(ctx, store.Status{
		WorkspaceID: workspaceID,
		Name:        name,
		Position:    len(existing),
	})
}

func (s *Service) RenameStatus(ctx context.Context, id, name string) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	return s.store.RenameStatus(ctx, id, name)
}

func (s *Service) ReorderStatuses(ctx context.Context, workspaceID string, orderedIDs []string) error {
	return s.store.ReorderStatuses(ctx, workspaceID, orderedIDs)
}

// DeleteStatus refuses to remove a lane that still holds items — the user
// empties it first. This keeps deletion from silently dropping work.
func (s *Service) DeleteStatus(ctx context.Context, id string) error {
	items, err := s.store.ItemsByStatus(ctx, id)
	if err != nil {
		return err
	}
	if len(items) > 0 {
		return ErrStatusNotEmpty
	}
	return s.store.DeleteStatus(ctx, id)
}

// --- items ---

func (s *Service) Items(ctx context.Context, workspaceID string) ([]store.Item, error) {
	return s.store.ItemsByWorkspace(ctx, workspaceID)
}

func (s *Service) CreateItem(ctx context.Context, workspaceID, statusID, title string) (store.Item, error) {
	title, err := cleanTitle(title)
	if err != nil {
		return store.Item{}, err
	}
	if err := s.requireStatusInWorkspace(ctx, statusID, workspaceID); err != nil {
		return store.Item{}, err
	}
	lane, err := s.store.ItemsByStatus(ctx, statusID)
	if err != nil {
		return store.Item{}, err
	}
	return s.store.CreateItem(ctx, store.Item{
		WorkspaceID: workspaceID,
		StatusID:    statusID,
		Title:       title,
		Position:    len(lane),
	})
}

func (s *Service) RenameItem(ctx context.Context, id, title string) error {
	title, err := cleanTitle(title)
	if err != nil {
		return err
	}
	return s.store.RenameItem(ctx, id, title)
}

// MoveItem transitions an item into toStatusID at the given index, keeping both
// the destination lane and (if it changed) the source lane densely ordered.
func (s *Service) MoveItem(ctx context.Context, itemID, toStatusID string, index int) error {
	item, err := s.store.ItemByID(ctx, itemID)
	if err != nil {
		return err
	}
	if err := s.requireStatusInWorkspace(ctx, toStatusID, item.WorkspaceID); err != nil {
		return err
	}

	// Destination order: the lane's current items minus this one (a no-op when
	// it's a cross-lane move), with the item spliced in at index.
	dest, err := s.store.ItemsByStatus(ctx, toStatusID)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(dest)+1)
	for _, it := range dest {
		if it.ID != itemID {
			ids = append(ids, it.ID)
		}
	}
	if index < 0 {
		index = 0
	}
	if index > len(ids) {
		index = len(ids)
	}
	ordered := make([]string, 0, len(ids)+1)
	ordered = append(ordered, ids[:index]...)
	ordered = append(ordered, itemID)
	ordered = append(ordered, ids[index:]...)
	if err := s.store.ReorderItems(ctx, toStatusID, ordered); err != nil {
		return err
	}

	// Re-densify the source lane if the item left it.
	if item.StatusID != toStatusID {
		src, err := s.store.ItemsByStatus(ctx, item.StatusID)
		if err != nil {
			return err
		}
		srcIDs := make([]string, len(src))
		for i, it := range src {
			srcIDs[i] = it.ID
		}
		return s.store.ReorderItems(ctx, item.StatusID, srcIDs)
	}
	return nil
}

func (s *Service) DeleteItem(ctx context.Context, id string) error {
	return s.store.DeleteItem(ctx, id)
}

// SeedDefaults gives a new workspace its starter lanes.
func (s *Service) SeedDefaults(ctx context.Context, workspaceID string) error {
	for i, name := range DefaultStatuses {
		if _, err := s.store.CreateStatus(ctx, store.Status{
			WorkspaceID: workspaceID,
			Name:        name,
			Position:    i,
		}); err != nil {
			return err
		}
	}
	return nil
}

// requireStatusInWorkspace confirms a status exists and belongs to workspaceID.
func (s *Service) requireStatusInWorkspace(ctx context.Context, statusID, workspaceID string) error {
	st, err := s.store.StatusByID(ctx, statusID)
	if err != nil {
		return err
	}
	if st.WorkspaceID != workspaceID {
		return ErrStatusMismatch
	}
	return nil
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > MaxStatusNameLen {
		return "", ErrInvalidName
	}
	return name, nil
}

func cleanTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > MaxItemTitleLen {
		return "", ErrInvalidTitle
	}
	return title, nil
}
