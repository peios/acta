package httpapi

import "net/http"

func (h *Handler) taskFollowRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/tasks/{task}/following", func(w http.ResponseWriter, r *http.Request) {
		following, err := h.management.TaskFollowing(r.Context(), token(r, h.sessionCookie), r.PathValue("task"))
		if err != nil {
			failure(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"following": following})
	})
	m.HandleFunc("POST /api/tasks/{task}/following", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Following *bool `json:"following"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Following == nil {
			writeError(w, 400, "invalid_following", "Choose whether to follow this task.", nil)
			return
		}
		if err := h.management.SetTaskFollowing(r.Context(), token(r, h.sessionCookie), r.PathValue("task"), *in.Following); err != nil {
			failure(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"following": *in.Following})
	})
}
