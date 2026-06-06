package web

import (
	"net/http"

	"github.com/peios/acta/internal/store"
)

// viewOut is the JSON a created/updated view returns: enough for board.js to
// build the new tab. Query is the server-normalised form (the client sends the
// raw URL query; the server whitelists + canonicalises it).
type viewOut struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Query string `json:"query"`
}

func toViewOut(v store.BoardView) viewOut {
	return viewOut{ID: v.ID, Slug: v.Slug, Name: v.Name, Icon: v.Icon, Query: v.Query}
}

// viewCreate saves the current board filter as a named view. The board the view
// belongs to is sent as board_id (the board being viewed); query is that board
// URL's filter string. Both are validated against the resolved workspace.
func (h *handlers) viewCreate(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Name    string `json:"name"`
		Query   string `json:"query"`
		BoardID string `json:"board_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	b, err := h.board.BoardByID(r.Context(), req.BoardID)
	if err != nil || b.WorkspaceID != ws.ID {
		writeBoardErr(w, store.ErrBoardNotFound)
		return
	}
	me := principalFrom(r.Context())
	v, err := h.board.CreateBoardView(r.Context(), b.ID, req.Name, req.Query, me.ID)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toViewOut(v))
}

// viewReorder sets the strip order for a board from the given view ids.
func (h *handlers) viewReorder(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		BoardID string   `json:"board_id"`
		IDs     []string `json:"ids"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	b, err := h.board.BoardByID(r.Context(), req.BoardID)
	if err != nil || b.WorkspaceID != ws.ID {
		writeBoardErr(w, store.ErrBoardNotFound)
		return
	}
	if err := h.board.ReorderBoardViews(r.Context(), b.ID, req.IDs); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// viewSave overwrites a view's stored filter with the current board query —
// "save my filter changes to this view".
func (h *handlers) viewSave(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewInWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Query string `json:"query"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.UpdateBoardViewQuery(r.Context(), v.ID, req.Query); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// viewRename changes a view's display name.
func (h *handlers) viewRename(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewInWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.RenameBoardView(r.Context(), v.ID, req.Name); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// viewDelete removes a view (defaults included).
func (h *handlers) viewDelete(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewInWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.board.DeleteBoardView(r.Context(), v.ID); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// viewInWorkspace resolves the {id} path value to a view and confirms it belongs
// to the request's workspace, writing the error response on any miss.
func (h *handlers) viewInWorkspace(w http.ResponseWriter, r *http.Request) (store.BoardView, bool) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return store.BoardView{}, false
	}
	v, err := h.board.BoardView(r.Context(), r.PathValue("id"))
	if err != nil || v.WorkspaceID != ws.ID {
		writeBoardErr(w, store.ErrBoardViewNotFound)
		return store.BoardView{}, false
	}
	return v, true
}
