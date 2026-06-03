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
	// ErrInvalidSlug is returned when a requested slug has no usable characters.
	ErrInvalidSlug = errors.New("workspace: invalid slug")
	// ErrSlugReserved is returned when a requested slug collides with a built-in
	// route segment (it would shadow a real path like /settings or /login).
	ErrSlugReserved = errors.New("workspace: slug is reserved")
	// ErrLastWorkspace is returned when deleting would leave none. There must
	// always be at least one workspace for the switcher and / redirect.
	ErrLastWorkspace = errors.New("workspace: cannot delete the last workspace")
)

// reservedSlugs are the first path segments owned by built-in routes. Boards
// live at /<slug>, so a workspace slug must avoid these or it would shadow a
// real route (e.g. a workspace slugged "settings" hiding /settings). Kept here
// as the single source of truth — the router and board-prefs.js mirror it.
var reservedSlugs = map[string]bool{
	"w": true, "api": true, "mcp": true, "static": true, "assets": true,
	"login": true, "logout": true, "account": true, "settings": true,
	"notifications": true, "welcome": true, "cli": true,
	"favicon.ico": true, "robots.txt": true,
}

// IsReservedSlug reports whether slug is reserved for a built-in route.
func IsReservedSlug(slug string) bool { return reservedSlugs[slug] }

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

// Rename changes only the display name; the slug is left untouched.
func (s *Service) Rename(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > MaxNameLen {
		return ErrInvalidName
	}
	return s.store.RenameWorkspace(ctx, id, name)
}

// Update renames the workspace and, when rawSlug normalises to a slug different
// from the current one, re-slugs it too. An empty rawSlug — or one that yields
// the current slug — leaves the slug untouched, so a name-only edit never moves
// the URL. A new slug must be non-empty, unreserved, and unused.
func (s *Service) Update(ctx context.Context, id, name, rawSlug string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > MaxNameLen {
		return ErrInvalidName
	}
	ws, err := s.store.WorkspaceByID(ctx, id)
	if err != nil {
		return err
	}
	slug := ws.Slug
	if strings.TrimSpace(rawSlug) != "" {
		candidate := slugCore(rawSlug)
		if candidate == "" {
			return ErrInvalidSlug
		}
		if candidate != ws.Slug {
			if reservedSlugs[candidate] {
				return ErrSlugReserved
			}
			switch _, err := s.store.WorkspaceBySlug(ctx, candidate); {
			case err == nil:
				return store.ErrWorkspaceSlugTaken
			case !errors.Is(err, store.ErrWorkspaceNotFound):
				return err
			}
			slug = candidate
		}
	}
	return s.store.UpdateWorkspace(ctx, id, name, slug)
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

// uniqueSlug returns base, or base-2, base-3… skipping any candidate that is
// reserved or already taken. (A reserved base, e.g. from a workspace named
// "API", falls through to base-2.)
func (s *Service) uniqueSlug(ctx context.Context, base string) (string, error) {
	candidate := base
	for i := 2; ; i++ {
		if !reservedSlugs[candidate] {
			_, err := s.store.WorkspaceBySlug(ctx, candidate)
			if errors.Is(err, store.ErrWorkspaceNotFound) {
				return candidate, nil
			}
			if err != nil {
				return "", err
			}
		}
		candidate = base + "-" + strconv.Itoa(i)
	}
}

// slugify derives a URL slug from a name, falling back to "workspace" for names
// with no usable characters. Used when auto-deriving a slug on workspace
// creation; an explicit user-typed slug uses slugCore so emptiness is an error
// rather than a silent placeholder.
func slugify(name string) string {
	if s := slugCore(name); s != "" {
		return s
	}
	return "workspace"
}

// slugCore lowercases and collapses any run of non-alphanumeric characters to a
// single dash, trims leading/trailing dashes, and caps length so slugs stay
// tidy. Returns "" when the input has no usable characters.
func slugCore(name string) string {
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
	if len(out) > maxSlugLen {
		out = strings.Trim(out[:maxSlugLen], "-")
	}
	return out
}
