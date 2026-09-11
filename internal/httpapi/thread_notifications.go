package httpapi

import (
	"acta/internal/threads"
	"github.com/google/uuid"
	"net/http"
)

func (h *Handler) threadNotifications(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	data, err := h.auth.ThreadNotifications(r.Context(), owner)
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, data)
}
func (h *Handler) threadNotificationsRead(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.harnessOwner(w, r)
	if !ok {
		return
	}
	var q struct {
		Items []threads.NotificationRead `json:"items"`
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
	if err := h.auth.ReadThreadNotifications(r.Context(), owner, q.Items); err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
