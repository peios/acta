package httpapi

import (
	"acta/internal/activity"
	"net/http"
)

func (h *Handler) taskActivityRoutes(m *http.ServeMux) {
	h.commentRoutes(m)
	m.HandleFunc("GET /api/tasks/{task}/activity", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.management.TaskActivity(r.Context(), token(r, h.sessionCookie), r.PathValue("task"), r.URL.Query().Get("cursor"))
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("POST /api/tasks/{task}/activity/read", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Entries []activity.Seen `json:"entries"`
		}
		if !decode(w, r, &in) {
			return
		}
		if e := h.management.ReadTaskActivity(r.Context(), token(r, h.sessionCookie), r.PathValue("task"), in.Entries); e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
}
