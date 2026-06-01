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
	MaxStatusNameLen  = 40
	MaxItemTitleLen   = 200
	MaxDescriptionLen = 20000
	MaxCommentLen     = 5000

	// endOfLane is a large index that MoveItem clamps to a lane's end; used by
	// status changes that carry no explicit position.
	endOfLane = 1 << 30
)

var (
	ErrInvalidName        = errors.New("board: invalid status name")
	ErrInvalidTitle       = errors.New("board: invalid item title")
	ErrInvalidDescription = errors.New("board: description too long")
	ErrInvalidComment     = errors.New("board: invalid comment")
	ErrStatusNotEmpty     = errors.New("board: status still has items")
	ErrNoStatus           = errors.New("board: workspace has no statuses")
	ErrCycle              = errors.New("board: would create a cycle")
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

// --- item detail ---

func (s *Service) Item(ctx context.Context, id string) (store.Item, error) {
	return s.store.ItemByID(ctx, id)
}

// Users lists every account, for the assignee picker (there's no membership
// model yet, so any user can be assigned).
func (s *Service) Users(ctx context.Context) ([]store.User, error) {
	return s.store.ListUsers(ctx)
}

func (s *Service) UpdateDescription(ctx context.Context, id, description string) error {
	if len([]rune(description)) > MaxDescriptionLen {
		return ErrInvalidDescription
	}
	return s.store.UpdateItemDescription(ctx, id, description)
}

// SetAssignee assigns the item to a user, or clears it when assigneeID is "".
func (s *Service) SetAssignee(ctx context.Context, id, assigneeID string) error {
	if assigneeID != "" {
		if _, err := s.store.UserByID(ctx, assigneeID); err != nil {
			return err
		}
	}
	return s.store.SetItemAssignee(ctx, id, assigneeID)
}

// SetStatus changes an item's status. A top-level item is repositioned to the
// end of the target lane; a subtask (which isn't on the board) just takes the
// new status, keeping its order within its parent.
func (s *Service) SetStatus(ctx context.Context, id, statusID string) error {
	item, err := s.store.ItemByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.requireStatusInWorkspace(ctx, statusID, item.WorkspaceID); err != nil {
		return err
	}
	if item.ParentID != "" {
		return s.store.SetItemStatus(ctx, id, statusID)
	}
	return s.MoveItem(ctx, id, statusID, endOfLane)
}

// Archive hides an item and its whole subtree, then re-densifies the container
// (lane or parent) it left so positions stay contiguous.
func (s *Service) Archive(ctx context.Context, id string) error {
	item, err := s.store.ItemByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.archiveSubtree(ctx, id); err != nil {
		return err
	}
	if item.ParentID == "" {
		return s.densifyLane(ctx, item.StatusID)
	}
	return s.densifyChildren(ctx, item.ParentID)
}

func (s *Service) archiveSubtree(ctx context.Context, id string) error {
	if err := s.store.ArchiveItem(ctx, id); err != nil {
		return err
	}
	kids, err := s.store.ChildrenByParent(ctx, id, false)
	if err != nil {
		return err
	}
	for _, k := range kids {
		if err := s.archiveSubtree(ctx, k.ID); err != nil {
			return err
		}
	}
	return nil
}

// Unarchive restores an item and its subtree, putting the root back at the end
// of its container.
func (s *Service) Unarchive(ctx context.Context, id string) error {
	item, err := s.store.ItemByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.unarchiveSubtree(ctx, id); err != nil {
		return err
	}
	if item.ParentID == "" {
		return s.appendToLaneEnd(ctx, item.StatusID, id)
	}
	return s.appendToParentEnd(ctx, item.ParentID, id)
}

func (s *Service) unarchiveSubtree(ctx context.Context, id string) error {
	if err := s.store.UnarchiveItem(ctx, id); err != nil {
		return err
	}
	kids, err := s.store.ChildrenByParent(ctx, id, true) // all — they were cascade-archived
	if err != nil {
		return err
	}
	for _, k := range kids {
		if err := s.unarchiveSubtree(ctx, k.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ArchivedItems(ctx context.Context, workspaceID string) ([]store.Item, error) {
	return s.store.ArchivedItemsByWorkspace(ctx, workspaceID)
}

func (s *Service) SubtaskCounts(ctx context.Context, workspaceID, doneStatusID string) (map[string]store.SubtaskCount, error) {
	return s.store.SubtaskCountsByWorkspace(ctx, workspaceID, doneStatusID)
}

// densifyLane renumbers a lane's active top-level items to 0..n-1.
func (s *Service) densifyLane(ctx context.Context, statusID string) error {
	active, err := s.store.ItemsByStatus(ctx, statusID)
	if err != nil {
		return err
	}
	return s.store.ReorderItems(ctx, statusID, idsOf(active))
}

// densifyChildren renumbers a parent's active children to 0..n-1.
func (s *Service) densifyChildren(ctx context.Context, parentID string) error {
	active, err := s.store.ChildrenByParent(ctx, parentID, false)
	if err != nil {
		return err
	}
	return s.store.SetItemPositions(ctx, idsOf(active))
}

func (s *Service) appendToLaneEnd(ctx context.Context, statusID, id string) error {
	active, err := s.store.ItemsByStatus(ctx, statusID)
	if err != nil {
		return err
	}
	return s.store.ReorderItems(ctx, statusID, appendLast(idsOf(active), id))
}

func (s *Service) appendToParentEnd(ctx context.Context, parentID, id string) error {
	active, err := s.store.ChildrenByParent(ctx, parentID, false)
	if err != nil {
		return err
	}
	return s.store.SetItemPositions(ctx, appendLast(idsOf(active), id))
}

func idsOf(items []store.Item) []string {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return ids
}

// appendLast returns ids with target removed (if present) then appended at the end.
func appendLast(ids []string, target string) []string {
	out := make([]string, 0, len(ids)+1)
	for _, id := range ids {
		if id != target {
			out = append(out, id)
		}
	}
	return append(out, target)
}

// --- subtasks ---

func (s *Service) CreateSubtask(ctx context.Context, parentID, title string) (store.Item, error) {
	title, err := cleanTitle(title)
	if err != nil {
		return store.Item{}, err
	}
	parent, err := s.store.ItemByID(ctx, parentID)
	if err != nil {
		return store.Item{}, err
	}
	statuses, err := s.store.StatusesByWorkspace(ctx, parent.WorkspaceID)
	if err != nil {
		return store.Item{}, err
	}
	if len(statuses) == 0 {
		return store.Item{}, ErrNoStatus
	}
	siblings, err := s.store.ChildrenByParent(ctx, parentID, false)
	if err != nil {
		return store.Item{}, err
	}
	return s.store.CreateItem(ctx, store.Item{
		WorkspaceID: parent.WorkspaceID,
		StatusID:    statuses[0].ID, // a fresh subtask starts in the first lane
		ParentID:    parentID,
		Title:       title,
		Position:    len(siblings),
	})
}

func (s *Service) Children(ctx context.Context, parentID string) ([]store.Item, error) {
	return s.store.ChildrenByParent(ctx, parentID, false)
}

func (s *Service) ReorderSubtasks(ctx context.Context, parentID string, orderedIDs []string) error {
	return s.store.SetItemPositions(ctx, orderedIDs)
}

// Reparent moves an item under newParentID — "" promotes it back to the board
// (top-level), an item id demotes it under that item. Parenting under itself or
// one of its own descendants would form a cycle and is refused. The item keeps
// its status and lands at the end of its new container.
func (s *Service) Reparent(ctx context.Context, itemID, newParentID string) error {
	item, err := s.store.ItemByID(ctx, itemID)
	if err != nil {
		return err
	}
	if newParentID == itemID {
		return ErrCycle
	}
	if newParentID != "" {
		parent, err := s.store.ItemByID(ctx, newParentID)
		if err != nil {
			return err
		}
		if parent.WorkspaceID != item.WorkspaceID {
			return ErrStatusMismatch
		}
		desc, err := s.descendants(ctx, itemID)
		if err != nil {
			return err
		}
		if desc[newParentID] {
			return ErrCycle
		}
	}
	if err := s.store.SetItemParent(ctx, itemID, newParentID); err != nil {
		return err
	}
	if item.ParentID == "" {
		if err := s.densifyLane(ctx, item.StatusID); err != nil {
			return err
		}
	} else if err := s.densifyChildren(ctx, item.ParentID); err != nil {
		return err
	}
	if newParentID == "" {
		return s.appendToLaneEnd(ctx, item.StatusID, itemID)
	}
	return s.appendToParentEnd(ctx, newParentID, itemID)
}

// descendants returns the set of item ids in itemID's subtree (excluding itself).
func (s *Service) descendants(ctx context.Context, itemID string) (map[string]bool, error) {
	out := map[string]bool{}
	queue := []string{itemID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		kids, err := s.store.ChildrenByParent(ctx, id, true)
		if err != nil {
			return nil, err
		}
		for _, k := range kids {
			if !out[k.ID] {
				out[k.ID] = true
				queue = append(queue, k.ID)
			}
		}
	}
	return out, nil
}

// CandidateParents lists the items an item may be reparented under: every active
// item except itself and its descendants.
func (s *Service) CandidateParents(ctx context.Context, workspaceID, itemID string) ([]store.Item, error) {
	all, err := s.store.AllItemsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	desc, err := s.descendants(ctx, itemID)
	if err != nil {
		return nil, err
	}
	out := make([]store.Item, 0, len(all))
	for _, it := range all {
		if it.ID == itemID || desc[it.ID] {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

// --- comments ---

func (s *Service) Comments(ctx context.Context, itemID string) ([]store.Comment, error) {
	return s.store.CommentsByItem(ctx, itemID)
}

func (s *Service) AddComment(ctx context.Context, itemID, authorID, body string) (store.Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" || len([]rune(body)) > MaxCommentLen {
		return store.Comment{}, ErrInvalidComment
	}
	return s.store.CreateComment(ctx, store.Comment{
		ItemID:   itemID,
		AuthorID: authorID,
		Body:     body,
	})
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
