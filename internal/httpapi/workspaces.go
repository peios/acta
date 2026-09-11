package httpapi

import (
	"net/http"
	"strconv"

	"acta/internal/auth"
	ws "acta/internal/workspaces"
)

func (h *Handler) workspaceRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/workspaces", h.workspaces)
	m.HandleFunc("POST /api/workspaces", h.createWorkspace)
	m.HandleFunc("GET /api/workspaces/permissions", func(w http.ResponseWriter, r *http.Request) {
		if _, e := h.auth.Current(r.Context(), token(r, h.sessionCookie)); e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"permissions": ws.Catalogue()})
	})
	m.HandleFunc("GET /api/workspace-slugs/{slug}", h.workspace)
	m.HandleFunc("GET /api/workspaces/{workspace}", h.workspace)
	m.HandleFunc("POST /api/workspaces/{workspace}/profile", h.workspaceProfile)
	m.HandleFunc("POST /api/workspaces/{workspace}/visit", h.workspaceVisit)
	m.HandleFunc("GET /api/workspaces/{workspace}/members", h.workspaceMembers)
	m.HandleFunc("POST /api/workspaces/{workspace}/members/{account}", h.workspaceMembership)
	m.HandleFunc("GET /api/workspaces/{workspace}/groups", h.workspaceGroups)
	m.HandleFunc("POST /api/workspaces/{workspace}/members/{account}/permissions", h.workspaceGrants)
	m.HandleFunc("POST /api/workspaces/{workspace}/groups/{group}/permissions", h.workspaceGrants)
	m.HandleFunc("GET /api/agents/{id}/workspaces", h.agentWorkspaces)
	m.HandleFunc("POST /api/agents/{id}/workspaces", h.agentWorkspaceAccess)
	m.HandleFunc("POST /api/agents/{id}/workspaces/{workspace}/permissions", h.agentWorkspacePolicy)
}
func workspacePage(r *http.Request) (string, int, error) {
	offset := 0
	var e error
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, e = strconv.Atoi(raw)
		if e != nil || offset < 0 {
			return "", 0, auth.ErrNotFound
		}
	}
	return r.URL.Query().Get("q"), offset, nil
}
func (h *Handler) workspaces(w http.ResponseWriter, r *http.Request) {
	q, o, e := workspacePage(r)
	if e != nil {
		failure(w, e)
		return
	}
	list, more, e := h.management.WorkspaceList(r.Context(), token(r, h.sessionCookie), q, o)
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"workspaces": list, "more": more})
}

type workspaceProfileInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Version     int64  `json:"version"`
}

func (h *Handler) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var in workspaceProfileInput
	if !decode(w, r, &in) {
		return
	}
	v, e := h.management.CreateWorkspace(r.Context(), token(r, h.sessionCookie), in.Name, in.Slug, in.Description)
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 201, v)
}
func (h *Handler) workspace(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("workspace")
	bySlug := r.PathValue("slug") != ""
	if bySlug {
		ref = r.PathValue("slug")
	}
	v, e := h.management.Workspace(r.Context(), token(r, h.sessionCookie), ref, bySlug)
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h *Handler) workspaceProfile(w http.ResponseWriter, r *http.Request) {
	var in workspaceProfileInput
	if !decode(w, r, &in) {
		return
	}
	v, e := h.management.EditWorkspace(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), in.Version, in.Name, in.Slug, in.Description)
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h *Handler) workspaceVisit(w http.ResponseWriter, r *http.Request) {
	if e := h.management.VisitWorkspace(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace")); e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"updated": true})
}
func (h *Handler) workspaceMembers(w http.ResponseWriter, r *http.Request) {
	q, o, e := workspacePage(r)
	if e != nil {
		failure(w, e)
		return
	}
	v, more, e := h.management.WorkspaceMembers(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), q, o, r.URL.Query().Get("candidates") == "true")
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"members": v, "more": more})
}
func (h *Handler) workspaceGroups(w http.ResponseWriter, r *http.Request) {
	q, o, e := workspacePage(r)
	if e != nil {
		failure(w, e)
		return
	}
	v, more, e := h.management.WorkspaceGroups(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), q, o)
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"groups": v, "more": more})
}
func (h *Handler) workspaceMembership(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64 `json:"version"`
		Member  bool  `json:"member"`
	}
	if !decode(w, r, &in) {
		return
	}
	if e := h.management.WorkspaceMembership(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), r.PathValue("account"), in.Version, in.Member); e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"updated": true})
}
func (h *Handler) workspaceGrants(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version     int64    `json:"version"`
		Permissions []string `json:"permissions"`
	}
	if !decode(w, r, &in) {
		return
	}
	target := r.PathValue("account")
	group := r.PathValue("group") != ""
	if group {
		target = r.PathValue("group")
	}
	if e := h.management.WorkspaceGrants(r.Context(), token(r, h.sessionCookie), r.PathValue("workspace"), target, in.Version, group, in.Permissions); e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"updated": true})
}
func (h *Handler) agentWorkspaces(w http.ResponseWriter, r *http.Request) {
	v, e := h.management.AgentWorkspaces(r.Context(), token(r, h.sessionCookie), r.PathValue("id"))
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h *Handler) agentWorkspaceAccess(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version  int64    `json:"version"`
		All      bool     `json:"all"`
		Selected []string `json:"selected"`
	}
	if !decode(w, r, &in) {
		return
	}
	if e := h.management.SetAgentWorkspaces(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), in.Version, in.All, in.Selected); e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"updated": true})
}
func (h *Handler) agentWorkspacePolicy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version     int64    `json:"version"`
		Inherit     bool     `json:"inherit"`
		Permissions []string `json:"permissions"`
	}
	if !decode(w, r, &in) {
		return
	}
	if e := h.management.SetAgentWorkspacePolicy(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), r.PathValue("workspace"), in.Version, in.Inherit, in.Permissions); e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"updated": true})
}
