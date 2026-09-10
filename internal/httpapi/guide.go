package httpapi

import (
	"acta2/internal/guide"
	"acta2/learn"
	"net/http"
)

func (h *Handler) guideRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/guide", func(w http.ResponseWriter, r *http.Request) {
		prefs, err := h.management.GuidePreferences(r.Context(), token(r, h.sessionCookie))
		if err != nil {
			failure(w, err)
			return
		}
		writeJSON(w, 200, guide.Compose(learn.AgentGuide, prefs))
	})
	m.HandleFunc("POST /api/guide/preferences", func(w http.ResponseWriter, r *http.Request) {
		var in guide.Save
		if !decode(w, r, &in) {
			return
		}
		prefs, err := h.management.SaveGuidePreferences(r.Context(), token(r, h.sessionCookie), in)
		if err != nil {
			failure(w, err)
			return
		}
		writeJSON(w, 200, prefs)
	})
}
