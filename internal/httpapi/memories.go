package httpapi

import (
	"acta/internal/memories"
	"net/http"
)

func (h *Handler) memoryRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/memories", func(w http.ResponseWriter, r *http.Request) {
		in := memories.Recall{Query: r.URL.Query().Get("query"), Cursor: r.URL.Query().Get("cursor"), AgentID: r.URL.Query().Get("agent_id")}
		if v := r.URL.Query().Get("workspace"); v != "" {
			in.Workspace = &v
		}
		out, e := h.management.RecallMemories(r.Context(), token(r, h.sessionCookie), in)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, out)
	})
	m.HandleFunc("GET /api/memories/{id}", func(w http.ResponseWriter, r *http.Request) {
		out, e := h.management.GetMemory(r.Context(), token(r, h.sessionCookie), r.PathValue("id"))
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, out)
	})
	m.HandleFunc("POST /api/memories", func(w http.ResponseWriter, r *http.Request) {
		var in memories.Save
		if !decode(w, r, &in) {
			return
		}
		out, e := h.management.SaveMemory(r.Context(), token(r, h.sessionCookie), in)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, out)
	})
	m.HandleFunc("POST /api/memories/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Revision int64 `json:"revision"`
		}
		if !decode(w, r, &in) {
			return
		}
		if e := h.management.DeleteMemory(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), in.Revision); e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, map[string]bool{"deleted": true})
	})
}
