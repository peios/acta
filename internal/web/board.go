package web

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
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
	Principal        *identity.Principal
	Mode             string   // "status" or "milestone"
	Lanes            []lane   // status mode
	Palette          []swatch // lane-colour options for the header picker
	MilestoneColumns []milestoneColumn
	StatusFilter     []statusOpt   // the status facet options
	StatusSelected   int           // count badge on the Status trigger
	Assignees        assigneeFacet // the assignee facet (hierarchical)
	AssigneeSelected int           // count badge on the Assignee trigger
	FilterActive     bool          // any facet currently narrowing the board
	Modal            *modalView    // set when ?item=<id> resolves within this workspace
}

// swatch is one option in the lane-colour picker: its hex (sent back on pick)
// and a pre-built, template-safe background declaration.
type swatch struct {
	Hex   string
	Style template.CSS
}

func palette() []swatch {
	out := make([]swatch, len(board.Palette))
	for i, hex := range board.Palette {
		out[i] = swatch{Hex: hex, Style: template.CSS("background:" + hex)}
	}
	return out
}

type lane struct {
	Status store.Status
	Color  string
	Hidden bool // filtered out (its status is deselected) — kept in the DOM, CSS-hidden
	Cards  []cardView
}

// ColorVar is the lane's colour as a template-safe `--lane-color` declaration
// for the header dot. The value is always a palette hex (explicit or derived),
// so wrapping it as trusted CSS is safe.
func (l lane) ColorVar() template.CSS { return colorVar(l.Color) }

// milestoneColumn is one column of Milestone mode: the Backlog (ID "") or a
// root milestone (ID = its item id) holding that milestone's children.
type milestoneColumn struct {
	ID    string
	Title string
	Color string // the milestone's own status colour, tinting its ◆ (Backlog: "")
	Cards []cardView
}

// ColorVar is the milestone's status colour as a template-safe `--lane-color`
// declaration for its header diamond.
func (m milestoneColumn) ColorVar() template.CSS { return colorVar(m.Color) }

type cardView struct {
	Item       store.Item
	Subtasks   store.SubtaskCount
	StatusName string // the card's status name (hover tooltip / accessible label)
	Color      string // the card's lane colour, for the left bar
	Hidden     bool   // filtered out by status/assignee — kept in the DOM, CSS-hidden
}

// ColorVar is the card's lane colour as a template-safe `--lane-color`
// declaration driving the left bar; see lane.ColorVar.
func (c cardView) ColorVar() template.CSS { return colorVar(c.Color) }

