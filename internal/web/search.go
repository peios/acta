package web

import (
	"net/http"
	"strings"
)

// searchResults backs the Cmd-K quick-switcher: an HTML fragment of items whose
// title or description matches ?q=, across every board — Backlog included,
// because this is the human's own switcher, not the agent's deliberately-scoped
// list_items. Session-authed; returns a handful of relevance-ranked hits.
func (h *handlers) searchResults(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	type hit struct{ Ref, Title, Board, URL string }
	data := struct {
		Query string
		Items []hit
	}{Query: strings.TrimSpace(r.URL.Query().Get("q"))}
	if data.Query == "" {
		renderSearchResults(w, data)
		return
	}

	ctx := r.Context()
	items, err := h.board.SearchItems(ctx, ws.ID, "", data.Query, false)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Label each hit with the board it lives on (its status's board) so a Backlog
	// result reads as such.
	boards, err := h.board.Boards(ctx, ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	boardName := make(map[string]string, len(boards))
	for _, b := range boards {
		boardName[b.ID] = b.Name
	}
	statuses, err := h.board.Statuses(ctx, ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	statusBoard := make(map[string]string, len(statuses))
	for _, s := range statuses {
		statusBoard[s.ID] = boardName[s.BoardID]
	}

	const maxHits = 15
	for _, it := range items {
		if len(data.Items) >= maxHits {
			break
		}
		data.Items = append(data.Items, hit{
			Ref:   refID(ws.ItemPrefix, it.RefNum),
			Title: it.Title,
			Board: statusBoard[it.StatusID],
			URL:   "/" + ws.Slug + "?item=" + it.ID,
		})
	}
	renderSearchResults(w, data)
}
