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

// MaxPrefixLen bounds a manually-set item-id prefix (the default is 3 chars).
const MaxPrefixLen = 10

var (
	// ErrInvalidName is returned for an empty or over-long name.
	ErrInvalidName = errors.New("workspace: invalid name")
	// ErrInvalidSlug is returned when a requested slug has no usable characters.
	ErrInvalidSlug = errors.New("workspace: invalid slug")
	// ErrSlugReserved is returned when a requested slug collides with a built-in
	// route segment (it would shadow a real path like /settings or /login).
	ErrSlugReserved = errors.New("workspace: slug is reserved")
	// ErrInvalidPrefix is returned when a requested item prefix has no usable
	// (alphanumeric) characters.
	ErrInvalidPrefix = errors.New("workspace: invalid item prefix")
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
	"notifications": true, "welcome": true, "cli": true, "events": true,
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
	prefix, err := s.uniquePrefix(ctx, defaultPrefix(name))
	if err != nil {
		return store.Workspace{}, err
	}
	return s.store.CreateWorkspace(ctx, store.Workspace{
		Slug:       slug,
		Name:       name,
		ItemPrefix: prefix,
		CreatedBy:  createdBy,
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

// ByPrefix resolves a workspace by its item-id prefix (case-insensitive), for
// turning a human id like ACTA-12 back into its workspace.
func (s *Service) ByPrefix(ctx context.Context, prefix string) (store.Workspace, error) {
	return s.store.WorkspaceByPrefix(ctx, prefix)
}

// Rename changes only the display name; the slug is left untouched.
func (s *Service) Rename(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > MaxNameLen {
		return ErrInvalidName
	}
	return s.store.RenameWorkspace(ctx, id, name)
}

// Update renames the workspace and optionally re-slugs / re-prefixes it. An
// empty rawSlug or rawPrefix leaves that field untouched, so a name-only edit
// never moves the URL or relabels items. A new slug must be non-empty,
// unreserved, and unused; a new prefix must have usable characters and be
// globally unique (case-insensitive), since human ids resolve by prefix.
func (s *Service) Update(ctx context.Context, id, name, rawSlug, rawPrefix string) error {
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
	prefix := ws.ItemPrefix
	if strings.TrimSpace(rawPrefix) != "" {
		candidate, ok := normalizePrefix(rawPrefix)
		if !ok {
			return ErrInvalidPrefix
		}
		if !strings.EqualFold(candidate, ws.ItemPrefix) {
			switch w, err := s.store.WorkspaceByPrefix(ctx, candidate); {
			case err == nil && w.ID != id:
				return store.ErrWorkspacePrefixTaken
			case err != nil && !errors.Is(err, store.ErrWorkspaceNotFound):
				return err
			}
		}
		prefix = candidate
	}
	return s.store.UpdateWorkspace(ctx, id, name, slug, prefix)
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

// uniquePrefix returns base, or base2, base3… until it finds one no workspace
// is using. An empty base (a name with no usable letters) yields "" — the
// workspace simply has no prefix and its items show as bare numbers.
func (s *Service) uniquePrefix(ctx context.Context, base string) (string, error) {
	if base == "" {
		return "", nil
	}
	candidate := base
	for i := 2; ; i++ {
		_, err := s.store.WorkspaceByPrefix(ctx, candidate)
		if errors.Is(err, store.ErrWorkspaceNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = base + strconv.Itoa(i)
	}
}

// defaultPrefix derives a 3-letter item-id prefix from a workspace name:
//   - 3+ words  → first letter of the first three words (Foo Bar Baz → FBB)
//   - 2 words   → word1[0], word1[1], word2[0] (Platform Team → PLT)
//   - 1 word    → its first three letters (Acta → ACT, General → GEN)
//
// It returns up to three uppercase alphanumerics (fewer if the name is short),
// or "" when the name has no usable characters.
func defaultPrefix(name string) string {
	words := prefixWords(name)
	var b []rune
	switch {
	case len(words) >= 3:
		for _, w := range words[:3] {
			b = append(b, []rune(w)[0])
		}
	case len(words) == 2:
		w0 := []rune(words[0])
		b = append(b, w0[0])
		if len(w0) > 1 {
			b = append(b, w0[1])
		}
		b = append(b, []rune(words[1])[0])
	case len(words) == 1:
		for _, r := range words[0] {
			if len(b) == 3 {
				break
			}
			b = append(b, r)
		}
	}
	if len(b) > 3 {
		b = b[:3]
	}
	return string(b)
}

// prefixWords splits a name into uppercase alphanumeric words (runs of letters
// or digits), dropping any other characters.
func prefixWords(name string) []string {
	var words []string
	var cur []rune
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			cur = append(cur, r)
		} else if len(cur) > 0 {
			words = append(words, string(cur))
			cur = nil
		}
	}
	if len(cur) > 0 {
		words = append(words, string(cur))
	}
	return words
}

// normalizePrefix uppercases a user-typed prefix and keeps only alphanumerics,
// capping length. ok is false when nothing usable remains.
func normalizePrefix(raw string) (string, bool) {
	var b []rune
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b = append(b, r)
		}
	}
	if len(b) == 0 {
		return "", false
	}
	if len(b) > MaxPrefixLen {
		b = b[:MaxPrefixLen]
	}
	return string(b), true
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
