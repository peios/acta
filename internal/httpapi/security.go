package httpapi

import (
	"net/http"
	"strings"
	"time"

	"acta/internal/auth"
)

func (h *Handler) binding(w http.ResponseWriter, r *http.Request) (string, error) {
	value := token(r, h.bindingCookie)
	if value != "" {
		return value, nil
	}
	value, err := auth.Token()
	if err == nil {
		h.cookie(w, h.bindingCookie, value, 24*time.Hour)
	}
	return value, err
}
func (h *Handler) flowResponse(w http.ResponseWriter, r *http.Request, result auth.FlowResult) {
	if result.SessionToken != "" {
		if err := h.auth.Logout(r.Context(), token(r, h.sessionCookie)); err != nil {
			_ = h.auth.Logout(r.Context(), result.SessionToken)
			failure(w, err)
			return
		}
		h.cookie(w, h.sessionCookie, result.SessionToken, auth.SessionLifetime)
	}
	writeJSON(w, 200, result)
}
func (h *Handler) securityView(w http.ResponseWriter, r *http.Request) {
	result, err := h.security.View(r.Context(), token(r, h.sessionCookie))
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *Handler) securityBegin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Purpose string `json:"purpose"`
		Target  string `json:"target"`
		Enabled bool   `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	binding, err := h.binding(w, r)
	if err != nil {
		failure(w, err)
		return
	}
	result, err := h.security.Begin(r.Context(), in.Purpose, in.Target, in.Enabled, token(r, h.sessionCookie), binding, h.address(r))
	if err != nil {
		failure(w, err)
		return
	}
	h.flowResponse(w, r, result)
}
func (h *Handler) securityAdvance(w http.ResponseWriter, r *http.Request) {
	var in auth.FlowInput
	if !decode(w, r, &in) {
		return
	}
	result, err := h.security.Advance(r.Context(), in, token(r, h.sessionCookie), token(r, h.bindingCookie), h.address(r), sessionDescription(r.UserAgent()))
	if err != nil {
		failure(w, err)
		return
	}
	h.flowResponse(w, r, result)
}
func (h *Handler) securityCancel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := h.security.Cancel(r.Context(), in.ID, token(r, h.bindingCookie)); err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"cancelled": true})
}
func (h *Handler) securityRevoke(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := h.security.Revoke(r.Context(), token(r, h.sessionCookie), in.ID); err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"revoked": true})
}
func sessionDescription(agent string) string {
	browser := "Browser"
	for _, item := range []struct{ match, name string }{{"Edg/", "Edge"}, {"Firefox/", "Firefox"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"}} {
		if strings.Contains(agent, item.match) {
			browser = item.name
			break
		}
	}
	os := ""
	for _, item := range []struct{ match, name string }{{"Android", "Android"}, {"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Windows", "Windows"}, {"Macintosh", "macOS"}, {"Linux", "Linux"}} {
		if strings.Contains(agent, item.match) {
			os = item.name
			break
		}
	}
	if os != "" {
		return browser + " · " + os
	}
	return browser + " session"
}
