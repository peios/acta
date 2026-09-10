package httpapi

import "net/http"

func (h *Handler) securitySessionGrants(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AccountID string   `json:"account_id"`
		Previous  []string `json:"previous_grants"`
		Grants    []string `json:"tool_grants"`
	}
	if !decode(w, r, &in) {
		return
	}
	if e := h.security.AmendMCPGrants(r.Context(), token(r, h.sessionCookie), in.AccountID, r.PathValue("id"), in.Previous, in.Grants); e != nil {
		failure(w, e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}
