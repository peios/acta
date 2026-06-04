package web

import (
	"net/http"
	"net/url"

	"github.com/peios/acta/internal/identity"
)

// The CLI login flow (gh-style loopback). `acta login` opens the browser to
// /cli/authorize with a loopback redirect_uri; the user — authenticated by the
// normal cookie session — approves, and we mint a token and redirect it back to
// the CLI's local listener. The security boundary is that we ONLY ever redirect
// a freshly minted token to a loopback address, so the endpoint can't be used
// to exfiltrate a token to an arbitrary host.

type cliAuthData struct {
	chrome
	Principal   *identity.Principal
	RedirectURI string
	State       string
	Label       string // proposed token name, e.g. "acta CLI @ hostname"
}

// defaultTokenLabel is used when the CLI sends no label.
const defaultTokenLabel = "acta CLI"

func (h *handlers) cliAuthorize(w http.ResponseWriter, r *http.Request) {
	redirectURI := r.URL.Query().Get("redirect_uri")
	if !isLoopbackRedirect(redirectURI) {
		http.Error(w, "invalid redirect_uri (must be a loopback address)", http.StatusBadRequest)
		return
	}
	// A focused consent screen reached by redirect right after `acta login`:
	// render it chrome-less (Nav stays false) so the only actions are Authorize
	// or Cancel — no sidebar to wander off into mid-approval. CSRFToken is still
	// threaded through for the form's hidden field and the <head> meta tag.
	render(w, http.StatusOK, "cli_authorize.html", cliAuthData{
		chrome:      chrome{CSRFToken: csrfTokenFrom(r.Context())},
		Principal:   principalFrom(r.Context()),
		RedirectURI: redirectURI,
		State:       r.URL.Query().Get("state"),
		Label:       tokenLabel(r.URL.Query().Get("label")),
	})
}

// tokenLabel falls back to a default when the CLI sends no label. Mint caps the
// length, so the raw value is safe to pass through.
func tokenLabel(label string) string {
	if label == "" {
		return defaultTokenLabel
	}
	return label
}

func (h *handlers) cliAuthorizeSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	redirectURI := r.PostFormValue("redirect_uri")
	state := r.PostFormValue("state")
	if !isLoopbackRedirect(redirectURI) {
		http.Error(w, "invalid redirect_uri (must be a loopback address)", http.StatusBadRequest)
		return
	}
	if r.PostFormValue("action") != "authorize" {
		http.Redirect(w, r, withQuery(redirectURI, url.Values{"error": {"access_denied"}, "state": {state}}), http.StatusSeeOther)
		return
	}
	p := principalFrom(r.Context())
	plaintext, _, err := h.tokens.Mint(r.Context(), p.ID, tokenLabel(r.PostFormValue("label")))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, withQuery(redirectURI, url.Values{"token": {plaintext}, "state": {state}}), http.StatusSeeOther)
}

// isLoopbackRedirect permits only http loopback targets — the linchpin that
// keeps a minted token from being redirected to an attacker-controlled host.
func isLoopbackRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return false
	}
	switch u.Hostname() {
	case "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}

func withQuery(base string, vals url.Values) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	for k, vs := range vals {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
