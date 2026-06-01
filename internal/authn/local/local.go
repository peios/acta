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
	"github.com/peios/acta/internal/session"
	"github.com/peios/acta/internal/store"
)

type Provider struct {
	store    store.Store
	sessions *session.Manager
	// dummyHash is verified against when the username is unknown, so a failed
	// login takes the same time whether or not the account exists — closing
	// the timing side-channel for username enumeration.
	dummyHash string
}

func NewProvider(st store.Store, sessions *session.Manager) *Provider {
	dummy, _ := HashPassword("not-a-real-password")
	return &Provider{store: st, sessions: sessions, dummyHash: dummy}
}

func (p *Provider) Name() string { return "local" }

func (p *Provider) Methods() []authn.Method {
	return []authn.Method{{ID: "password", Label: "Password"}}
}

func (p *Provider) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /login/password", p.handlePassword)
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
	if err := p.sessions.Establish(r.Context(), w, principal); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, returnTo, http.StatusSeeOther)
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
