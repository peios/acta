package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"maps"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

// --- the item modal ---

type modalView struct {
	Slug      string
	CSRFToken string
	Item      store.Item
	RefID     string   // human id, e.g. "ACTA-12"
	Desc      descView // the rendered, collapsible description
	// StatusGroups are the picker's options, grouped by board. Picking a status
	// on another board *is* how an item moves boards (promote/demote), so the
	// picker spans every board, not just the item's current one.
	StatusGroups    []statusGroup
	StatusName      string         // current status's display name (for the pill)
	StatusBoard     string         // current status's board name (shown beside the pill)
	StatusDashed    bool           // current status is a Backlog lane (dashed pill dot)
	StatusColorVar  template.CSS   // current status's --lane-color (for the pill dot)
	Assignables     []store.User   // assignee-picker options: humans + your agents (+ current assignee)
	Assignee        string         // display name of the assignee, "" if unassigned
	Projects        []modalProject // project-picker options (the workspace's active projects)
	ProjectName     string         // current project's name, "" if none
	ProjectColorVar template.CSS   // current project's --lane-color (for the pill dot)
	Archived        bool
	ParentID        string // "" if this is a top-level item
	ParentTitle     string
	Children        []childView
	SubDone         int
	SubTotal        int
	CreatedBy       string // display name of the creator, "" if unrecorded
	CreatedByAgent  bool
	Watching        bool            // the viewer is subscribed to this item
	WatchCats       []catToggle     // the five category toggles for the Watch dropdown, ticked per the filter
	Timeline        []timelineGroup // unified activity feed: comments + system events, oldest first
}

// statusChoice is one option in the modal's status pickers (the side <select>
// and the mobile pill); Color is the resolved lane hex so the pill's dot is
// coloured server-side, working regardless of the board's current view mode.
type statusChoice struct {
	ID     string
	Name   string
	Color  string
	Dashed bool // a Backlog-board lane — rendered as a dashed (unstarted) dot
}

func (s statusChoice) ColorVar() template.CSS { return colorVar(s.Color) }

// statusGroup is one board's lanes in the modal status picker, labelled by the
// board's name so the picker reads as two sections (Tasks / Backlog).
type statusGroup struct {
	Board   string
	Choices []statusChoice
}

// modalProject is one option in the modal's project picker: its id, name, and
// resolved colour (for the dropdown dot).
type modalProject struct {
	ID    string
	Name  string
	Color string
}

func (p modalProject) ColorVar() template.CSS { return colorVar(p.Color) }

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
	ID          string
	AuthorID    string
	Author      string
	AvatarStyle template.CSS
	AvatarText  string
	Body        string        // raw markdown source, for the inline editor (empty if deleted)
	BodyHTML    template.HTML // rendered, sanitized markdown (empty if deleted)
	Rel         string        // relative time ("2h ago")
	Abs         string        // absolute time, for the hover tooltip
	Mine        bool          // authored by the viewer (gates edit/delete affordances)
	Edited      bool          // has been edited at least once
	Deleted     bool          // soft-deleted — render a tombstone, no body
}

// timelineGroup is one render unit in the unified activity feed: either a
// comment card (Comment != nil) or a run of consecutive system events sharing a
// connecting rail (Events).
type timelineGroup struct {
	Comment *commentView
	Events  []eventView
}

// commentToView renders a stored comment for the feed: author display + avatar,
// markdown body, and a Mine flag when the viewer wrote it.
func commentToView(c store.Comment, nameByID map[string]string, viewerID string) commentView {
	author := nameByID[c.AuthorID]
	if author == "" {
		author = "Unknown"
	}
	cv := commentView{
		ID:          c.ID,
		AuthorID:    c.AuthorID,
		Author:      author,
		AvatarStyle: avatarStyle(c.AuthorID),
		AvatarText:  initials(author),
		Rel:         relativeWhen(c.CreatedAt),
		Abs:         formatWhen(c.CreatedAt),
		Mine:        viewerID != "" && c.AuthorID == viewerID,
		Edited:      c.EditedAt != nil,
		Deleted:     c.DeletedAt != nil,
	}
	// A tombstone carries no body — the deleted text never reaches the client.
	if c.DeletedAt == nil {
		cv.Body = c.Body
		cv.BodyHTML = mdToHTML(c.Body)
	}
	return cv
}

