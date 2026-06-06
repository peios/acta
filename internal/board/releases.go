package board

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/peios/acta/internal/store"
)

const (
	MaxReleaseNameLen = 80
	MaxReleaseDescLen = MaxDescriptionLen
)

var (
	ErrInvalidReleaseName   = errors.New("board: invalid release name")
	ErrInvalidReleaseDesc   = errors.New("board: release description too long")
	ErrInvalidReleaseStatus = errors.New("board: invalid release status")
	// ErrReleaseMismatch is returned when a release doesn't belong to the
	// workspace it's being used in — a malformed or cross-workspace request.
	ErrReleaseMismatch = errors.New("board: release not in this workspace")
	// ErrNotMilestone is returned when a convert-to-release is attempted on an
	// item that isn't a milestone.
	ErrNotMilestone = errors.New("board: item is not a milestone")
)

// ReleaseStatuses are the lifecycle states a release moves through: Planned (a
// future target being scoped) → Active (accruing current work) → Shipped (frozen
// changelog). A new release is created Planned or Active; Shipped is reached by
// shipping, never at creation.
var ReleaseStatuses = []string{"planned", "active", "shipped"}

func validReleaseStatus(s string) bool { return slices.Contains(ReleaseStatuses, s) }

// ReleaseColorFor returns a release's display colour: a stable palette colour
// derived from its position. Releases have no explicit colour (yet) — the dot is
// purely to tell them apart at a glance, mirroring ProjectColorFor's fallback.
func ReleaseColorFor(r store.Release) string {
	return Palette[((r.Position%len(Palette))+len(Palette))%len(Palette)]
}

// --- releases ---

// Releases lists a workspace's releases in display order (active and shipped).
func (s *Service) Releases(ctx context.Context, workspaceID string) ([]store.Release, error) {
	return s.store.ReleasesByWorkspace(ctx, workspaceID)
}

// Release resolves a release by id.
func (s *Service) Release(ctx context.Context, id string) (store.Release, error) {
	return s.store.ReleaseByID(ctx, id)
}

// ReleaseItems returns a release's active top-level items, newest first — its
// changelog.
func (s *Service) ReleaseItems(ctx context.Context, releaseID string) ([]store.Item, error) {
	return s.store.ItemsByRelease(ctx, releaseID)
}

// ReleasesForItem returns the releases an item belongs to (position order). The
// UI keeps this to at most one, but the store models it as many.
func (s *Service) ReleasesForItem(ctx context.Context, itemID string) ([]store.Release, error) {
	return s.store.ReleasesByItem(ctx, itemID)
}

// ReleaseLinks maps every linked item in the workspace to its release ids,
// backing the board's per-card chip and the release filter.
func (s *Service) ReleaseLinks(ctx context.Context, workspaceID string) (map[string][]string, error) {
	return s.store.ReleaseLinksByWorkspace(ctx, workspaceID)
}

// CreateRelease makes a release in a workspace. name is required and unique
// within the workspace (case-insensitively); description is optional. status is
// the lifecycle state it lands in — "planned" or "active" ("" defaults to
// active; "shipped" is rejected, since shipping is a later transition).
// createdBy records the authoring principal ("" if unknown).
func (s *Service) CreateRelease(ctx context.Context, workspaceID, name, description, status, createdBy string) (store.Release, error) {
	name, err := cleanReleaseName(name)
	if err != nil {
		return store.Release{}, err
	}
	if len([]rune(description)) > MaxReleaseDescLen {
		return store.Release{}, ErrInvalidReleaseDesc
	}
	if status == "" {
		status = "active"
	}
	if status != "planned" && status != "active" {
		return store.Release{}, ErrInvalidReleaseStatus
	}
	existing, err := s.store.ReleasesByWorkspace(ctx, workspaceID)
	if err != nil {
		return store.Release{}, err
	}
	return s.store.CreateRelease(ctx, store.Release{
		WorkspaceID: workspaceID,
		Name:        name,
		Description: description,
		Status:      status,
		Position:    len(existing),
		CreatedBy:   createdBy,
	})
}

