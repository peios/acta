package web

import (
	"errors"
	"net/http"
	"time"

	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

// --- the item modal ---

type modalView struct {
	Slug           string
	CSRFToken      string
	Item           store.Item
	Desc           descView // the rendered, collapsible description
	Statuses       []store.Status
	Assignables    []store.User // assignee-picker options: humans + your agents (+ current assignee)
	Assignee       string       // display name of the assignee, "" if unassigned
	Comments       []commentView
	Archived       bool
	ParentID       string // "" if this is a top-level item
	ParentTitle    string
	Parents        []parentOption // candidates this item may be reparented under
	Children       []childView
	SubDone        int
	SubTotal       int
	CreatedBy      string // display name of the creator, "" if unrecorded
	CreatedByAgent bool
	History        []eventView // activity log for this item, newest first
}

type parentOption struct {
	ID    string
	Title string
}

// containsUser reports whether a user with the given id is in us.
func containsUser(us []store.User, id string) bool {
	for _, u := range us {
		if u.ID == id {
			return true
		}
	}
	return false
}

type commentView struct {
	Author string
	Body   string
	At     string
}

type childView struct {
	ID         string
	Title      string
	StatusName string
}

// buildModal assembles the modal view for an item, resolving the assignee and
// comment authors to display names. found is false (no error) when the item
// doesn't exist or belongs to another workspace — ?item= is scoped to the
// workspace whose page you're on.
func (h *handlers) buildModal(r *http.Request, ws store.Workspace, itemID string) (modalView, bool, error) {
	ctx := r.Context()
	item, err := h.board.Item(ctx, itemID)
	if errors.Is(err, store.ErrItemNotFound) {
		return modalView{}, false, nil
	}
	if err != nil {
		return modalView{}, false, err
	}
	if item.WorkspaceID != ws.ID {
		return modalView{}, false, nil
	}
	statuses, err := h.board.Statuses(ctx, ws.ID)
	if err != nil {
		return modalView{}, false, err
	}
	users, err := h.board.Users(ctx)
	if err != nil {
		return modalView{}, false, err
	}
	comments, err := h.board.Comments(ctx, itemID)
	if err != nil {
		return modalView{}, false, err
	}

	children, err := h.board.Children(ctx, item.ID)
	if err != nil {
		return modalView{}, false, err
	}

	nameByID := make(map[string]string, len(users))
	isAgent := make(map[string]bool, len(users))
	for _, u := range users {
		nameByID[u.ID] = u.Display
		isAgent[u.ID] = u.AgentOfID != ""
	}
	createdBy := ""
	if item.CreatedBy != "" {
		if createdBy = nameByID[item.CreatedBy]; createdBy == "" {
			createdBy = "Unknown"
		}
	}
	cvs := make([]commentView, len(comments))
	for i, c := range comments {
		author := nameByID[c.AuthorID]
		if author == "" {
			author = "Unknown"
		}
		cvs[i] = commentView{Author: author, Body: c.Body, At: formatWhen(c.CreatedAt)}
	}

	statusName := make(map[string]string, len(statuses))
	for _, st := range statuses {
		statusName[st.ID] = st.Name
	}
	lastStatusID := ""
	if len(statuses) > 0 {
		lastStatusID = statuses[len(statuses)-1].ID
	}
	kids := make([]childView, len(children))
	done := 0
	for i, c := range children {
		kids[i] = childView{ID: c.ID, Title: c.Title, StatusName: statusName[c.StatusID]}
		if c.StatusID == lastStatusID {
			done++
		}
	}
	parentTitle := ""
	if item.ParentID != "" {
		if p, perr := h.board.Item(ctx, item.ParentID); perr == nil {
			parentTitle = p.Title
		}
	}
	candidates, err := h.board.CandidateParents(ctx, ws.ID, item.ID)
	if err != nil {
		return modalView{}, false, err
	}
	parents := make([]parentOption, len(candidates))
	for i, c := range candidates {
		parents[i] = parentOption{ID: c.ID, Title: c.Title}
	}

	history, err := h.board.ItemHistory(ctx, item.ID, 50)
	if err != nil {
		return modalView{}, false, err
	}

	assignables, err := h.board.Assignables(ctx)
	if err != nil {
		return modalView{}, false, err
	}
	// Keep the current assignee selectable even when they're outside the
	// directable set (a legacy or cross-owner agent), so saving the modal can't
	// silently re-assign the item.
	if item.AssigneeID != "" && !containsUser(assignables, item.AssigneeID) {
		for _, u := range users {
			if u.ID == item.AssigneeID {
				assignables = append(assignables, u)
				break
			}
		}
	}

	return modalView{
		Slug:           ws.Slug,
		CSRFToken:      csrfTokenFrom(ctx),
		Item:           item,
		Desc:           renderDescription(item.Description),
		Statuses:       statuses,
		Assignables:    assignables,
		Assignee:       nameByID[item.AssigneeID],
		Comments:       cvs,
		Archived:       item.ArchivedAt != nil,
		ParentID:       item.ParentID,
		ParentTitle:    parentTitle,
		Parents:        parents,
		Children:       kids,
		SubDone:        done,
		SubTotal:       len(children),
		CreatedBy:      createdBy,
		CreatedByAgent: isAgent[item.CreatedBy],
		History:        toEventViews(history),
	}, true, nil
}

// itemModal returns just the modal markup, for board.js to open without a
// full page reload.
func (h *handlers) itemModal(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	mv, found, err := h.buildModal(r, ws, r.PathValue("id"))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	renderItemModal(w, mv)
}

// --- item field mutations (JSON, from the modal) ---

func (h *handlers) itemDescription(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		Description string `json:"description"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.UpdateDescription(r.Context(), r.PathValue("id"), req.Description); err != nil {
		writeBoardErr(w, err)
		return
	}
	// Return the freshly rendered, collapsible view so the modal can swap it in
	// without a reload when the editor closes.
	renderDescView(w, renderDescription(req.Description))
}

func (h *handlers) itemAssignee(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		AssigneeID string `json:"assignee_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.SetAssignee(r.Context(), r.PathValue("id"), req.AssigneeID); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) itemSetStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		StatusID string `json:"status_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.SetStatus(r.Context(), r.PathValue("id"), req.StatusID); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) itemComment(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	p := principalFrom(r.Context())
	c, err := h.board.AddComment(r.Context(), r.PathValue("id"), p.ID, req.Body)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Author string `json:"author"`
		Body   string `json:"body"`
		At     string `json:"at"`
	}{p.Display, c.Body, formatWhen(c.CreatedAt)})
}