// buildTimeline merges an item's comments and system events into one
// chronological feed (oldest first). The comment.added events are dropped — the
// comment card stands in for them — and consecutive system events are folded
// into one group so the template can draw a single rail through them. events
// arrives newest-first (as the store returns it); comments oldest-first.
func buildTimeline(comments []store.Comment, events []store.Event, nameByID, colorByStatus map[string]string, viewerID, backlogBoardID string) []timelineGroup {
	type entry struct {
		when    time.Time
		comment *store.Comment
		event   *store.Event
	}
	entries := make([]entry, 0, len(comments)+len(events))
	for i := range comments {
		entries = append(entries, entry{when: comments[i].CreatedAt, comment: &comments[i]})
	}
	for i := range events {
		if events[i].Verb == store.EventCommentAdded {
			continue
		}
		entries = append(entries, entry{when: events[i].CreatedAt, event: &events[i]})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].when.Before(entries[j].when) })

	var feed []timelineGroup
	for _, e := range entries {
		if e.comment != nil {
			cv := commentToView(*e.comment, nameByID, viewerID)
			feed = append(feed, timelineGroup{Comment: &cv})
			continue
		}
		ev := eventToView(*e.event)
		setEventDot(&ev, e.event.Data, colorByStatus, e.event.BoardID, backlogBoardID)
		if n := len(feed); n > 0 && feed[n-1].Comment == nil {
			feed[n-1].Events = append(feed[n-1].Events, ev)
		} else {
			feed = append(feed, timelineGroup{Events: []eventView{ev}})
		}
	}
	return feed
}

type childView struct {
	ID         string
	Title      string
	StatusName string
}

// parseItemRef parses a human item reference: "PREFIX-N" (prefix + number, e.g.
// ACTA-12) or a bare "N". It returns the prefix ("" for a bare number), the
// number, and ok=false when the string isn't a reference at all.
func parseItemRef(s string) (prefix string, num int, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, false
	}
	if i := strings.LastIndexByte(s, '-'); i > 0 {
		p, rest := s[:i], s[i+1:]
		if n, err := strconv.Atoi(rest); err == nil && n > 0 && isAlnum(p) {
			return p, n, true
		}
		return "", 0, false
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return "", n, true
	}
	return "", 0, false
}

