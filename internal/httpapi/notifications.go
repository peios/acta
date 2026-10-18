package httpapi

import (
	"acta/internal/notifications"
	"github.com/google/uuid"
	"net/http"
)

func (h *Handler) notifications(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.notificationOwner(w, r)
	if !ok {
		return
	}
	data, err := h.auth.Notifications(r.Context(), owner)
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, data)
}
func (h *Handler) notificationsRead(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.notificationOwner(w, r)
	if !ok {
		return
	}
	var q struct {
		Items []notifications.NotificationRead `json:"items"`
	}
	if !decode(w, r, &q) {
		return
	}
	valid := len(q.Items) > 0 && len(q.Items) <= 200
	for _, item := range q.Items {
		if _, err := uuid.Parse(item.ID); err != nil || item.Revision < 1 {
			valid = false
		}
	}
	if !valid {
		writeError(w, 400, "invalid_notifications", "Choose up to 200 notification revisions to mark read.", nil)
		return
	}
	if err := h.auth.ReadNotifications(r.Context(), owner, q.Items); err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Handler) notificationOwner(w http.ResponseWriter, r *http.Request) (string, bool) {
	a, err := h.auth.Current(r.Context(), token(r, h.sessionCookie))
	if err != nil {
		failure(w, err)
		return "", false
	}
	if a.ParentID != nil {
		writeError(w, 403, "human_required", "Notifications require a human account.", nil)
		return "", false
	}
	return a.ID, true
}
