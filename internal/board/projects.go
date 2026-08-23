package board

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/peios/acta/internal/store"
)

const (
	MaxProjectNameLen  = 80
	MaxProjectBriefLen = MaxDescriptionLen
)

var (
	ErrInvalidProjectName   = errors.New("board: invalid project name")
	ErrInvalidProjectBrief  = errors.New("board: project brief too long")
	ErrInvalidProjectStatus = errors.New("board: invalid project status")
	// ErrProjectMismatch is returned when a project doesn't belong to the
	// workspace it's being used in — a malformed or cross-workspace request.
	ErrProjectMismatch = errors.New("board: project not in this workspace")
)

// ProjectStatuses are the lifecycle states a project moves through, in display
// order. A new project defaults to "active".
var ProjectStatuses = []string{"planned", "active", "paused", "done"}

func validProjectStatus(s string) bool {
	return slices.Contains(ProjectStatuses, s)
}

// ProjectColorFor returns a project's display colour: its explicit Color when
// set, otherwise a stable palette colour derived from its position. Mirrors
// ColorFor for statuses, so projects share the board's palette.
func ProjectColorFor(p store.Project) string {
	if p.Color != "" {
		return p.Color
	}
	return Palette[((p.Position%len(Palette))+len(Palette))%len(Palette)]
}

var nonSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify lowercases s and collapses every run of non-alphanumerics to a single
// hyphen, trimming hyphens from the ends.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlugRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// uniqueSlug returns base if free, else base-2, base-3, … — the first not in
// taken. An empty base falls back to "project".
func uniqueSlug(base string, taken map[string]bool) string {
	if base == "" {
		base = "project"
	}
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		cand := base + "-" + strconv.Itoa(i)
		if !taken[cand] {
			return cand
		}
	}
}

// --- projects ---

// Projects lists a workspace's projects in display order; includeArchived false
// omits archived ones.
func (s *Service) Projects(ctx context.Context, workspaceID string, includeArchived bool) ([]store.Project, error) {
	return s.store.ProjectsByWorkspace(ctx, workspaceID, includeArchived)
}

// Project resolves a project by id.
func (s *Service) Project(ctx context.Context, id string) (store.Project, error) {
	return s.store.ProjectByID(ctx, id)
}

// ProjectBySlug resolves a project within a workspace by its URL slug.
func (s *Service) ProjectBySlug(ctx context.Context, workspaceID, slug string) (store.Project, error) {
	return s.store.ProjectBySlug(ctx, workspaceID, slug)
}

// ProjectItems returns a project's active top-level items, newest first.
func (s *Service) ProjectItems(ctx context.Context, projectID string) ([]store.Item, error) {
	return s.store.ItemsByProject(ctx, projectID)
}

// CreateProject makes a project in a workspace. name is required; the URL slug
// is derived from it and made unique within the workspace. brief, leadID and
// color are optional; status defaults to "active" when blank. createdBy records
// the authoring principal ("" when unknown).
func (s *Service) CreateProject(ctx context.Context, workspaceID, name, brief, leadID, status, color, createdBy string) (store.Project, error) {
	name, err := cleanProjectName(name)
	if err != nil {
		return store.Project{}, err
	}
	if len([]rune(brief)) > MaxProjectBriefLen {
		return store.Project{}, ErrInvalidProjectBrief
	}
	if status == "" {
		status = "active"
	}
	if !validProjectStatus(status) {
		return store.Project{}, ErrInvalidProjectStatus
	}
	color, err = cleanColor(color)
	if err != nil {
		return store.Project{}, err
	}
	if leadID != "" {
		if _, err := s.store.UserByID(ctx, leadID); err != nil {
			return store.Project{}, err
		}
	}
	existing, err := s.store.ProjectsByWorkspace(ctx, workspaceID, true)
	if err != nil {
		return store.Project{}, err
	}
	taken := make(map[string]bool, len(existing))
	for _, p := range existing {
		taken[p.Slug] = true
	}
	pr, err := s.store.CreateProject(ctx, store.Project{
		WorkspaceID: workspaceID,
		Slug:        uniqueSlug(slugify(name), taken),
		Name:        name,
		Brief:       brief,
		LeadID:      leadID,
		Status:      status,
		Color:       color,
		Position:    len(existing),
		CreatedBy:   createdBy,
	})
	if err != nil {
		return store.Project{}, err
	}
	s.autoSubscribe(ctx, createdBy, store.SubjectProject, pr.ID) // creator follows their project
	return pr, nil
}