func isAlnum(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// resolveItem turns a ?item= / path value into an item within ws. It tries the
// opaque id first, then a human reference (PREFIX-N or bare N) scoped to the
// workspace — a present prefix must match this workspace's. Returns
// ErrItemNotFound when nothing matches.
func (h *handlers) resolveItem(ctx context.Context, ws store.Workspace, ref string) (store.Item, error) {
	ref = strings.TrimSpace(ref)
	switch it, err := h.board.Item(ctx, strings.ToLower(ref)); {
	case err == nil:
		return it, nil
	case !errors.Is(err, store.ErrItemNotFound):
		return store.Item{}, err
	}
	if prefix, num, ok := parseItemRef(ref); ok && (prefix == "" || strings.EqualFold(prefix, ws.ItemPrefix)) {
		return h.board.ItemByRef(ctx, ws.ID, num)
	}
	return store.Item{}, store.ErrItemNotFound
}

// buildModal assembles the modal view for an item, resolving the assignee and
// comment authors to display names. found is false (no error) when the item
// doesn't exist or belongs to another workspace — ?item= is scoped to the
// workspace whose page you're on.
func (h *handlers) buildModal(r *http.Request, ws store.Workspace, itemID string) (modalView, bool, error) {
	ctx := r.Context()
	item, err := h.resolveItem(ctx, ws, itemID)
	if errors.Is(err, store.ErrItemNotFound) {
		return modalView{}, false, nil
	}
	if err != nil {
		return modalView{}, false, err
	}
	if item.WorkspaceID != ws.ID {
		return modalView{}, false, nil
	}
	itemID = item.ID // the input may have been a human ref; use the opaque id below
	// The status picker spans every board, grouped by board: picking a status on
	// another board is how an item moves boards. The item's own board is resolved
	// separately for the done-lane (subtask progress counts its board's last lane).
	itemStatus, err := h.board.StatusByID(ctx, item.StatusID)
	if err != nil {
		return modalView{}, false, err
	}
	boardStatuses, err := h.board.BoardStatuses(ctx, itemStatus.BoardID)
	if err != nil {
		return modalView{}, false, err
	}
	boards, err := h.board.Boards(ctx, ws.ID)
	if err != nil {
		return modalView{}, false, err
	}
	// Lead the picker with the item's own board — that's where most status
	// changes stay — keeping the other boards in their normal order beneath.
	sort.SliceStable(boards, func(i, j int) bool {
		return boards[i].ID == itemStatus.BoardID && boards[j].ID != itemStatus.BoardID
	})
	users, err := h.board.Users(ctx)
	if err != nil {
		return modalView{}, false, err
	}
	comments, err := h.board.CommentsWithDeleted(ctx, itemID)
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
	// Build the status picker across every board (grouped), and alongside it the
	// flat lookups the rest of the modal needs: a name+colour by status id, and
	// the current status's name/colour/board for the pill.
	var statusGroups []statusGroup
	statusName := map[string]string{}
	colorByStatus := map[string]string{}
	var curStatusName, curStatusColor, curStatusBoard string
	var curStatusDashed bool
	for _, b := range boards {
		sts, serr := h.board.BoardStatuses(ctx, b.ID)
		if serr != nil {
			return modalView{}, false, serr
		}
		dashed := isBacklogBoard(b) // Backlog lanes render as dashed (unstarted) dots
		choices := make([]statusChoice, len(sts))
		for i, st := range sts {
			c := board.ColorFor(st)
			choices[i] = statusChoice{ID: st.ID, Name: st.Name, Color: c, Dashed: dashed}
			statusName[st.ID] = st.Name
			colorByStatus[st.Name] = c
			if st.ID == item.StatusID {
				curStatusName, curStatusColor, curStatusBoard, curStatusDashed = st.Name, c, b.Name, dashed
			}
		}
		statusGroups = append(statusGroups, statusGroup{Board: b.Name, Choices: choices})
	}
	// The done lane for subtask progress is the item's *own* board's last lane.
	lastStatusID := ""
	if n := len(boardStatuses); n > 0 {
		lastStatusID = boardStatuses[n-1].ID
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

	// Project picker: the workspace's active projects, plus the item's own project
	// when it's archived (so it stays shown/selectable rather than silently
	// reading as "No project").
	projects, err := h.board.Projects(ctx, ws.ID, false)
	if err != nil {
		return modalView{}, false, err
	}
	var mprojects []modalProject
	var curProjectName, curProjectColor string
	for _, pr := range projects {
		c := board.ProjectColorFor(pr)
		mprojects = append(mprojects, modalProject{ID: pr.ID, Name: pr.Name, Color: c})
		if pr.ID == item.ProjectID {
			curProjectName, curProjectColor = pr.Name, c
		}
	}
	if item.ProjectID != "" && curProjectName == "" {
		if pr, perr := h.board.Project(ctx, item.ProjectID); perr == nil {
			c := board.ProjectColorFor(pr)
			mprojects = append(mprojects, modalProject{ID: pr.ID, Name: pr.Name, Color: c})
			curProjectName, curProjectColor = pr.Name, c
		}
	}

	viewerID := ""
	if p := principalFrom(ctx); p != nil {
		viewerID = p.ID
	}
	backlogID := ""
	for _, b := range boards {
		if isBacklogBoard(b) {
			backlogID = b.ID
			break
		}
	}
	timeline := buildTimeline(comments, history, nameByID, colorByStatus, viewerID, backlogID)

	// The Watch control reflects the viewer's item subscription: whether they
	// watch it, and which categories its filter delivers (for the dropdown).
	watchSub, watching, _ := h.board.SubscriptionFor(ctx, viewerID, store.SubjectItem, itemID)

	return modalView{
		Slug:            ws.Slug,
		CSRFToken:       csrfTokenFrom(ctx),
		Item:            item,
		RefID:           refID(ws.ItemPrefix, item.RefNum),
		Desc:            renderDescription(item.Description),
		StatusGroups:    statusGroups,
		StatusName:      curStatusName,
		StatusBoard:     curStatusBoard,
		StatusDashed:    curStatusDashed,
		StatusColorVar:  colorVar(curStatusColor),
		Assignables:     assignables,
		Assignee:        nameByID[item.AssigneeID],
		Projects:        mprojects,
		ProjectName:     curProjectName,
		ProjectColorVar: colorVar(curProjectColor),
		Archived:        item.ArchivedAt != nil,
		ParentID:        item.ParentID,
		ParentTitle:     parentTitle,
		Children:        kids,
		SubDone:         done,
		SubTotal:        len(children),
		CreatedBy:       createdBy,
		CreatedByAgent:  isAgent[item.CreatedBy],
		Watching:        watching,
		WatchCats:       catToggles(watchSub.Events),
		Timeline:        timeline,
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
	h.liveUpsert(r, r.PathValue("id"))
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
	h.liveUpsert(r, r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) itemComment(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	p := principalFrom(r.Context())
	itemID := r.PathValue("id")
	c, notified, err := h.board.AddComment(r.Context(), itemID, p.ID, req.Body)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	// Render server-side so a freshly posted comment renders identically to a
	// reloaded one (markdown, avatar colour, relative time) — the card builder in
	// board.js just injects these.
	abs := formatWhen(c.CreatedAt)
	card := map[string]any{
		"id":           c.ID,
		"author":       p.Display,
		"body":         c.Body,
		"body_html":    string(mdToHTML(c.Body)),
		"rel":          relativeWhen(c.CreatedAt),
		"abs":          abs,
		"avatar_style": string(avatarStyle(p.ID)),
		"avatar_text":  initials(p.Display),
	}
	// Stream the comment to everyone else with this item's modal open, and bump
	// each mentioned principal's bell.
	live := map[string]any{"item": itemID}
	maps.Copy(live, card)
	h.publishLive(wsTopic(ws.ID), "comment.add", clientID(r), live)
	h.publishNotifications(r.Context(), notified)
	writeJSON(w, http.StatusOK, card)
}

// itemCommentEdit replaces the body of the caller's own comment. The author
// check lives in the board service; here we just render the new body and fan it
// out to other open modals.
func (h *handlers) itemCommentEdit(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	p := principalFrom(r.Context())
	c, notified, err := h.board.EditComment(r.Context(), r.PathValue("cid"), p.ID, req.Body)
	if err != nil {
		writeBoardErr(w, err)
		return
	}
	payload := map[string]any{
		"item":      r.PathValue("id"),
		"id":        c.ID,
		"body":      c.Body,
		"body_html": string(mdToHTML(c.Body)),
		"edited":    true,
	}
	h.publishLive(wsTopic(ws.ID), "comment.edit", clientID(r), payload)
	h.publishNotifications(r.Context(), notified) // ping any newly @mentioned principal
	writeJSON(w, http.StatusOK, payload)
}

// itemCommentDelete soft-deletes the caller's own comment, replacing it with a
// tombstone everywhere the item's modal is open.
func (h *handlers) itemCommentDelete(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	p := principalFrom(r.Context())
	cid := r.PathValue("cid")
	if _, err := h.board.DeleteComment(r.Context(), cid, p.ID); err != nil {
		writeBoardErr(w, err)
		return
	}
	h.publishLive(wsTopic(ws.ID), "comment.delete", clientID(r), map[string]any{
		"item": r.PathValue("id"),
		"id":   cid,
	})
	w.WriteHeader(http.StatusNoContent)
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
	h.publishSubtaskAdd(clientID(r), it.WorkspaceID, it)
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
	h.liveUpsert(r, r.PathValue("id"))
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
	// The client applies item.upsert by parent_id: a now-subtask is pulled off
	// the board, a now-root is (re)placed.
	h.liveUpsert(r, r.PathValue("id"))
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
	h.publishItemRemove(clientID(r), ws.ID, r.PathValue("id"))
	respond204OrRedirect(w, r, "/"+ws.Slug)
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
	h.liveUpsert(r, r.PathValue("id"))
	respond204OrRedirect(w, r, "/"+ws.Slug+"/archive")
}

// --- archive view ---

type archiveData struct {
	chrome
	Principal *identity.Principal
	Title     string // "Archived items" (workspace) or "Tasks archive" (board)
	BackHref  string // where the ‹ Back link points
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
	ch, err := h.chromeFor(r, "home", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
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
	// A {board} segment scopes the archive (and its lane names) to one board;
	// without one it's the whole workspace's archived items.
	title, backHref := "Archived items", "/"+ws.Slug
	if boardSlugParam(r) != "" {
		bd, ok := h.resolveBoard(w, r, ws)
		if !ok {
			return
		}
		if statuses, err = h.board.BoardStatuses(r.Context(), bd.ID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		items = itemsOnBoard(items, statuses)
		ch.ActiveBoard = bd.Slug
		title, backHref = bd.Name+" archive", boardViewPath(ws.Slug, bd)
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
	render(w, http.StatusOK, "archive.html", archiveData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Title:     title,
		BackHref:  backHref,
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

// relativeWhen renders a coarse, human relative time ("just now", "5m ago",
// "3h ago", "2d ago", "4w ago"), falling back to the absolute date past a month.
func relativeWhen(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < 45*time.Second:
		return "just now"
	case d < 90*time.Second:
		return "1m ago"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	default:
		return formatWhen(t)
	}
}
