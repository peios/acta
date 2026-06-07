package web

import (
	"html/template"
	"net/http"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

// eventView is one rendered activity-log line. Summary is the verb phrase
// ("moved from To do to Doing"); Actor and the item are shown alongside it, so
// the phrase itself never repeats them.
type eventView struct {
	Actor     string
	Summary   string
	When      string // absolute timestamp (workspace feed + hover tooltip)
	Rel       string // relative timestamp ("2h ago"), for the modal feed
	ItemID    string
	ItemTitle string
	Kind      string       // glyph selector: status/create/assign/milestone/parent/rename/describe/archive/comment/generic
	Dot       template.CSS // for status/create events, the lane colour to fill the glyph; "" → use the Kind glyph
	Dashed    bool         // the dot is a Backlog-board status — render it as a dashed ring
}

// humanizeEvent turns a stored event into the verb phrase shown to people. The
// mapping is domain logic shared with the subscription fanout (which snapshots
// the phrase onto each notification), so it lives in the board package; this is
// the web-package alias the feed renderers call.
func humanizeEvent(e store.Event) string {
	return board.HumanizeEvent(e)
}

// kindForVerb maps a stored verb to the feed glyph it renders with.
func kindForVerb(verb string) string {
	switch verb {
	case store.EventItemCreated:
		return "create"
	case store.EventItemRenamed:
		return "rename"
	case store.EventItemStatusChange:
		return "status"
	case store.EventItemAssigned:
		return "assign"
	case store.EventItemDescribed:
		return "describe"
	case store.EventItemArchived, store.EventItemUnarchived:
		return "archive"
	case store.EventItemMilestone:
		return "milestone"
	case store.EventItemReparented:
		return "parent"
	case store.EventItemProject:
		return "project"
	case store.EventCommentAdded:
		return "comment"
	case store.EventDocumentAdded, store.EventDocumentUpdated, store.EventDocumentRemoved:
		return "describe" // a document glyph: lines on a page, same as the description icon
	default:
		return "generic"
	}
}

// eventToView renders one stored event. Dot is left unset here; the item modal
// fills it for status/create events, where it has the lane colours to hand.
func eventToView(e store.Event) eventView {
	actor := e.ActorName
	if actor == "" {
		actor = "System"
	}
	return eventView{
		Actor:     actor,
		Summary:   humanizeEvent(e),
		When:      formatWhen(e.CreatedAt),
		Rel:       relativeWhen(e.CreatedAt),
		ItemID:    e.ItemID,
		ItemTitle: e.ItemTitle,
		Kind:      kindForVerb(e.Verb),
	}
}

// toEventViews renders a slice of events for display, oldest- or newest-first
// as given (the caller decides; the store returns newest-first).
func toEventViews(events []store.Event) []eventView {
	out := make([]eventView, len(events))
	for i, e := range events {
		out[i] = eventToView(e)
	}
	return out
}

// setEventDot fills a status/create event's glyph with its destination lane
// colour (when known), so the feed shows the status's coloured dot rather than a
// generic icon. Shared by the item modal timeline and the workspace feed.
func setEventDot(ev *eventView, data, colorByStatus map[string]string, eventBoardID, backlogBoardID string) {
	switch ev.Kind {
	case "status":
		if c := colorByStatus[data["to"]]; c != "" {
			ev.Dot = colorVar(c)
		}
	case "create":
		if c := colorByStatus[data["status"]]; c != "" {
			ev.Dot = colorVar(c)
		}
	}
	// A dot whose status lives on the Backlog board renders dashed (unstarted),
	// matching the picker and board. The event's board is the item's board at the
	// time — for a cross-board move, the destination — so this is exact.
	ev.Dashed = backlogBoardID != "" && eventBoardID == backlogBoardID
}

// statusColors keys lane colours by status name — the form event Data carries
// (humanised), so the feed can colour its dots.
func statusColors(statuses []store.Status) map[string]string {
	m := make(map[string]string, len(statuses))
	for _, st := range statuses {
		m[st.Name] = board.ColorFor(st)
	}
	return m
}

// statusColorsByName resolves a whole workspace's lane colours (across boards),
// for the workspace-wide feed.
func (h *handlers) statusColorsByName(r *http.Request, wsID string) map[string]string {
	statuses, err := h.board.Statuses(r.Context(), wsID)
	if err != nil {
		return nil
	}
	return statusColors(statuses)
}

type activityData struct {
	chrome
	Principal *identity.Principal
	Title     string // "Activity" (workspace) or "Tasks activity" (board)
	BackHref  string // where the ‹ Back link points
	Events    []eventView
}

// activityPage renders an activity feed, newest first, each line linking back to
// the item it touched. With a {board} segment it's that board's feed; without,
// the whole workspace's.
func (h *handlers) activityPage(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	ch, err := h.chromeFor(r, "activity", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	title, backHref := "Activity", "/"+ws.Slug
	colorByStatus := h.statusColorsByName(r, ws.ID)
	var events []store.Event
	if boardSlugParam(r) != "" {
		bd, ok := h.resolveBoard(w, r, ws)
		if !ok {
			return
		}
		events, err = h.board.BoardActivity(r.Context(), bd.ID, 200)
		ch.ActiveBoard = bd.Slug
		title, backHref = bd.Name+" activity", boardViewPath(ws.Slug, bd)
		if sts, serr := h.board.BoardStatuses(r.Context(), bd.ID); serr == nil {
			colorByStatus = statusColors(sts)
		}
	} else {
		events, err = h.board.WorkspaceActivity(r.Context(), ws.ID, 200)
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	backlogID := h.backlogBoardID(r.Context(), ws.ID)
	views := toEventViews(events)
	for i := range views {
		setEventDot(&views[i], events[i].Data, colorByStatus, events[i].BoardID, backlogID)
	}
	render(w, http.StatusOK, "activity.html", activityData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Title:     title,
		BackHref:  backHref,
		Events:    views,
	})
}