// UpdateRelease edits a release's name and description. The name stays unique
// within the workspace.
func (s *Service) UpdateRelease(ctx context.Context, id, name, description string) error {
	cur, err := s.store.ReleaseByID(ctx, id)
	if err != nil {
		return err
	}
	name, err = cleanReleaseName(name)
	if err != nil {
		return err
	}
	if len([]rune(description)) > MaxReleaseDescLen {
		return ErrInvalidReleaseDesc
	}
	return s.store.UpdateRelease(ctx, store.Release{ID: id, WorkspaceID: cur.WorkspaceID, Name: name, Description: description})
}

// SetReleaseStatus moves a release along its lifecycle: planned ↔ active, and
// either → shipped (which stamps shipped_at) or back (which clears it — a
// premature ship, or activating a plan). Rejects an unknown status.
func (s *Service) SetReleaseStatus(ctx context.Context, id, status string) error {
	if !validReleaseStatus(status) {
		return ErrInvalidReleaseStatus
	}
	return s.store.SetReleaseStatus(ctx, id, status)
}

// DeleteRelease removes a release and its memberships (the join cascades). Its
// items are untouched — they just lose this release tag.
func (s *Service) DeleteRelease(ctx context.Context, id string) error {
	return s.store.DeleteRelease(ctx, id)
}

// ConvertMilestoneToRelease turns a milestone into a release: it creates an
// active release from the milestone's title and description, moves each of the
// milestone's sub-tasks into the release (promoting them to top-level items so
// the release lists them), then archives the now-empty milestone. The new
// release is returned. Errors if the item isn't a milestone, or if a release
// with the milestone's name already exists (ErrReleaseNameTaken).
func (s *Service) ConvertMilestoneToRelease(ctx context.Context, itemID, createdBy string) (store.Release, error) {
	item, err := s.store.ItemByID(ctx, itemID)
	if err != nil {
		return store.Release{}, err
	}
	if !item.IsMilestone {
		return store.Release{}, ErrNotMilestone
	}
	rel, err := s.CreateRelease(ctx, item.WorkspaceID, item.Title, item.Description, "active", createdBy)
	if err != nil {
		return store.Release{}, err
	}
	children, err := s.Children(ctx, itemID)
	if err != nil {
		return store.Release{}, err
	}
	for _, c := range children {
		if err := s.Reparent(ctx, c.ID, ""); err != nil { // promote to top-level
			return store.Release{}, err
		}
		if err := s.SetItemRelease(ctx, c.ID, rel.ID); err != nil {
			return store.Release{}, err
		}
	}
	if err := s.Archive(ctx, itemID); err != nil {
		return store.Release{}, err
	}
	return rel, nil
}

// SetItemRelease puts an item in a release (or clears it with ""), replacing any
// existing membership — the one-release-per-item UI write path. The release must
// belong to the item's workspace. Idempotent; records an activity event on change.
func (s *Service) SetItemRelease(ctx context.Context, itemID, releaseID string) error {
	item, err := s.store.ItemByID(ctx, itemID)
	if err != nil {
		return err
	}
	releaseName := ""
	if releaseID != "" {
		rel, err := s.store.ReleaseByID(ctx, releaseID)
		if err != nil {
			return err
		}
		if rel.WorkspaceID != item.WorkspaceID {
			return ErrReleaseMismatch
		}
		releaseName = rel.Name
	}
	cur, err := s.store.ReleasesByItem(ctx, itemID)
	if err != nil {
		return err
	}
	// Already in exactly this state — nothing to do, no event.
	if releaseID == "" && len(cur) == 0 {
		return nil
	}
	if len(cur) == 1 && cur[0].ID == releaseID {
		return nil
	}
	if err := s.store.SetItemRelease(ctx, itemID, releaseID); err != nil {
		return err
	}
	s.recordEvent(ctx, item, store.EventItemRelease, map[string]string{"to": releaseName})
	return nil
}

// ReleaseProgress returns per-release top-level item progress for a workspace,
// Done counting items in their board's last lane — the same "done = last lane"
// rule the board and project overviews use. Backs the overview's progress bars.
func (s *Service) ReleaseProgress(ctx context.Context, workspaceID string) (map[string]store.SubtaskCount, error) {
	done, err := s.doneStatusIDs(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.store.ReleaseItemCounts(ctx, workspaceID, done)
}

func cleanReleaseName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > MaxReleaseNameLen {
		return "", ErrInvalidReleaseName
	}
	return name, nil
}
