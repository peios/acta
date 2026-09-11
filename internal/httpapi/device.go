package httpapi

import (
	"acta/internal/auth"
	"errors"
	"net/http"
)

func (h *Handler) deviceStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Machine string `json:"machine"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := h.security.StartDevice(r.Context(), h.address(r), in.Machine)
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 201, out)
}
func (h *Handler) devicePoll(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"device_code"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := h.security.PollDevice(r.Context(), in.Code)
	if errors.Is(err, auth.ErrRateLimited) {
		w.Header().Set("Retry-After", "5")
		writeError(w, 429, "slow_down", "Wait before checking for device approval again.", nil)
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (h *Handler) deviceInfo(w http.ResponseWriter, r *http.Request) {
	// Authorization can only be completed from a browser session, never another CLI.
	if r.Header.Get("Authorization") != "" {
		failure(w, auth.ErrForbidden)
		return
	}
	if _, err := h.auth.Current(r.Context(), token(r, h.sessionCookie)); err != nil {
		failure(w, err)
		return
	}
	d, err := h.security.DeviceInfo(r.Context(), r.URL.Query().Get("code"), h.address(r))
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"description": d.Description, "expires_at": d.ExpiresAt})
}
func (h *Handler) deviceApprove(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" {
		failure(w, auth.ErrForbidden)
		return
	}
	var in struct {
		Code    string `json:"code"`
		Approve bool   `json:"approve"`
		Subject string `json:"account_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := h.security.ApproveDevice(r.Context(), token(r, h.sessionCookie), in.Code, h.address(r), in.Approve, in.Subject); err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"approved": in.Approve})
}
