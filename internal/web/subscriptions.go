package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
)

// --- subscription category toggles (shared by the watch controls) ---

// catToggle is one of the five notification-category checkboxes a Watch control
// shows, ticked per the subscription's filter.
type catToggle struct {
	Key     string
	Label   string
	Checked bool
}

// catToggles builds the five category checkboxes for a subject, ticking those in
// the subscription's stored filter.
func catToggles(events []string) []catToggle {
	out := make([]catToggle, 0, len(board.AllCategories))
	for _, c := range board.AllCategories {
		out = append(out, catToggle{Key: c, Label: board.CategoryLabel(c), Checked: slices.Contains(events, c)})
	}
	return out
}

func validSubjectType(t string) bool {
	return t == store.SubjectItem || t == store.SubjectProject || t == store.SubjectPrincipal
}

// principalID is the signed-in principal's id, or "".
func principalID(r *http.Request) string {
	if p := principalFrom(r.Context()); p != nil {
		return p.ID
	}
	return ""
}

// --- inline watch toggles (item modal, project page) ---

// watchStateView is a Watch control's state: whether the caller watches the
// subject and which categories its filter delivers. Shared by the item modal and
// the project page.
type watchStateView struct {
	Watching bool     `json:"watching"`
	Events   []string `json:"events"`
}

// watchState reads the caller's current subscription to a subject.
func (h *handlers) watchState(ctx context.Context, userID, subjectType, subjectID string) watchStateView {
	sub, ok, _ := h.board.SubscriptionFor(ctx, userID, subjectType, subjectID)
	ev := sub.Events
	if ev == nil {
		ev = []string{}
	}
	return watchStateView{Watching: ok, Events: ev}
}

// handleSubscribeJSON drives a Watch control over JSON, shared by item and
// project. `{watching:false}` unsubscribes; `{watching:true}` with no events
// subscribes with the subject-type default; `{watching:true, events:[…]}` sets
// an explicit filter (the category dropdown). Returns the resulting state so the
// control repaints.
func (h *handlers) handleSubscribeJSON(w http.ResponseWriter, r *http.Request, subjectType, subjectID string) {
	var req struct {
		Watching bool      `json:"watching"`
		Events   *[]string `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p := principalFrom(r.Context())
	var err error
	switch {
	case !req.Watching:
		err = h.board.Unsubscribe(r.Context(), p.ID, subjectType, subjectID)
	case req.Events != nil:
		_, err = h.board.SetSubscription(r.Context(), p.ID, subjectType, subjectID, *req.Events)
	default:
		_, err = h.board.Subscribe(r.Context(), p.ID, subjectType, subjectID)
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, h.watchState(r.Context(), p.ID, subjectType, subjectID))
}

// itemSubscribe and projectSubscribe drive the Watch control on the item modal
// and the project page respectively.
func (h *handlers) itemSubscribe(w http.ResponseWriter, r *http.Request) {
	h.handleSubscribeJSON(w, r, store.SubjectItem, r.PathValue("id"))
}

func (h *handlers) projectSubscribe(w http.ResponseWriter, r *http.Request) {
	h.handleSubscribeJSON(w, r, store.SubjectProject, r.PathValue("id"))
}

// --- REST: subscriptions ---

func (h *handlers) apiListSubscriptions(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	subs, err := h.board.Subscriptions(r.Context(), p.ID, "")
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := struct {
		Subscriptions []subscriptionAPI `json:"subscriptions"`
	}{Subscriptions: make([]subscriptionAPI, 0, len(subs))}
	for _, s := range subs {
		out.Subscriptions = append(out.Subscriptions, h.toSubscriptionAPI(r.Context(), s))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) apiSubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type      string   `json:"type"`
		Ref       string   `json:"ref"`
		Workspace string   `json:"workspace"`
		Events    []string `json:"events"`
	}
	if !readAPIJSON(w, r, &req) {
		return
	}
	if !validSubjectType(req.Type) {
		apiError(w, http.StatusBadRequest, errUnknownSubjectType.Error())
		return
	}
	p := principalFrom(r.Context())
	id, err := h.resolveSubjectRef(r.Context(), req.Type, req.Ref, req.Workspace)
	if err != nil {
		apiSubErr(w, err)
		return
	}
	var sub store.Subscription
	if len(req.Events) > 0 {
		sub, err = h.board.SetSubscription(r.Context(), p.ID, req.Type, id, req.Events)
	} else {
		sub, err = h.board.Subscribe(r.Context(), p.ID, req.Type, id)
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, h.toSubscriptionAPI(r.Context(), sub))
}

func (h *handlers) apiUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type      string `json:"type"`
		Ref       string `json:"ref"`
		Workspace string `json:"workspace"`
	}
	if !readAPIJSON(w, r, &req) {
		return
	}
	if !validSubjectType(req.Type) {
		apiError(w, http.StatusBadRequest, errUnknownSubjectType.Error())
		return
	}
	p := principalFrom(r.Context())
	id, err := h.resolveSubjectRef(r.Context(), req.Type, req.Ref, req.Workspace)
	if err != nil {
		apiSubErr(w, err)
		return
	}
	if err := h.board.Unsubscribe(r.Context(), p.ID, req.Type, id); err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiSubErr maps a subject-resolution failure to a clean status. All the
// resolution errors carry user-facing messages (they name the bad value), so
// they pass straight through.
func apiSubErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errUnknownProject), errors.Is(err, errUnknownUser), errors.Is(err, store.ErrItemNotFound):
		apiError(w, http.StatusNotFound, err.Error())
	default:
		apiError(w, http.StatusBadRequest, err.Error())
	}
}
