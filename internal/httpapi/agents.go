package httpapi

import (
	"acta/internal/accounts"
	"acta/internal/auth"
	"net/http"
)

func (h *Handler) agents(w http.ResponseWriter, r *http.Request) {
	list, e := h.management.Agents(r.Context(), token(r, h.sessionCookie))
	if e != nil {
		failure(w, e)
		return
	}
	out := []map[string]any{}
	for _, a := range list {
		out = append(out, accountData(a))
	}
	writeJSON(w, 200, map[string]any{"agents": out})
}
func (h *Handler) createAgent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	}
	if !decode(w, r, &in) {
		return
	}
	a, e := h.management.CreateAgent(r.Context(), token(r, h.sessionCookie), in.Username, in.DisplayName)
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 201, accountData(a))
}
func (h *Handler) agent(w http.ResponseWriter, r *http.Request) {
	a, e := h.management.Agent(r.Context(), token(r, h.sessionCookie), r.PathValue("id"))
	if e != nil {
		failure(w, e)
		return
	}
	writeAccount(w, a)
}
func (h *Handler) agentProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Version     int64  `json:"profile_version"`
	}
	if !decode(w, r, &in) {
		return
	}
	a, e := h.management.UpdateAgent(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), in.Version, in.Username, in.DisplayName)
	if e != nil {
		failure(w, e)
		return
	}
	writeAccount(w, a)
}
func (h *Handler) agentPermissions(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Permissions []string `json:"permissions"`
		Version     int64    `json:"permissions_version"`
	}
	if !decode(w, r, &in) {
		return
	}
	a, e := h.management.AgentPermissions(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), in.Version, in.Permissions)
	if e != nil {
		failure(w, e)
		return
	}
	writeAccount(w, a)
}
func (h *Handler) agentCatalogue(w http.ResponseWriter, r *http.Request) {
	a, e := h.auth.Current(r.Context(), token(r, h.sessionCookie))
	if e != nil {
		failure(w, e)
		return
	}
	if a.IsAgent() {
		failure(w, auth.ErrForbidden)
		return
	}
	writeJSON(w, 200, map[string]any{"permissions": accounts.AgentCatalogue(a)})
}
func (h *Handler) agentDisabled(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Disabled bool `json:"disabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if e := h.management.DisableAgent(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), in.Disabled); e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"updated": true})
}
func (h *Handler) agentSessions(w http.ResponseWriter, r *http.Request) {
	sessions, e := h.management.AgentSessions(r.Context(), token(r, h.sessionCookie), r.PathValue("id"))
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"sessions": sessions})
}
func (h *Handler) agentRevoke(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if e := h.management.RevokeAgent(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), in.ID); e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"revoked": true})
}
