package httpapi

import (
	"net/http"

	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/backup"
)

func (h *Handler) backupRoutes(m *http.ServeMux) {
	for _, pattern := range []string{"GET /api/backups", "POST /api/backups/policy", "POST /api/backups/jobs"} {
		m.HandleFunc(pattern, h.backups)
	}
}
func (h *Handler) backups(w http.ResponseWriter, r *http.Request) {
	a, err := h.auth.Current(r.Context(), token(r, h.sessionCookie))
	if err != nil {
		failure(w, err)
		return
	}
	if a.IsAgent() || !accounts.CheckPermission(a, accounts.ManageBackups) {
		failure(w, auth.ErrForbidden)
		return
	}
	if h.config.BackupSocket == "" {
		if r.Method == "GET" {
			writeJSON(w, 200, map[string]any{"configured": false})
			return
		}
		writeError(w, 503, "unavailable", "The deployment operator has not connected a backup service.", nil)
		return
	}
	path := "/v1/status"
	var in any
	if r.Method == "POST" {
		switch r.URL.Path {
		case "/api/backups/policy":
			var p backup.Policy
			if !decode(w, r, &p) {
				return
			}
			in = p
			path = "/v1/policy"
		case "/api/backups/jobs":
			var j struct {
				Kind string `json:"kind"`
			}
			if !decode(w, r, &j) {
				return
			}
			in = map[string]string{"kind": j.Kind, "actor": a.ID}
			path = "/v1/jobs"
		}
	}
	status, raw, err := (backup.Client{Socket: h.config.BackupSocket, TokenFile: h.config.BackupTokenFile}).Do(r.Context(), r.Method, path, in)
	if err != nil {
		writeError(w, status, "unavailable", err.Error(), nil)
		return
	}
	writeJSON(w, status, raw)
}
