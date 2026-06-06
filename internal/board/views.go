package board

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"github.com/peios/acta/internal/store"
)

// A board view is a saved, named filter: the filter-defining URL params captured
// as a normalised query string. The board's header tabs ship as seeded defaults
// (DefaultBoardViews); users add their own. See store.BoardView.

// defaultView is one seeded view. Position is its index in DefaultBoardViews.
type defaultView struct{ Slug, Name, Icon, Query string }

// DefaultBoardViews are the five tabs every board starts with. Keep these in
// sync with the backfill in migration 0027_board_views — the migration seeds
// existing boards, seedBoardViews seeds boards created later. Each Query is
// already in NormalizeViewQuery's canonical form.
var DefaultBoardViews = []defaultView{
	{"all-items", "All items", "columns", ""},
	{"my-items", "My items", "person", "assignee=me"},
	{"current-release", "Current Release", "hexagon", "release=active"},
	{"milestones", "Milestones", "diamond", "mode=milestone"},
	{"releases", "Releases", "cube", "mode=release"},
}

// viewFacetKeys are the multi-valued filter params a saved view retains.
var viewFacetKeys = []string{"status", "assignee", "project", "release", "priority", "type", "size"}

// GroupModes are the non-default board groupings (the ?mode= values). "status"
// is the default lane grouping and is not listed — it's the empty/absent mode.
// milestone and release have bespoke column layouts; the rest bucket items by a
// single key (an enum, the assignee, the project) or by date (due).
var GroupModes = []string{"milestone", "release", "priority", "type", "size", "due", "assignee", "project"}

// IsGroupMode reports whether m is a recognised non-default grouping. Used to
// validate the ?mode= param and to decide whether a saved view captures it.
func IsGroupMode(m string) bool { return slices.Contains(GroupModes, m) }

// SubgroupModes are the axes a board can sub-group by — a second level shown as
// sub-sections inside each primary group. These reduce to a single per-item key
// (an enum, the status, the assignee, the project, a due bucket); milestone and
// release (structural/membership groupings) aren't offered as sub-axes in v1.
var SubgroupModes = []string{"status", "priority", "type", "size", "due", "assignee", "project"}

// IsSubgroupMode reports whether s is a recognised sub-grouping axis.
func IsSubgroupMode(s string) bool { return slices.Contains(SubgroupModes, s) }

// OrderModes are the sort orders for cards within a group (the Display menu's
// Ordering control). "manual" is the default (stored drag position) and isn't
// listed. Each has a fixed sensible direction: priority urgent-first, due
// soonest-first (none last), title A–Z, created newest-first.
var OrderModes = []string{"priority", "due", "title", "created"}

// IsOrderMode reports whether o is a recognised non-default ordering.
func IsOrderMode(o string) bool { return slices.Contains(OrderModes, o) }

// NormalizeViewQuery reduces a board URL's query to the filter-defining params
// (mode, the facet keys, and q) in a canonical, order-independent form, dropping
// everything else (item=, board=, transient UI state). The result is what a view
// stores and what the active tab is matched against: two URLs that mean the same
// filter normalise to the same string. mode=status is the default, so it's
// omitted — the All-items view is the empty string.
func NormalizeViewQuery(v url.Values) string {
	out := url.Values{}
	if m := v.Get("mode"); IsGroupMode(m) {
		out.Set("mode", m)
	}
	// A sub-grouping is captured too, unless it's the no-op of matching the primary
	// grouping (the server ignores that), so canonical forms line up with what
	// renders. The primary defaults to status when absent.
	if s := v.Get("subgroup"); IsSubgroupMode(s) {
		m := v.Get("mode")
		if m == "" {
			m = "status"
		}
		if s != m {
			out.Set("subgroup", s)
		}
	}
	if o := v.Get("order"); IsOrderMode(o) {
		out.Set("order", o)
	}
	for _, key := range viewFacetKeys {
		for _, x := range dedupeSorted(v[key]) {
			out.Add(key, x)
		}
	}
	// due is a single-valued token (only "overdue" today), kept like q rather than
	// as a multi-valued facet.
	if d := v.Get("due"); d == "overdue" {
		out.Set("due", d)
	}
	if q := strings.TrimSpace(v.Get("q")); q != "" {
		out.Set("q", q)
	}
	return out.Encode()
}

