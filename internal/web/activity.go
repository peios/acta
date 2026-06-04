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
}

// humanizeEvent turns a stored event into the verb phrase shown to people. It
// reads the same denormalised Data the board service wrote, so it never needs
// to resolve ids — and stays correct after the referenced rows change.
func humanizeEvent(e store.Event) string {
	d := e.Data
	switch e.Verb {
	case store.EventItemCreated:
		if s := d["status"]; s != "" {
			return "created this in " + s
		}
		return "created this"
	case store.EventItemRenamed:
		return "renamed “" + d["from"] + "” → “" + d["to"] + "”"
	case store.EventItemStatusChange:
		return "moved from " + d["from"] + " to " + d["to"]
	case store.EventItemAssigned:
		switch {
		case d["to"] == "":
			return "unassigned this"
		case d["from"] == "":
			return "assigned this to " + d["to"]
		default:
			return "reassigned this from " + d["from"] + " to " + d["to"]
		}
	case store.EventItemDescribed:
		return "updated the description"
	case store.EventItemArchived:
		return "archived this"
	case store.EventItemUnarchived:
		return "restored this"
	case store.EventItemMilestone:
		if d["on"] == "true" {
			return "marked this as a milestone"
		}
		return "removed the milestone mark"
	case store.EventItemReparented:
		if d["to"] == "" {
			return "moved this to the top level"
		}
		return "moved this under " + d["to"]
	case store.EventCommentAdded:
		if x := d["excerpt"]; x != "" {
			return "commented: “" + x + "”"
		}
		return "added a comment"
	default:
		return e.Verb
	}
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
	case store.EventCommentAdded:
		return "comment"
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
func setEventDot(ev *eventView, data, colorByStatus map[string]string) {
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
}

// statusColorsByName resolves a workspace's lane colours keyed by status name —
// the form event Data carries (humanised), so the feed can colour its dots.
func (h *handlers) statusColorsByName(r *http.Request, wsID string) map[string]string {
	statuses, err := h.board.Statuses(r.Context(), wsID)
	if err != nil {
		return nil
	}
	m := make(map[string]string, len(statuses))
	for _, st := range statuses {
		m[st.Name] = board.ColorFor(st)
	}
	return m
}

type activityData struct {
	chrome
	Principal *identity.Principal
	Events    []eventView
}

// activityPage renders the workspace-wide activity feed: every recorded
// mutation, newest first, each linking back to the item it touched.
func (h *handlers) activityPage(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.resolveWorkspace(w, r)
	if !ok {
		return
	}
	events, err := h.board.WorkspaceActivity(r.Context(), ws.ID, 200)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	ch, err := h.chromeFor(r, "activity", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	colorByStatus := h.statusColorsByName(r, ws.ID)
	views := toEventViews(events)
	for i := range views {
		setEventDot(&views[i], events[i].Data, colorByStatus)
	}
	render(w, http.StatusOK, "activity.html", activityData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Events:    views,
	})
}
