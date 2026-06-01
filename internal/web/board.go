package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/httpx"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

// --- the board page ---

type boardData struct {
	chrome
	Principal *identity.Principal
	Lanes     []lane
}

type lane struct {
	Status store.Status
	Items  []store.Item
}

// boardPage renders a workspace's board: its statuses (lanes) and the items in
// each. The initial state is server-rendered so it works without JavaScript;
// board.js then layers on drag-and-drop and inline create/edit.
func (h *handlers) boardPage(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	httpx.SetWorkspaceCookie(w, ws.Slug, h.secure)

	statuses, err := h.board.Statuses(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	items, err := h.board.Items(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	ch, err := h.chromeFor(r, "home", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "board.html", boardData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Lanes:     groupLanes(statuses, items),
	})
}

// groupLanes buckets items under their status. items arrives ordered by
// position, so each lane's slice stays in position order.
func groupLanes(statuses []store.Status, items []store.Item) []lane {
	byStatus := map[string][]store.Item{}
	for _, it := range items {
		byStatus[it.StatusID] = append(byStatus[it.StatusID], it)
	}
	lanes := make([]lane, len(statuses))
	for i, st := range statuses {
		lanes[i] = lane{Status: st, Items: byStatus[st.ID]}
	}
	return lanes
}

// --- JSON API (consumed by board.js, and by automation later) ---

type statusDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

type itemDTO struct {
	ID       string `json:"id"`
	StatusID string `json:"status_id"`
	Title    string `json:"title"`
	Position int    `json:"position"`
}

func (h *handlers) statusCreate(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	st, err := h.board.CreateStatus(r.Context(), ws.ID, req.Name)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusDTO{ID: st.ID, Name: st.Name, Position: st.Position})
}

func (h *handlers) statusRename(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.RenameStatus(r.Context(), r.PathValue("id"), req.Name); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) statusReorder(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.ReorderStatuses(r.Context(), ws.ID, req.IDs); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) statusDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	if err := h.board.DeleteStatus(r.Context(), r.PathValue("id")); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) itemCreate(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		StatusID string `json:"status_id"`
		Title    string `json:"title"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	it, err := h.board.CreateItem(r.Context(), ws.ID, req.StatusID, req.Title)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, itemDTO{ID: it.ID, StatusID: it.StatusID, Title: it.Title, Position: it.Position})
}

func (h *handlers) itemRename(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.RenameItem(r.Context(), r.PathValue("id"), req.Title); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) itemMove(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		StatusID string `json:"status_id"`
		Index    int    `json:"index"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.MoveItem(r.Context(), r.PathValue("id"), req.StatusID, req.Index); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) itemDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	if err := h.board.DeleteItem(r.Context(), r.PathValue("id")); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

// resolveWorkspace looks up the workspace named in the {slug} path value,
// writing a 404 (or 500) and returning ok=false if it can't be served.
func (h *handlers) resolveWorkspace(w http.ResponseWriter, r *http.Request) (store.Workspace, bool) {
	ws, err := h.workspaces.BySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, store.ErrWorkspaceNotFound) {
		http.NotFound(w, r)
		return store.Workspace{}, false
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return store.Workspace{}, false
	}
	return ws, true
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeBoardErr maps a board/store error to an HTTP status plus a small JSON
// {"error": code} body the client can branch on.
func writeBoardErr(w http.ResponseWriter, err error) {
	type body struct {
		Error string `json:"error"`
	}
	switch {
	case errors.Is(err, board.ErrInvalidName):
		writeJSON(w, http.StatusBadRequest, body{"invalid_name"})
	case errors.Is(err, board.ErrInvalidTitle):
		writeJSON(w, http.StatusBadRequest, body{"invalid_title"})
	case errors.Is(err, board.ErrStatusNotEmpty):
		writeJSON(w, http.StatusConflict, body{"status_not_empty"})
	case errors.Is(err, board.ErrStatusMismatch):
		writeJSON(w, http.StatusBadRequest, body{"status_mismatch"})
	case errors.Is(err, store.ErrStatusNotFound):
		writeJSON(w, http.StatusNotFound, body{"status_not_found"})
	case errors.Is(err, store.ErrItemNotFound):
		writeJSON(w, http.StatusNotFound, body{"item_not_found"})
	default:
		writeJSON(w, http.StatusInternalServerError, body{"internal"})
	}
}