// UpdateProject edits a project's mutable fields. The slug stays fixed (renaming
// doesn't move the URL — a deliberate re-slug would be a separate action), so
// links never break under a rename. brief, leadID and color may be cleared.
func (s *Service) UpdateProject(ctx context.Context, id, name, brief, leadID, status, color string) error {
	cur, err := s.store.ProjectByID(ctx, id)
	if err != nil {
		return err
	}
	name, err = cleanProjectName(name)
	if err != nil {
		return err
	}
	if len([]rune(brief)) > MaxProjectBriefLen {
		return ErrInvalidProjectBrief
	}
	if status == "" {
		status = "active"
	}
	if !validProjectStatus(status) {
		return ErrInvalidProjectStatus
	}
	color, err = cleanColor(color)
	if err != nil {
		return err
	}
	if leadID != "" {
		if _, err := s.store.UserByID(ctx, leadID); err != nil {
			return err
		}
	}
	return s.store.UpdateProject(ctx, store.Project{
		ID:          id,
		WorkspaceID: cur.WorkspaceID,
		Slug:        cur.Slug,
		Name:        name,
		Brief:       brief,
		LeadID:      leadID,
		Status:      status,
		Color:       color,
	})
}

// ArchiveProject and UnarchiveProject toggle a project's soft-delete. Archiving
// leaves its items filed under it (their project_id is untouched); the project
// just drops out of the default lists.
func (s *Service) ArchiveProject(ctx context.Context, id string) error {
	return s.store.SetProjectArchived(ctx, id, true)
}

func (s *Service) UnarchiveProject(ctx context.Context, id string) error {
	return s.store.SetProjectArchived(ctx, id, false)
}

// SetItemProject files an item under a project (or clears it with ""). The
// project must belong to the item's workspace. It's idempotent and does not
// touch the item's subtasks.
func (s *Service) SetItemProject(ctx context.Context, itemID, projectID string) error {
	item, err := s.store.ItemByID(ctx, itemID)
	if err != nil {
		return err
	}
	projectName := ""
	if projectID != "" {
		pr, err := s.store.ProjectByID(ctx, projectID)
		if err != nil {
			return err
		}
		if pr.WorkspaceID != item.WorkspaceID {
			return ErrProjectMismatch
		}
		projectName = pr.Name
	}
	if item.ProjectID == projectID {
		return nil
	}
	if err := s.store.SetItemProject(ctx, itemID, projectID); err != nil {
		return err
	}
	item.ProjectID = projectID
	s.recordEvent(ctx, item, store.EventItemProject, map[string]string{"to": projectName})
	return nil
}

// doneStatusIDs is the set of "done" lanes across a workspace's boards: each
// board's last lane.
func (s *Service) doneStatusIDs(ctx context.Context, workspaceID string) ([]string, error) {
	boards, err := s.store.BoardsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, b := range boards {
		lanes, err := s.store.StatusesByBoard(ctx, b.ID)
		if err != nil {
			return nil, err
		}
		if n := len(lanes); n > 0 {
			ids = append(ids, lanes[n-1].ID)
		}
	}
	return ids, nil
}

func cleanProjectName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > MaxProjectNameLen {
		return "", ErrInvalidProjectName
	}
	return name, nil
}
