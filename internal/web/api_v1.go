package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
)

// itemAPI is an item as the JSON API presents it: human-meaningful names rather
// than internal ids for status and principals (a CLI/agent thinks in "Doing"
// and "jack/deploy-bot", not uuids). Items themselves stay id-addressed.
type itemAPI struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Assignee  string    `json:"assignee,omitempty"`
	Milestone bool      `json:"milestone,omitempty"`
	CreatedBy string    `json:"created_by,omitempty"`
	ParentID  string    `json:"parent_id,omitempty"`
	Subtasks  []itemAPI `json:"subtasks,omitempty"` // populated by the item-show endpoint
	// Direct-subtask progress, populated by the board listing (done = last lane).
	SubtasksDone  int `json:"subtasks_done,omitempty"`
	SubtasksTotal int `json:"subtasks_total,omitempty"`
}

type workspaceAPI struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func (h *handlers) apiWorkspaces(w http.ResponseWriter, r *http.Request) {
	list, err := h.workspaces.List(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]workspaceAPI, len(list))
	for i, ws := range list {
		out[i] = workspaceAPI{Slug: ws.Slug, Name: ws.Name}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) apiListItems(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.apiWorkspace(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	items, err := h.board.Items(ctx, ws.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	statuses, err := h.board.Statuses(ctx, ws.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	users, err := h.board.Users(ctx)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	statusName := make(map[string]string, len(statuses))
	for _, s := range statuses {
		statusName[s.ID] = s.Name
	}
	userName := make(map[string]string, len(users))
	for _, u := range users {
		userName[u.ID] = u.Username
	}
	// "Done" is the last lane, matching the board's progress badge.
	doneStatusID := ""
	if len(statuses) > 0 {
		doneStatusID = statuses[len(statuses)-1].ID
	}
	counts, err := h.board.SubtaskCounts(ctx, ws.ID, doneStatusID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]itemAPI, len(items))
	for i, it := range items {
		v := toItemAPI(it, statusName, userName)
		if c, ok := counts[it.ID]; ok && c.Total > 0 {
			v.SubtasksDone, v.SubtasksTotal = c.Done, c.Total
		}
		out[i] = v
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) apiCreateItem(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.apiWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	if !readAPIJSON(w, r, &req) {
		return
	}
	statusID, ok := h.resolveStatus(w, r.Context(), ws, req.Status) // "" -> first lane
	if !ok {
		return
	}
	p := principalFrom(r.Context())
	it, err := h.board.CreateRootItemAs(r.Context(), ws.ID, statusID, req.Title, p.ID)
	if err != nil {
		apiBoardErr(w, err)
		return
	}
	h.writeItem(w, r.Context(), ws, it, http.StatusCreated)
}

func (h *handlers) apiItem(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.apiWorkspace(w, r)
	if !ok {
		return
	}
	item, err := h.board.Item(r.Context(), strings.ToLower(r.PathValue("id")))
	if errors.Is(err, store.ErrItemNotFound) || (err == nil && item.WorkspaceID != ws.ID) {
		apiError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	children, err := h.board.Children(r.Context(), item.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	statusName, userName, err := h.nameMaps(r.Context(), ws)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	view := toItemAPI(item, statusName, userName)
	for _, c := range children {
		view.Subtasks = append(view.Subtasks, toItemAPI(c, statusName, userName))
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *handlers) apiCreateSubtask(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.apiWorkspace(w, r)
	if !ok {
		return
	}
	parent, err := h.board.Item(r.Context(), strings.ToLower(r.PathValue("id")))
	if errors.Is(err, store.ErrItemNotFound) || (err == nil && parent.WorkspaceID != ws.ID) {
		apiError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if !readAPIJSON(w, r, &req) {
		return
	}
	p := principalFrom(r.Context())
	it, err := h.board.CreateSubtaskAs(r.Context(), parent.ID, req.Title, p.ID)
	if err != nil {
		apiBoardErr(w, err)
		return
	}
	h.writeItem(w, r.Context(), ws, it, http.StatusCreated)
}

func (h *handlers) apiTransition(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.apiWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !readAPIJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Status) == "" {
		apiError(w, http.StatusBadRequest, "status required")
		return
	}
	item, err := h.board.Item(r.Context(), strings.ToLower(r.PathValue("id")))
	if errors.Is(err, store.ErrItemNotFound) || (err == nil && item.WorkspaceID != ws.ID) {
		apiError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	statusID, ok := h.resolveStatus(w, r.Context(), ws, req.Status)
	if !ok {
		return
	}
	if err := h.board.SetStatus(r.Context(), item.ID, statusID); err != nil {
		apiBoardErr(w, err)
		return
	}
	updated, err := h.board.Item(r.Context(), item.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.writeItem(w, r.Context(), ws, updated, http.StatusOK)
}

// --- helpers ---

// apiWorkspace resolves the {slug} path value, writing a JSON 404 if missing.
func (h *handlers) apiWorkspace(w http.ResponseWriter, r *http.Request) (store.Workspace, bool) {
	ws, err := h.workspaces.BySlug(r.Context(), strings.ToLower(r.PathValue("slug")))
	if errors.Is(err, store.ErrWorkspaceNotFound) {
		apiError(w, http.StatusNotFound, "workspace not found")
		return store.Workspace{}, false
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return store.Workspace{}, false
	}
	return ws, true
}

// resolveStatus maps a status name to its id within ws. An empty name yields
// ("", true) — the caller decides what that means (create defaults to the first
// lane). An unknown name writes a 400 and returns ok=false.
func (h *handlers) resolveStatus(w http.ResponseWriter, ctx context.Context, ws store.Workspace, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", true
	}
	statuses, err := h.board.Statuses(ctx, ws.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return "", false
	}
	for _, s := range statuses {
		if strings.EqualFold(s.Name, name) {
			return s.ID, true
		}
	}
	apiError(w, http.StatusBadRequest, "unknown status: "+name)
	return "", false
}

func (h *handlers) nameMaps(ctx context.Context, ws store.Workspace) (statusName, userName map[string]string, err error) {
	statuses, err := h.board.Statuses(ctx, ws.ID)
	if err != nil {
		return nil, nil, err
	}
	users, err := h.board.Users(ctx)
	if err != nil {
		return nil, nil, err
	}
	statusName = make(map[string]string, len(statuses))
	for _, s := range statuses {
		statusName[s.ID] = s.Name
	}
	userName = make(map[string]string, len(users))
	for _, u := range users {
		userName[u.ID] = u.Username
	}
	return statusName, userName, nil
}

func (h *handlers) writeItem(w http.ResponseWriter, ctx context.Context, ws store.Workspace, it store.Item, status int) {
	statusName, userName, err := h.nameMaps(ctx, ws)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, status, toItemAPI(it, statusName, userName))
}

func toItemAPI(it store.Item, statusName, userName map[string]string) itemAPI {
	return itemAPI{
		ID:        it.ID,
		Title:     it.Title,
		Status:    statusName[it.StatusID],
		Assignee:  userName[it.AssigneeID],
		Milestone: it.IsMilestone,
		CreatedBy: userName[it.CreatedBy],
		ParentID:  it.ParentID,
	}
}

func readAPIJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

// apiBoardErr maps a board/store error to a JSON error response.
func apiBoardErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, board.ErrInvalidTitle):
		apiError(w, http.StatusBadRequest, "invalid title")
	case errors.Is(err, board.ErrNoStatus):
		apiError(w, http.StatusBadRequest, "workspace has no statuses")
	case errors.Is(err, board.ErrStatusMismatch), errors.Is(err, store.ErrStatusNotFound):
		apiError(w, http.StatusBadRequest, "invalid status")
	case errors.Is(err, store.ErrItemNotFound):
		apiError(w, http.StatusNotFound, "item not found")
	default:
		apiError(w, http.StatusInternalServerError, "internal error")
	}
}
