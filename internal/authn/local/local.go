// Package local is the local-accounts authentication provider: it owns the
// users table and verifies username/password credentials with argon2id. This
// is the provider used on Debian. On Peios it would be swapped for a
// kernel-mediated provider, with no change to the session/web layers.
package local

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/peios/acta/internal/authn"
	"github.com/peios/acta/internal/httpx"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/passkey"
	"github.com/peios/acta/internal/session"
	"github.com/peios/acta/internal/store"
)

type Provider struct {
	store    store.Store
	sessions *session.Manager
	passkeys *passkey.Service
	secure   bool
	// dummyHash is verified against when the username is unknown, so a failed
	// login takes the same time whether or not the account exists — closing
	// the timing side-channel for username enumeration.
	dummyHash string
}

func NewProvider(st store.Store, sessions *session.Manager, passkeys *passkey.Service, secure bool) *Provider {
	dummy, _ := HashPassword("not-a-real-password")
	return &Provider{store: st, sessions: sessions, passkeys: passkeys, secure: secure, dummyHash: dummy}
}

func (p *Provider) Name() string { return "local" }

func (p *Provider) Methods() []authn.Method {
	return []authn.Method{
		{ID: "password", Label: "Password"},
		{ID: "passkey", Label: "Passkey"},
	}
}

func (p *Provider) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /login/password", p.handlePassword)
	mux.HandleFunc("POST /login/passkey/begin", p.handlePasskeyBegin)
	mux.HandleFunc("POST /login/passkey/finish", p.handlePasskeyFinish)
}

func (p *Provider) handlePassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	username := NormalizeUsername(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	returnTo := httpx.SafeReturnTo(r.PostFormValue("return_to"))

	u, err := p.store.UserByUsername(r.Context(), username)
	if err != nil {
		// Unknown user (or lookup error): burn the same time as a real
		// verify, then fail generically. No account-existence leak.
		_, _ = VerifyPassword(p.dummyHash, password)
		p.fail(w, r, returnTo)
		return
	}

	ok, err := VerifyPassword(u.PasswordHash, password)
	if err != nil || !ok {
		p.fail(w, r, returnTo)
		return
	}

	principal := identity.Principal{ID: u.ID, Username: u.Username, Display: u.Display}
	if err := p.sessions.EstablishWithRequest(r.Context(), w, r, principal); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Nudge first-time users to add a passkey, but only if they have none.
	if has, err := p.passkeys.HasCredentials(r.Context(), u.ID); err == nil && !has {
		q := url.Values{}
		if returnTo != "/" {
			q.Set("return_to", returnTo)
		}
		http.Redirect(w, r, "/welcome/passkey?"+q.Encode(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, returnTo, http.StatusSeeOther)
}

// handlePasskeyBegin starts a usernameless assertion and returns the options
// as JSON for the browser's navigator.credentials.get().
func (p *Provider) handlePasskeyBegin(w http.ResponseWriter, r *http.Request) {
	options, challengeID, err := p.passkeys.BeginLogin(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	httpx.SetChallengeCookie(w, challengeID, p.secure)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(options)
}

// handlePasskeyFinish validates the assertion, establishes a session, and
// returns 204 — the browser script then navigates to the return target.
func (p *Provider) handlePasskeyFinish(w http.ResponseWriter, r *http.Request) {
	challengeID := httpx.ChallengeCookieValue(r)
	httpx.ClearChallengeCookie(w, p.secure)
	if challengeID == "" {
		http.Error(w, "no ceremony in progress", http.StatusBadRequest)
		return
	}
	principal, err := p.passkeys.FinishLogin(r.Context(), challengeID, r)
	if err != nil {
		http.Error(w, "passkey login failed", http.StatusUnauthorized)
		return
	}
	if err := p.sessions.EstablishWithRequest(r.Context(), w, r, principal); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fail redirects back to the login page with a generic error, preserving the
// intended destination so a successful retry still lands where the user meant.
func (p *Provider) fail(w http.ResponseWriter, r *http.Request, returnTo string) {
	q := url.Values{}
	q.Set("err", "invalid_credentials")
	if returnTo != "/" {
		q.Set("return_to", returnTo)
	}
	http.Redirect(w, r, "/login?"+q.Encode(), http.StatusSeeOther)
}

// NormalizeUsername trims and lowercases, matching how usernames are stored.
func NormalizeUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

var _ authn.Provider = (*Provider)(nil)
