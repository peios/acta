package httpapi

import (
	"context"
	"net/http"
	"time"

	"acta2/internal/accounts"
	"acta2/internal/auth"
	"acta2/internal/backup"
)

func (h *Handler) updateRoutes(m *http.ServeMux) {
	for _, p := range []string{"GET /api/updates", "POST /api/updates/check", "POST /api/updates/install", "POST /api/updates/retry"} {
		m.HandleFunc(p, h.updates)
	}
}
func (h *Handler) updates(w http.ResponseWriter, r *http.Request) {
	a, err := h.auth.Current(r.Context(), token(r, h.sessionCookie))
	if err != nil {
		failure(w, err)
		return
	}
	if a.IsAgent() || !accounts.CheckPermission(a, accounts.Superuser) {
		failure(w, auth.ErrForbidden)
		return
	}
	if h.config.UpdateSocket == "" {
		if r.Method == "GET" {
			writeJSON(w, 200, map[string]bool{"configured": false})
			return
		}
		writeError(w, 503, "unavailable", "The deployment updater is not configured.", nil)
		return
	}
	path := "/v1/status"
	var in any
	switch r.URL.Path {
	case "/api/updates/check":
		path = "/v1/check"
	case "/api/updates/retry":
		path = "/v1/retry"
	case "/api/updates/install":
		var body struct {
			ID string `json:"id"`
		}
		if !decode(w, r, &body) {
			return
		}
		path = "/v1/install"
		in = map[string]string{"id": body.ID, "actor": a.ID}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	status, raw, err := (backup.Client{Socket: h.config.UpdateSocket, TokenFile: h.config.UpdateTokenFile, Timeout: 2 * time.Minute}).Do(ctx, r.Method, path, in)
	if err != nil {
		writeError(w, status, "unavailable", "The deployment updater is unavailable. It may still be working; reconnect to check progress.", nil)
		return
	}
	writeJSON(w, status, raw)
}
