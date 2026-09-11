package httpapi

import (
	"acta/internal/comments"
	"net/http"
)

func (h *Handler) commentRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/tasks/{task}/comments", func(w http.ResponseWriter, r *http.Request) {
		var in comments.Create
		if !decode(w, r, &in) {
			return
		}
		v, e := h.management.CreateComment(r.Context(), token(r, h.sessionCookie), r.PathValue("task"), in)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("GET /api/tasks/{task}/comments/{comment}", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.management.Comment(r.Context(), token(r, h.sessionCookie), r.PathValue("task"), r.PathValue("comment"))
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("GET /api/tasks/{task}/comments/{comment}/replies", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.management.CommentReplies(r.Context(), token(r, h.sessionCookie), r.PathValue("task"), r.PathValue("comment"), r.URL.Query().Get("cursor"))
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("POST /api/tasks/{task}/comments/{comment}", func(w http.ResponseWriter, r *http.Request) {
		var in comments.Update
		if !decode(w, r, &in) {
			return
		}
		v, e := h.management.UpdateComment(r.Context(), token(r, h.sessionCookie), r.PathValue("task"), r.PathValue("comment"), in)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
}
