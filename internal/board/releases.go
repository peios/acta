package board

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

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
// active; "shipped" is rejected, since shipping is a later transition). target
// is the date it's aiming at, or nil for "when it's ready". createdBy records
// the authoring principal ("" if unknown).
func (s *Service) CreateRelease(ctx context.Context, workspaceID, name, description, status string, target *time.Time, createdBy string) (store.Release, error) {
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
		TargetDate:  normalizeTarget(target),
		Position:    len(existing),
		CreatedBy:   createdBy,
	})
}

// UpdateRelease edits a release's name, description and target date (nil clears
// the target). The name stays unique within the workspace.
func (s *Service) UpdateRelease(ctx context.Context, id, name, description string, target *time.Time) error {
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
	return s.store.UpdateRelease(ctx, store.Release{
		ID: id, WorkspaceID: cur.WorkspaceID, Name: name, Description: description,
		TargetDate: normalizeTarget(target),
	})
}

// SetReleaseStatus moves a release along its lifecycle: planned ↔ active, and
// either → shipped (which stamps shipped_at) or back (which clears it — a
// premature ship, or activating a plan). Rejects an unknown status.
func (s *Service) SetReleaseStatus(ctx context.Context, id, status string) error {
	if !validReleaseStatus(status) {
		return ErrInvalidReleaseStatus
	}
	rel, err := s.store.ReleaseByID(ctx, id)
	if err != nil {
		return err
	}
	// Shipping freezes the release's history, so take a final reading first —
	// once it's shipped the sweep skips it, and a snapshot taken afterwards
	// would be skipped too. Best-effort: a missing last data point mustn't fail
	// the ship.
	if status == "shipped" && rel.Status != "shipped" {
		_ = s.SnapshotWorkspace(ctx, rel.WorkspaceID, s.now())
	}
	return s.store.SetReleaseStatus(ctx, id, status)
}

// DeleteRelease removes a release and its memberships (the join cascades). Its
// items are untouched — they just lose this release tag. The release's progress
// history goes with it: subject_id carries no FK, so that cascade is manual.
func (s *Service) DeleteRelease(ctx context.Context, id string) error {
	if err := s.store.DeleteRelease(ctx, id); err != nil {
		return err
	}
	return s.store.DeleteProgressSnapshots(ctx, SubjectRelease, id)
}

// ConvertMilestoneToRelease turns a milestone into a release: it creates an
// active release from the milestone's title and description — carrying its due
// date over as the release's target date — moves each of the milestone's
// sub-tasks into the release (promoting them to top-level items so the release
// lists them), then archives the now-empty milestone. The new
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
	rel, err := s.CreateRelease(ctx, item.WorkspaceID, item.Title, item.Description, "active", item.DueDate, createdBy)
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

// ReleaseMove is one thing that happened to a release's scope recently: an item
// finished, reopened, or added to the release.
type ReleaseMove struct {
	ItemID string
	Title  string
	Kind   string // "done" | "reopened" | "added"
	At     time.Time
}

// releaseMoveScan is how much of the activity log ReleaseMoves reads. The log is
// workspace-wide, so this is a window over everything that happened, not over
// this release — deep enough for a busy fortnight.
const releaseMoveScan = 1000

// ReleaseMoves is a release's recent story: which of its items crossed into a
// done lane, which came back out, and which were added to the release, newest
// first and capped at limit.
//
// It reads the activity log rather than diffing snapshots because the useful
// question here is *which* items moved; the snapshots answer "how much". Only
// items currently in the release are reported: one that has since moved on is
// no longer part of this release's story, and its old "added" line would read
// as a lie. Removals are absent for a related reason — an item.release event
// records the release an item moved *to*, not the one it left, so a departure
// is indistinguishable from any other clearing.
func (s *Service) ReleaseMoves(ctx context.Context, releaseID string, since time.Time, limit int) ([]ReleaseMove, error) {
	rel, err := s.store.ReleaseByID(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	items, err := s.store.ItemsByRelease(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	member := make(map[string]bool, len(items))
	for _, it := range items {
		member[it.ID] = true
	}
	doneIDs, err := s.doneStatusIDs(ctx, rel.WorkspaceID)
	if err != nil {
		return nil, err
	}
	doneLanes, err := s.doneLaneNames(ctx, rel.WorkspaceID, doneIDs)
	if err != nil {
		return nil, err
	}
	events, err := s.store.EventsByWorkspace(ctx, rel.WorkspaceID, releaseMoveScan)
	if err != nil {
		return nil, err
	}
	var out []ReleaseMove
	for _, e := range events { // newest first
		if len(out) >= limit || e.CreatedAt.Before(since) {
			break
		}
		if !member[e.ItemID] {
			continue // it isn't in the release now, so it isn't this release's story
		}
		kind := ""
		switch e.Verb {
		case store.EventItemRelease:
			if e.Data["to"] == rel.Name {
				kind = "added"
			}
		case store.EventItemStatusChange, store.EventItemStatusForced:
			switch {
			case doneLanes[e.Data["to"]]:
				kind = "done"
			case doneLanes[e.Data["from"]]:
				kind = "reopened"
			}
		}
		if kind == "" {
			continue
		}
		out = append(out, ReleaseMove{ItemID: e.ItemID, Title: e.ItemTitle, Kind: kind, At: e.CreatedAt})
	}
	return out, nil
}

// normalizeTarget floors a target date to UTC midnight; nil stays nil.
func normalizeTarget(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	d := truncDay(*t)
	return &d
}

func cleanReleaseName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > MaxReleaseNameLen {
		return "", ErrInvalidReleaseName
	}
	return name, nil
}
