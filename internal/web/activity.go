package web

import (
	"net/http"

	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

// eventView is one rendered activity-log line. Summary is the verb phrase
// ("moved from To do to Doing"); Actor and the item are shown alongside it, so
// the phrase itself never repeats them.
type eventView struct {
	Actor     string
	Summary   string
	When      string
	ItemID    string
	ItemTitle string
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

// toEventViews renders a slice of events for display, oldest- or newest-first
// as given (the caller decides; the store returns newest-first).
func toEventViews(events []store.Event) []eventView {
	out := make([]eventView, len(events))
	for i, e := range events {
		actor := e.ActorName
		if actor == "" {
			actor = "System"
		}
		out[i] = eventView{
			Actor:     actor,
			Summary:   humanizeEvent(e),
			When:      formatWhen(e.CreatedAt),
			ItemID:    e.ItemID,
			ItemTitle: e.ItemTitle,
		}
	}
	return out
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
	render(w, http.StatusOK, "activity.html", activityData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Events:    toEventViews(events),
	})
}
