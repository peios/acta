package httpapi

import (
	"acta2/internal/push"
	"errors"
	"github.com/google/uuid"
	"net/http"
	"strconv"
)

func (h *Handler) pushRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/push", h.pushConfig)
	m.HandleFunc("POST /api/push/subscribe", h.pushSubscribe)
	m.HandleFunc("POST /api/push/unsubscribe", h.pushUnsubscribe)
	m.HandleFunc("GET /api/push/notification", h.pushNotification)
}
func (h *Handler) pushConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.harnessOwner(w, r); !ok {
		return
	}
	writeJSON(w, 200, map[string]string{"public_key": h.pushKey})
}
func (h *Handler) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	if h.pushKey == "" {
		writeError(w, 503, "push_unavailable", "Push is not configured on this server.", nil)
		return
	}
	var sub push.Subscription
	if !decode(w, r, &sub) {
		return
	}
	if err := push.Validate(sub); err != nil {
		writeError(w, 400, "invalid_subscription", "The browser supplied an invalid push subscription.", nil)
		return
	}
	id, err := h.auth.SavePush(r.Context(), owner, token(r, h.sessionCookie), sub)
	if errors.Is(err, push.ErrSubscription) {
		writeError(w, 409, "push_subscription_conflict", "Reset browser alerts and try again, or remove an unused subscription.", nil)
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"id": id})
}
func (h *Handler) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	var q struct {
		Endpoint string `json:"endpoint"`
	}
	if !decode(w, r, &q) {
		return
	}
	if q.Endpoint == "" || len(q.Endpoint) > 4096 {
		writeError(w, 400, "invalid_subscription", "Choose a browser subscription.", nil)
		return
	}
	if err := h.auth.DeletePush(r.Context(), owner, token(r, h.sessionCookie), q.Endpoint); err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (h *Handler) pushNotification(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	revision, err := strconv.ParseInt(q.Get("revision"), 10, 64)
	_, a := uuid.Parse(q.Get("subscription"))
	_, b := uuid.Parse(q.Get("id"))
	if err != nil || revision < 1 || a != nil || b != nil {
		writeError(w, 400, "invalid_notification", "Invalid push notification.", nil)
		return
	}
	n, err := h.auth.PushNotice(r.Context(), owner, token(r, h.sessionCookie), q.Get("subscription"), q.Get("id"), revision)
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, n)
}