// --- subtasks ---

func (h *handlers) subtaskCreate(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	it, err := h.board.CreateSubtaskAs(r.Context(), r.PathValue("id"), req.Title, principalFrom(r.Context()).ID)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.itemDTOFor(r.Context(), it))
}

func (h *handlers) itemMilestone(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		IsMilestone bool `json:"is_milestone"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.SetMilestone(r.Context(), r.PathValue("id"), req.IsMilestone); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) itemParent(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		ParentID string `json:"parent_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.Reparent(r.Context(), r.PathValue("id"), req.ParentID); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) subtaskReorder(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveWorkspace(w, r); !ok {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.board.ReorderSubtasks(r.Context(), r.PathValue("id"), req.IDs); err != nil {
		writeBoardErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- archive / unarchive (dual-mode: JSON from the board, form from archive) ---

func (h *handlers) itemArchive(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.board.Archive(r.Context(), r.PathValue("id")); err != nil {
		writeBoardErr(w, err)
		return
	}
	respond204OrRedirect(w, r, "/w/"+ws.Slug)
}

func (h *handlers) itemUnarchive(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.board.Unarchive(r.Context(), r.PathValue("id")); err != nil {
		writeBoardErr(w, err)
		return
	}
	respond204OrRedirect(w, r, "/w/"+ws.Slug+"/archive")
}

// --- archive view ---

type archiveData struct {
	chrome
	Principal *identity.Principal
	Items     []archivedItemView
}

type archivedItemView struct {
	ID         string
	Title      string
	StatusName string
	Archived   string
}

func (h *handlers) archivePage(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	items, err := h.board.ArchivedItems(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	statuses, err := h.board.Statuses(r.Context(), ws.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nameByID := make(map[string]string, len(statuses))
	for _, s := range statuses {
		nameByID[s.ID] = s.Name
	}
	views := make([]archivedItemView, len(items))
	for i, it := range items {
		at := ""
		if it.ArchivedAt != nil {
			at = formatWhen(*it.ArchivedAt)
		}
		views[i] = archivedItemView{ID: it.ID, Title: it.Title, StatusName: nameByID[it.StatusID], Archived: at}
	}
	ch, err := h.chromeFor(r, "home", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "archive.html", archiveData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Items:     views,
	})
}

// --- helpers ---

// respond204OrRedirect answers an AJAX call (which carries the CSRF token in a
// header) with 204, and a plain form submit (token in the body) with a 303 to
// redirect. Lets one endpoint serve both board.js and the no-JS archive forms.
func respond204OrRedirect(w http.ResponseWriter, r *http.Request, redirect string) {
	if r.Header.Get("X-CSRF-Token") != "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func formatWhen(t time.Time) string {
	return t.Format("2 Jan 2006, 15:04")
}
