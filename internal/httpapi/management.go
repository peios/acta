package httpapi

import (
	"acta2/internal/accounts"
	"acta2/internal/auth"
	"net/http"
	"strconv"
)

func (h *Handler) users(w http.ResponseWriter, r *http.Request) {
	offset := 0
	var e error
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, e = strconv.Atoi(raw)
		if e != nil {
			writeError(w, 400, "invalid_request", "Invalid page.", nil)
			return
		}
	}
	users, more, e := h.management.List(r.Context(), token(r, h.sessionCookie), r.URL.Query().Get("status"), r.URL.Query().Get("q"), offset)
	if e != nil {
		failure(w, e)
		return
	}
	out := []map[string]any{}
	for _, a := range users {
		out = append(out, accountData(a))
	}
	writeJSON(w, 200, map[string]any{"users": out, "more": more})
}
func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	}
	if !decode(w, r, &in) {
		return
	}
	a, secret, e := h.management.Create(r.Context(), token(r, h.sessionCookie), in.Username, in.DisplayName)
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 201, map[string]any{"account": accountData(a), "url": h.config.PublicURL + "/account-link#token=" + secret})
}
func (h *Handler) user(w http.ResponseWriter, r *http.Request) {
	a, e := h.management.Get(r.Context(), token(r, h.sessionCookie), r.PathValue("id"))
	if e != nil {
		failure(w, e)
		return
	}
	writeAccount(w, a)
}
func (h *Handler) userProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username    string  `json:"username"`
		DisplayName *string `json:"display_name"`
		Version     int64   `json:"profile_version"`
	}
	if !decode(w, r, &in) {
		return
	}
	name := ""
	if in.DisplayName != nil {
		name = *in.DisplayName
	}
	a, e := h.management.Update(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), in.Version, in.Username, name)
	if e != nil {
		failure(w, e)
		return
	}
	writeAccount(w, a)
}
func (h *Handler) userLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DisableMFA     bool `json:"disable_mfa"`
		RemovePasskeys bool `json:"remove_passkeys"`
	}
	if !decode(w, r, &in) {
		return
	}
	secret, expires, e := h.management.Link(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), in.DisableMFA, in.RemovePasskeys)
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"url": h.config.PublicURL + "/account-link#token=" + secret, "expires_at": expires})
}
func (h *Handler) userDisabled(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Disabled bool `json:"disabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if e := h.management.Disable(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), in.Disabled); e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"updated": true})
}
func (h *Handler) openAccountLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	g, a, e := h.management.InspectLink(r.Context(), in.Token, h.address(r))
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"can_change_display_name": accounts.CheckPermission(a, accounts.ChangeDisplayName), "purpose": g.Purpose, "username": a.Handle(), "display_name": a.DisplayName, "profile_version": a.ProfileVersion, "disable_mfa": g.DisableMFA, "remove_passkeys": g.RemovePasskeys})
}
func (h *Handler) redeemAccountLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token       string `json:"token"`
		Password    string `json:"new_password"`
		DisplayName string `json:"display_name"`
		Version     int64  `json:"profile_version"`
	}
	if !decode(w, r, &in) {
		return
	}
	e := h.management.Redeem(r.Context(), in.Token, in.Password, in.DisplayName, h.address(r), in.Version)
	if e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"complete": true})
}

func (h *Handler) permissionCatalogue(w http.ResponseWriter, r *http.Request) {
	a, e := h.auth.Current(r.Context(), token(r, h.sessionCookie))
	if e != nil {
		failure(w, e)
		return
	}
	if !accounts.CheckPermission(a, accounts.ManagePermissions) {
		failure(w, auth.ErrForbidden)
		return
	}
	writeJSON(w, 200, map[string]any{"permissions": accounts.PermissionCatalogue()})
}
func (h *Handler) userPermissions(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Grants     []string `json:"permissions"`
		RequireMFA bool     `json:"require_mfa"`
		Version    int64    `json:"permissions_version"`
	}
	if !decode(w, r, &in) {
		return
	}
	a, e := h.management.UpdatePermissions(r.Context(), token(r, h.sessionCookie), r.PathValue("id"), in.Version, in.Grants, in.RequireMFA)
	if e != nil {
		failure(w, e)
		return
	}
	writeAccount(w, a)
}