// dedupeSorted trims blanks, removes duplicates, and sorts — so a facet's values
// have one canonical order regardless of how the URL listed them.
func dedupeSorted(vals []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

// ViewQueryHiddenByReleases reports whether a release-oriented view should be
// hidden because the workspace lacks the releases it references — preserving the
// old behaviour where the Current Release tab hid without an active release and
// the Releases tab hid without any release. A view filtering by a specific
// release hides the same way (no releases ⇒ nothing to show).
func ViewQueryHiddenByReleases(query string, hasReleases, hasActiveRelease bool) bool {
	v, err := url.ParseQuery(query)
	if err != nil {
		return false
	}
	needAny := v.Get("mode") == "release"
	needActive := false
	for _, r := range v["release"] {
		needAny = true
		if r == "active" {
			needActive = true
		}
	}
	if needActive && !hasActiveRelease {
		return true
	}
	return needAny && !hasReleases
}

// --- service ---

// BoardViews lists a board's saved views in position order.
func (s *Service) BoardViews(ctx context.Context, boardID string) ([]store.BoardView, error) {
	return s.store.BoardViewsByBoard(ctx, boardID)
}

// BoardView resolves a single view by id.
func (s *Service) BoardView(ctx context.Context, id string) (store.BoardView, error) {
	return s.store.BoardViewByID(ctx, id)
}

// CreateBoardView saves the current filter as a named view on a board. name is
// required; rawQuery is the board URL's query string (a leading "?" is fine),
// normalised before storing. The slug is derived from the name and made unique
// within the board. createdBy records the authoring principal ("" when unknown).
func (s *Service) CreateBoardView(ctx context.Context, boardID, name, rawQuery, createdBy string) (store.BoardView, error) {
	name, err := cleanName(name)
	if err != nil {
		return store.BoardView{}, err
	}
	b, err := s.store.BoardByID(ctx, boardID)
	if err != nil {
		return store.BoardView{}, err
	}
	values, _ := url.ParseQuery(strings.TrimPrefix(rawQuery, "?"))
	existing, err := s.store.BoardViewsByBoard(ctx, b.ID)
	if err != nil {
		return store.BoardView{}, err
	}
	taken := make(map[string]bool, len(existing))
	for _, v := range existing {
		taken[v.Slug] = true
	}
	return s.store.CreateBoardView(ctx, store.BoardView{
		WorkspaceID: b.WorkspaceID,
		BoardID:     b.ID,
		Slug:        uniqueSlug(slugify(name), taken),
		Name:        name,
		Icon:        "filter",
		Query:       NormalizeViewQuery(values),
		Position:    len(existing),
		CreatedBy:   createdBy,
	})
}

// RenameBoardView changes a view's display name (its slug is left alone).
func (s *Service) RenameBoardView(ctx context.Context, id, name string) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	return s.store.RenameBoardView(ctx, id, name)
}

// UpdateBoardViewQuery overwrites a view's stored filter with the current one —
// "save my changes to this view". rawQuery is the board URL's query string
// (a leading "?" is fine), normalised before storing.
func (s *Service) UpdateBoardViewQuery(ctx context.Context, id, rawQuery string) error {
	values, _ := url.ParseQuery(strings.TrimPrefix(rawQuery, "?"))
	return s.store.UpdateBoardViewQuery(ctx, id, NormalizeViewQuery(values))
}

// DeleteBoardView removes a view. Defaults are removable too — they're seeded
// rows, not protected built-ins.
func (s *Service) DeleteBoardView(ctx context.Context, id string) error {
	return s.store.DeleteBoardView(ctx, id)
}

// ReorderBoardViews sets the strip order from the given ids (board-scoped).
func (s *Service) ReorderBoardViews(ctx context.Context, boardID string, orderedIDs []string) error {
	return s.store.ReorderBoardViews(ctx, boardID, orderedIDs)
}

// seedBoardViews gives a freshly-created board its five default views. Mirrors
// the backfill in migration 0027 for boards created after that migration ran.
func (s *Service) seedBoardViews(ctx context.Context, workspaceID, boardID string) error {
	for i, dv := range DefaultBoardViews {
		if _, err := s.store.CreateBoardView(ctx, store.BoardView{
			WorkspaceID: workspaceID,
			BoardID:     boardID,
			Slug:        dv.Slug,
			Name:        dv.Name,
			Icon:        dv.Icon,
			Query:       dv.Query,
			Position:    i,
		}); err != nil {
			return err
		}
	}
	return nil
}
