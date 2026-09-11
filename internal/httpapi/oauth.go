package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"acta/internal/auth"
)

func (h *Handler) mcpResource() string { return h.config.PublicURL + "/mcp" }

func (h *Handler) oauthMetadata(w http.ResponseWriter, r *http.Request) {
	u := h.config.PublicURL
	writeJSON(w, 200, map[string]any{
		"issuer": u, "authorization_endpoint": u + "/oauth/authorize",
		"token_endpoint": u + "/oauth/token", "registration_endpoint": u + "/oauth/register",
		"revocation_endpoint":                            u + "/oauth/revoke",
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"revocation_endpoint_auth_methods_supported":     []string{"none"},
		"scopes_supported":                               []string{auth.OAuthScope},
		"authorization_response_iss_parameter_supported": true,
	})
}
func (h *Handler) protectedResource(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"resource": h.mcpResource(), "resource_name": "Acta",
		"authorization_servers": []string{h.config.PublicURL}, "scopes_supported": []string{auth.OAuthScope},
		"bearer_methods_supported": []string{"header"}})
}

// OAuth uses its standard, flat error envelope; the browser API keeps the
// regular Acta error format so shared sign-in and MFA handling still applies.
func oauthFailure(w http.ResponseWriter, err error) {
	status, code, description := 400, "invalid_grant", "This authorization is invalid, expired or revoked. Connect again."
	var oauth *auth.OAuthError
	switch {
	case errors.As(err, &oauth):
		code, description = oauth.Code, oauth.Description
	case errors.Is(err, auth.ErrRateLimited):
		status, code, description = 429, "temporarily_unavailable", "Too many attempts. Try again in five minutes."
		w.Header().Set("Retry-After", "300")
	case errors.Is(err, auth.ErrUnauthenticated), errors.Is(err, auth.ErrAccountDisabled), errors.Is(err, auth.ErrMFARequired):
	default:
		slog.Error("OAuth request failed", "error", err)
		status, code, description = 503, "temporarily_unavailable", "Acta could not complete this request. Try again."
	}
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}
func (h *Handler) oauthRegister(w http.ResponseWriter, r *http.Request) {
	var in auth.OAuthClient
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	d := json.NewDecoder(r.Body)
	// RFC 7591 requires ignoring unrecognized client metadata.
	if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
		oauthFailure(w, &auth.OAuthError{Code: "invalid_client_metadata", Description: "Send one client metadata object."})
		return
	}
	out, err := h.security.RegisterOAuthClient(r.Context(), in, h.address(r))
	if err != nil {
		oauthFailure(w, err)
		return
	}
	writeJSON(w, 201, out)
}
func (h *Handler) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	binding, err := h.binding(w, r)
	if err != nil {
		oauthFailure(w, err)
		return
	}
	in, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		oauthFailure(w, &auth.OAuthError{Code: "invalid_request", Description: "Invalid authorization parameters."})
		return
	}
	id, err := h.security.BeginOAuth(r.Context(), in, binding, h.address(r), h.mcpResource())
	if err != nil {
		oauthFailure(w, err)
		return
	}
	http.Redirect(w, r, "/login/oauth?request="+url.QueryEscape(id), http.StatusSeeOther)
}
func (h *Handler) oauthConsent(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" {
		failure(w, auth.ErrForbidden)
		return
	}
	if _, err := h.auth.Current(r.Context(), token(r, h.sessionCookie)); err != nil {
		failure(w, err)
		return
	}
	req, err := h.security.OAuthConsent(r.Context(), r.URL.Query().Get("request"), token(r, h.bindingCookie))
	if err != nil {
		var oe *auth.OAuthError
		if errors.As(err, &oe) {
			writeError(w, 410, "authorization_expired", "This connection request is invalid or expired. Start again from your MCP client.", nil)
		} else {
			failure(w, err)
		}
		return
	}
	writeJSON(w, 200, map[string]any{"client_name": req.ClientName, "expires_at": req.ExpiresAt})
}
func (h *Handler) oauthApprove(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" {
		failure(w, auth.ErrForbidden)
		return
	}
	var in struct {
		Request string   `json:"request"`
		Approve bool     `json:"approve"`
		Subject string   `json:"account_id"`
		Tools   []string `json:"tools"`
	}
	if !decode(w, r, &in) {
		return
	}
	redirect, err := h.security.ApproveOAuthTools(r.Context(), token(r, h.sessionCookie), in.Request, token(r, h.bindingCookie), in.Approve, h.config.PublicURL, in.Subject, in.Tools)
	if err != nil {
		var oe *auth.OAuthError
		if errors.As(err, &oe) {
			writeError(w, 410, "authorization_expired", "This connection request is invalid or expired. Start again from your MCP client.", nil)
		} else {
			failure(w, err)
		}
		return
	}
	writeJSON(w, 200, map[string]string{"redirect": redirect})
}
func oauthForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if r.Header.Get("Authorization") != "" || r.ParseForm() != nil {
		oauthFailure(w, &auth.OAuthError{Code: "invalid_request", Description: "Send form parameters for a public OAuth client."})
		return false
	}
	for _, v := range r.PostForm {
		if len(v) != 1 {
			oauthFailure(w, &auth.OAuthError{Code: "invalid_request", Description: "Duplicate form parameters."})
			return false
		}
	}
	return true
}
func (h *Handler) oauthToken(w http.ResponseWriter, r *http.Request) {
	if !oauthForm(w, r) {
		return
	}
	out, err := h.security.ExchangeOAuth(r.Context(), r.PostForm, h.mcpResource(), h.address(r))
	if err != nil {
		oauthFailure(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (h *Handler) oauthRevoke(w http.ResponseWriter, r *http.Request) {
	if !oauthForm(w, r) {
		return
	}
	if r.PostForm.Get("token") == "" || r.PostForm.Get("client_id") == "" {
		oauthFailure(w, &auth.OAuthError{Code: "invalid_request", Description: "A token and client_id are required."})
		return
	}
	if err := h.security.RevokeOAuth(r.Context(), r.PostForm.Get("token"), r.PostForm.Get("client_id"), h.address(r)); err != nil {
		oauthFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
