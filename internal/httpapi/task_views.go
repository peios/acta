package httpapi

import (
	"acta2/internal/tasks"
	"net/http"
)

func (h *Handler) taskViewRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/workspaces/{workspace}/task-views", func(w http.ResponseWriter, r *http.Request) {
		views, e := h.management.TaskViews(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), r.URL.Query().Get("board"))
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"views": views})
	})
	m.HandleFunc("POST /api/workspaces/{workspace}/task-views", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
			tasks.ViewSettings
		}
		if !decode(w, r, &in) {
			return
		}
		v, e := h.management.CreateTaskView(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), in.Name, in.ViewSettings)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 201, v)
	})
	m.HandleFunc("POST /api/workspaces/{workspace}/task-views/{view}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Version int64 `json:"version"`
			tasks.ViewSettings
		}
		if !decode(w, r, &in) {
			return
		}
		v, e := h.management.SaveTaskView(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), r.PathValue("view"), in.Version, in.ViewSettings)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("POST /api/workspaces/{workspace}/task-views/{view}/rename", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Version int64  `json:"version"`
			Name    string `json:"name"`
		}
		if !decode(w, r, &in) {
			return
		}
		v, e := h.management.RenameTaskView(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), r.PathValue("view"), in.Version, in.Name)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("POST /api/workspaces/{workspace}/task-views/{view}/delete", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Version int64 `json:"version"`
		}
		if !decode(w, r, &in) {
			return
		}
		e := h.management.DeleteTaskView(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), r.PathValue("view"), in.Version)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, map[string]bool{"deleted": true})
	})

}
