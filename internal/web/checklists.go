package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
)

// --- JSON shapes shared by the gating modal, the Pending band, and moves ---

type factJSON struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Checked bool   `json:"checked"`
}

// gateJSON is a pending transition's target lane and its checklist state.
type gateJSON struct {
	StatusID    string     `json:"status_id"`
	StatusName  string     `json:"status_name"`
	StatusColor string     `json:"status_color"`
	Facts       []factJSON `json:"facts"`
}

// moveResultJSON is the reply to any gated move/tick: Moved true means the item
// changed lane; otherwise Gate carries the checklist still to satisfy.
type moveResultJSON struct {
	Moved bool      `json:"moved"`
	Gate  *gateJSON `json:"gate,omitempty"`
}

func gateJSONFrom(g *board.PendingGate) *gateJSON {
	if g == nil {
		return nil
	}
	out := &gateJSON{StatusID: g.StatusID, StatusName: g.StatusName, StatusColor: g.StatusColor}
	for _, f := range g.Facts {
		out.Facts = append(out.Facts, factJSON{ID: f.ID, Title: f.Title, Checked: f.Checked})
	}
	return out
}

func moveResultFrom(o board.MoveOutcome) moveResultJSON {
	return moveResultJSON{Moved: o.Moved, Gate: gateJSONFrom(o.Gate)}
}

// --- Manage Checklist editor (which facts gate a lane) ---

// checklistFactJSON is one row of the Manage Checklist editor: a workspace fact
// and whether it currently gates the status being edited.
type checklistFactJSON struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Gates bool   `json:"gates"`
}

// statusInWorkspace resolves a path status id and confirms it belongs to ws.
func (h *handlers) statusInWorkspace(ctx context.Context, ws store.Workspace, statusID string) (store.Status, bool) {
	st, err := h.board.StatusByID(ctx, statusID)
	if err != nil || st.WorkspaceID != ws.ID {
		return store.Status{}, false
	}
	return st, true
}

// statusChecklist returns the whole workspace fact vocabulary, flagging which
// facts gate this status — the Manage Checklist editor's state.
func (h *handlers) statusChecklist(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	st, ok := h.statusInWorkspace(r.Context(), ws, r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.writeChecklistEditor(w, r, ws, st.ID)
}

// statusChecklistSave creates any new facts, then replaces the set gating this
// status with the chosen facts (existing + newly created), in the given order.
func (h *handlers) statusChecklistSave(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	st, ok := h.statusInWorkspace(r.Context(), ws, r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	var req struct {
		GateIDs   []int64  `json:"gate_ids"`
		NewTitles []string `json:"new_titles"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ids := req.GateIDs
	for _, t := range req.NewTitles {
		if strings.TrimSpace(t) == "" {
			continue
		}
		f, err := h.board.CreateFact(r.Context(), ws.ID, t)
		if err == store.ErrFactTitleTaken {
			// A fact by this title already exists — reuse it rather than erroring.
			if ex, ok := h.factByTitle(r.Context(), ws.ID, t); ok {
				ids = append(ids, ex.ID)
			}
			continue
		}
		if err != nil {
			writeBoardErr(w, err)
			return
		}
		ids = append(ids, f.ID)
	}
	if err := h.board.SetStatusFacts(r.Context(), st.ID, dedupeInt64(ids)); err != nil {
		writeBoardErr(w, err)
		return
	}
	h.writeChecklistEditor(w, r, ws, st.ID)
}

func (h *handlers) writeChecklistEditor(w http.ResponseWriter, r *http.Request, ws store.Workspace, statusID string) {
	vocab, err := h.board.WorkspaceFacts(r.Context(), ws.ID)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	gating, err := h.board.StatusFacts(r.Context(), statusID)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	gates := make(map[int64]bool, len(gating))
	order := make(map[int64]int, len(gating))
	for i, f := range gating {
		gates[f.ID] = true
		order[f.ID] = i
	}
	rows := make([]checklistFactJSON, 0, len(vocab))
	for _, f := range vocab {
		rows = append(rows, checklistFactJSON{ID: f.ID, Title: f.Title, Gates: gates[f.ID]})
	}
	// Gating facts first, in their gate order, so the editor reads as the lane's
	// checklist with the rest of the vocabulary beneath.
	stableSortChecklist(rows, order)
	writeJSON(w, http.StatusOK, struct {
		Facts []checklistFactJSON `json:"facts"`
	}{Facts: rows})
}

func (h *handlers) factByTitle(ctx context.Context, workspaceID, title string) (store.Fact, bool) {
	vocab, err := h.board.WorkspaceFacts(ctx, workspaceID)
	if err != nil {
		return store.Fact{}, false
	}
	for _, f := range vocab {
		if strings.EqualFold(f.Title, strings.TrimSpace(title)) {
			return f, true
		}
	}
	return store.Fact{}, false
}

// --- item ticks + pending transition ---

// itemFactToggle ticks or unticks one of an item's facts; if that completes a
// pending checklist the item moves (reflected in the reply's Moved).
func (h *handlers) itemFactToggle(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	factID, err := strconv.ParseInt(r.PathValue("fact"), 10, 64)
	if err != nil {
		http.Error(w, "bad fact id", http.StatusBadRequest)
		return
	}
	var req struct {
		Checked bool `json:"checked"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	out, err := h.board.SetItemFact(r.Context(), r.PathValue("id"), factID, req.Checked)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	if out.Moved {
		h.liveUpsert(r, r.PathValue("id"))
	}
	writeJSON(w, http.StatusOK, moveResultFrom(out))
}

// itemPendingForce overrides a pending checklist and moves the item now.
func (h *handlers) itemPendingForce(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	out, err := h.board.ForceStatus(r.Context(), r.PathValue("id"))
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	h.liveUpsert(r, r.PathValue("id"))
	writeJSON(w, http.StatusOK, moveResultFrom(out))
}

// itemPendingCancel drops a pending transition (the ticks persist).
func (h *handlers) itemPendingCancel(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	if err := h.board.CancelPending(r.Context(), r.PathValue("id")); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func dedupeInt64(xs []int64) []int64 {
	seen := make(map[int64]bool, len(xs))
	out := xs[:0]
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// stableSortChecklist orders gating facts first (by their gate position), then
// the rest of the vocabulary in its existing order.
func stableSortChecklist(rows []checklistFactJSON, gateOrder map[int64]int) {
	rank := func(r checklistFactJSON) int {
		if p, ok := gateOrder[r.ID]; ok {
			return p
		}
		return len(gateOrder) + 1
	}
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rank(rows[j]) < rank(rows[j-1]); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}
