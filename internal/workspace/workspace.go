// Package workspace owns workspace lifecycle: validating names, deriving the
// immutable URL slug, and enforcing the "always at least one" invariant. It
// sits between the HTTP handlers and the store so that slug generation and the
// last-workspace guard live in one tested place rather than in request code.
package workspace

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/peios/acta/internal/store"
)

// MaxNameLen bounds the human label; the slug is derived and capped separately.
const MaxNameLen = 60

var (
	// ErrInvalidName is returned for an empty or over-long name.
	ErrInvalidName = errors.New("workspace: invalid name")
	// ErrLastWorkspace is returned when deleting would leave none. There must
	// always be at least one workspace for the switcher and / redirect.
	ErrLastWorkspace = errors.New("workspace: cannot delete the last workspace")
)

type Service struct {
	store store.Store
}

func New(st store.Store) *Service { return &Service{store: st} }

// Create validates the name, derives a unique slug, and persists the workspace.
func (s *Service) Create(ctx context.Context, name, createdBy string) (store.Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > MaxNameLen {
		return store.Workspace{}, ErrInvalidName
	}
	slug, err := s.uniqueSlug(ctx, slugify(name))
	if err != nil {
		return store.Workspace{}, err
	}
	return s.store.CreateWorkspace(ctx, store.Workspace{
		Slug:      slug,
		Name:      name,
		CreatedBy: createdBy,
	})
}

func (s *Service) List(ctx context.Context) ([]store.Workspace, error) {
	return s.store.ListWorkspaces(ctx)
}

func (s *Service) ByID(ctx context.Context, id string) (store.Workspace, error) {
	return s.store.WorkspaceByID(ctx, id)
}

func (s *Service) BySlug(ctx context.Context, slug string) (store.Workspace, error) {
	return s.store.WorkspaceBySlug(ctx, slug)
}

// Rename changes only the display name; the slug is immutable so links survive.
func (s *Service) Rename(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > MaxNameLen {
		return ErrInvalidName
	}
	return s.store.RenameWorkspace(ctx, id, name)
}

// Delete removes a workspace, refusing to remove the last one.
func (s *Service) Delete(ctx context.Context, id string) error {
	n, err := s.store.CountWorkspaces(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastWorkspace
	}
	return s.store.DeleteWorkspace(ctx, id)
}

// uniqueSlug returns base, or base-2, base-3… if the slug is already taken.
func (s *Service) uniqueSlug(ctx context.Context, base string) (string, error) {
	candidate := base
	for i := 2; ; i++ {
		_, err := s.store.WorkspaceBySlug(ctx, candidate)
		if errors.Is(err, store.ErrWorkspaceNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = base + "-" + strconv.Itoa(i)
	}
}

// slugify lowercases and collapses any run of non-alphanumeric characters to a
// single dash, trimming leading/trailing dashes. Falls back to "workspace" for
// names with no usable characters, and caps length so slugs stay tidy.
func slugify(name string) string {
	const maxSlugLen = 48
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "workspace"
	}
	if len(out) > maxSlugLen {
		out = strings.Trim(out[:maxSlugLen], "-")
	}
	return out
}
