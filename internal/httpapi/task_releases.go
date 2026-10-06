package httpapi

import (
	"acta/internal/tasks"
	"net/http"
)

func (h *Handler) taskReleaseRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/workspaces/{workspace}/releases", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.management.TaskReleases(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"))
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"releases": v})
	})
	m.HandleFunc("POST /api/workspaces/{workspace}/releases", func(w http.ResponseWriter, r *http.Request) {
		var in tasks.ReleaseCreate
		if !decode(w, r, &in) {
			return
		}
		v, e := h.management.CreateTaskRelease(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), in)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 201, v)
	})
	m.HandleFunc("GET /api/workspaces/{workspace}/releases/{release}", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.management.TaskRelease(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), r.PathValue("release"))
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("POST /api/workspaces/{workspace}/releases/{release}", func(w http.ResponseWriter, r *http.Request) {
		var in tasks.ReleaseUpdate
		if !decode(w, r, &in) {
			return
		}
		v, e := h.management.UpdateTaskRelease(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), r.PathValue("release"), in)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
}