// colorVar wraps a palette hex as a trusted `--lane-color` CSS declaration.
// Values are always palette members (explicit choices are validated server-side
// and auto colours come from the palette), so they're safe to emit verbatim.
func colorVar(hex string) template.CSS {
	if hex == "" {
		return ""
	}
	return template.CSS("--lane-color:" + hex)
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
	doneStatusID := ""
	if len(statuses) > 0 {
		doneStatusID = statuses[len(statuses)-1].ID // "done" = the last lane
	}
	counts, err := h.board.SubtaskCounts(r.Context(), ws.ID, doneStatusID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	mode := "status"
	if r.URL.Query().Get("mode") == "milestone" {
		mode = "milestone"
	}

	me := principalFrom(r.Context())
	filter := newBoardFilter(r.URL.Query()["status"], r.URL.Query()["assignee"], me.ID)
	users, err := h.board.Users(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data := boardData{
		chrome:           ch,
		Principal:        me,
		Mode:             mode,
		Palette:          palette(),
		StatusFilter:     statusFacet(statuses, filter),
		StatusSelected:   len(filter.statuses),
		Assignees:        assigneeFacetFrom(users, filter),
		AssigneeSelected: len(filter.assignees),
		FilterActive:     filter.active(),
	}
	if mode == "milestone" {
		cols, err := h.milestoneColumns(r.Context(), items, statuses, counts, filter)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		data.MilestoneColumns = cols
	} else {
		data.Lanes = groupLanes(statuses, items, counts, filter)
	}
	// A ?item=<id> deep link opens that item's modal (server-rendered, so it
	// works on refresh and with JS off).
	if itemID := r.URL.Query().Get("item"); itemID != "" {
		mv, found, err := h.buildModal(r, ws, itemID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if found {
			data.Modal = &mv
		}
	}
	render(w, http.StatusOK, "board.html", data)
}

// milestoneColumns builds Milestone mode: a Backlog column of root
// non-milestones, then one column per root milestone holding its children.
func (h *handlers) milestoneColumns(ctx context.Context, roots []store.Item, statuses []store.Status, counts map[string]store.SubtaskCount, filter boardFilter) ([]milestoneColumn, error) {
	statusByID := make(map[string]store.Status, len(statuses))
	for _, s := range statuses {
		statusByID[s.ID] = s
	}
	card := func(it store.Item) cardView {
		st := statusByID[it.StatusID]
		return cardView{Item: it, Subtasks: counts[it.ID], StatusName: st.Name, Color: board.ColorFor(st), Hidden: filter.cardHidden(it)}
	}
	backlog := milestoneColumn{Title: "Backlog"}
	var msCols []milestoneColumn
	for _, it := range roots {
		if !it.IsMilestone {
			backlog.Cards = append(backlog.Cards, card(it))
			continue
		}
		kids, err := h.board.Children(ctx, it.ID)
		if err != nil {
			return nil, err
		}
		col := milestoneColumn{ID: it.ID, Title: it.Title, Color: board.ColorFor(statusByID[it.StatusID])}
		for _, k := range kids {
			col.Cards = append(col.Cards, card(k))
		}
		msCols = append(msCols, col)
	}
	return append([]milestoneColumn{backlog}, msCols...), nil
}

// groupLanes buckets items under their status, attaching each item's subtask
// progress. items arrives ordered by position, so each lane stays in order.
func groupLanes(statuses []store.Status, items []store.Item, counts map[string]store.SubtaskCount, filter boardFilter) []lane {
	byID := make(map[string]store.Status, len(statuses))
	for _, st := range statuses {
		byID[st.ID] = st
	}
	byStatus := map[string][]cardView{}
	for _, it := range items {
		st := byID[it.StatusID]
		byStatus[it.StatusID] = append(byStatus[it.StatusID], cardView{
			Item: it, Subtasks: counts[it.ID], StatusName: st.Name, Color: board.ColorFor(st), Hidden: filter.cardHidden(it),
		})
	}
	lanes := make([]lane, len(statuses))
	for i, st := range statuses {
		lanes[i] = lane{Status: st, Color: board.ColorFor(st), Hidden: !filter.statusVisible(st.ID), Cards: byStatus[st.ID]}
	}
	return lanes
}

// --- JSON API (consumed by board.js, and by automation later) ---

type statusDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Color    string `json:"color"` // resolved display colour (never empty)
}

type itemDTO struct {
	ID       string `json:"id"`
	StatusID string `json:"status_id"`
	Title    string `json:"title"`
	Position int    `json:"position"`
	Color    string `json:"color"` // resolved lane colour, so the client can paint the new card
}

// itemDTOFor builds the response for a freshly created item, resolving its lane
// colour so board.js can render the card's left bar without a reload.
func (h *handlers) itemDTOFor(ctx context.Context, it store.Item) itemDTO {
	dto := itemDTO{ID: it.ID, StatusID: it.StatusID, Title: it.Title, Position: it.Position}
	if st, err := h.board.StatusByID(ctx, it.StatusID); err == nil {
		dto.Color = board.ColorFor(st)
	}
	return dto
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
	writeJSON(w, http.StatusOK, statusDTO{ID: st.ID, Name: st.Name, Position: st.Position, Color: board.ColorFor(st)})
}

func (h *handlers) statusColor(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		Color string `json:"color"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.SetStatusColor(r.Context(), r.PathValue("id"), req.Color); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	it, err := h.board.CreateRootItemAs(r.Context(), ws.ID, req.StatusID, req.Title, principalFrom(r.Context()).ID)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.itemDTOFor(r.Context(), it))
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
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.board.DeleteItem(r.Context(), r.PathValue("id")); err != nil {
		writeBoardErr(w, err)
		return
	}
	respond204OrRedirect(w, r, "/w/"+ws.Slug+"/archive")
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
	case errors.Is(err, board.ErrInvalidDescription):
		writeJSON(w, http.StatusBadRequest, body{"invalid_description"})
	case errors.Is(err, board.ErrInvalidComment):
		writeJSON(w, http.StatusBadRequest, body{"invalid_comment"})
	case errors.Is(err, board.ErrStatusNotEmpty):
		writeJSON(w, http.StatusConflict, body{"status_not_empty"})
	case errors.Is(err, board.ErrNoStatus):
		writeJSON(w, http.StatusConflict, body{"no_status"})
	case errors.Is(err, board.ErrCycle):
		writeJSON(w, http.StatusConflict, body{"cycle"})
	case errors.Is(err, board.ErrStatusMismatch):
		writeJSON(w, http.StatusBadRequest, body{"status_mismatch"})
	case errors.Is(err, board.ErrInvalidColor):
		writeJSON(w, http.StatusBadRequest, body{"invalid_color"})
	case errors.Is(err, store.ErrStatusNotFound):
		writeJSON(w, http.StatusNotFound, body{"status_not_found"})
	case errors.Is(err, store.ErrItemNotFound):
		writeJSON(w, http.StatusNotFound, body{"item_not_found"})
	case errors.Is(err, store.ErrUserNotFound):
		writeJSON(w, http.StatusBadRequest, body{"user_not_found"})
	default:
		writeJSON(w, http.StatusInternalServerError, body{"internal"})
	}
}
