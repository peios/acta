package httpapi

import (
	"acta/internal/accounts"
	"net/http"
	"strconv"
)

func pageOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	s := r.URL.Query().Get("offset")
	if s == "" {
		return 0, true
	}
	n, e := strconv.Atoi(s)
	if e != nil {
		writeError(w, 400, "invalid_request", "Invalid page.", nil)
		return 0, false
	}
	return n, true
}
func groupData(g accounts.Group, actor accounts.Account) map[string]any {
	return map[string]any{"id": g.ID, "name": g.Name, "description": g.Description, "is_default": g.Default, "direct_permissions": g.Permissions, "direct_require_mfa": g.RequireMFA, "permissions_version": g.Version, "member_count": g.MemberCount, "assignable": accounts.CanAssignGroup(actor, g)}
}
func (h *Handler) writeGroup(w http.ResponseWriter, r *http.Request, g accounts.Group, status int) {
	a, e := h.auth.Authenticated(r.Context(), token(r, h.sessionCookie))
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, status, groupData(g, a))
}
func (h *Handler) groups(w http.ResponseWriter, r *http.Request) {
	offset, ok := pageOffset(w, r)
	if !ok {
		return
	}
	list, more, e := h.management.ListGroups(r.Context(), token(r, h.sessionCookie), r.URL.Query().Get("q"), offset)
	if e != nil {
		failure(w, e)
		return
	}
	a, e := h.auth.Current(r.Context(), token(r, h.sessionCookie))
	if e != nil {
		failure(w, e)
		return
	}
	out := []map[string]any{}
	for _, g := range list {
		out = append(out, groupData(g, a))
	}
	writeJSON(w, 200, map[string]any{"groups": out, "more": more})
}
func (h *Handler) createGroup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decode(w, r, &in) {
		return
	}
	g, e := h.management.CreateGroup(r.Context(), token(r, h.sessionCookie), in.Name, in.Description)
	if e != nil {
		failure(w, e)
		return
	}
	h.writeGroup(w, r, g, 201)
}
func (h *Handler) group(w http.ResponseWriter, r *http.Request) {
	g, e := h.management.GetGroup(r.Context(), token(r, h.sessionCookie), r.PathValue("id"))
	if e != nil {
		failure(w, e)
		return
	}
	h.writeGroup(w, r, g, 200)
}
func (h *Handler) groupProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     int64  `json:"permissions_version"`
	}
	if !decode(w, r, &in) {
		return
	}
	g, e := h.management.UpdateGroup(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), in.Version, in.Name, in.Description)
	if e != nil {
		failure(w, e)
		return
	}
	h.writeGroup(w, r, g, 200)
}
func (h *Handler) groupPermissions(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Grants  []string `json:"permissions"`
		Require bool     `json:"require_mfa"`
		Version int64    `json:"permissions_version"`
	}
	if !decode(w, r, &in) {
		return
	}
	g, e := h.management.UpdateGroupPermissions(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), in.Version, in.Grants, in.Require)
	if e != nil {
		failure(w, e)
		return
	}
	h.writeGroup(w, r, g, 200)
}
func (h *Handler) groupMembers(w http.ResponseWriter, r *http.Request) {
	offset, ok := pageOffset(w, r)
	if !ok {
		return
	}
	members, more, e := h.management.GroupMembers(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), r.URL.Query().Get("q"), offset)
	if e != nil {
		failure(w, e)
		return
	}
	out := []map[string]any{}
	for _, a := range members {
		out = append(out, accountData(a))
	}
	writeJSON(w, 200, map[string]any{"users": out, "more": more})
}
func (h *Handler) groupMembership(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Member         bool  `json:"member"`
		GroupVersion   int64 `json:"group_version"`
		AccountVersion int64 `json:"permissions_version"`
	}
	if !decode(w, r, &in) {
		return
	}
	e := h.management.SetGroupMembership(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), r.PathValue("account"), in.GroupVersion, in.AccountVersion, in.Member)
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"updated": true})
}
