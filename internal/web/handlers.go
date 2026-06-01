package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/peios/acta/internal/authn"
	"github.com/peios/acta/internal/httpx"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/passkey"
	"github.com/peios/acta/internal/session"
	"github.com/peios/acta/internal/store"
)

type handlers struct {
	sessions *session.Manager
	provider authn.Provider
	passkeys *passkey.Service
	secure   bool
}

type loginData struct {
	Methods   []authn.Method
	CSRFToken string
	ReturnTo  string
	Err       string
	Nav       bool // always false: no top-bar nav when logged out
}

func (h *handlers) loginPage(w http.ResponseWriter, r *http.Request) {
	// Already signed in? Don't show the login page.
	if p, _ := h.sessions.Current(r.Context(), r); p != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	render(w, http.StatusOK, "login.html", loginData{
		Methods:   h.provider.Methods(),
		CSRFToken: csrfTokenFrom(r.Context()),
		ReturnTo:  httpx.SafeReturnTo(r.URL.Query().Get("return_to")),
		Err:       loginError(r.URL.Query().Get("err")),
	})
}

// loginError maps a known error code to a fixed message. Only recognised
// codes produce text, so nothing from the query string is reflected verbatim.
func loginError(code string) string {
	switch code {
	case "invalid_credentials":
		return "Incorrect username or password."
	default:
		return ""
	}
}

func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.sessions.Destroy(r.Context(), w, r); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

type homeData struct {
	Principal *identity.Principal
	CSRFToken string
	Nav       bool
}

func (h *handlers) home(w http.ResponseWriter, r *http.Request) {
	render(w, http.StatusOK, "home.html", homeData{
		Principal: principalFrom(r.Context()),
		CSRFToken: csrfTokenFrom(r.Context()),
		Nav:       true,
	})
}

// --- settings / passkeys ---

type securityData struct {
	Principal   *identity.Principal
	CSRFToken   string
	Credentials []store.Credential
	Nav         bool
}

func (h *handlers) settingsIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/settings/security", http.StatusSeeOther)
}

func (h *handlers) settingsSecurity(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	creds, err := h.passkeys.List(r.Context(), p.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "security.html", securityData{
		Principal:   p,
		CSRFToken:   csrfTokenFrom(r.Context()),
		Credentials: creds,
		Nav:         true,
	})
}

func (h *handlers) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	options, challengeID, err := h.passkeys.BeginRegistration(r.Context(), *p)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	httpx.SetChallengeCookie(w, challengeID, h.secure)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(options)
}

func (h *handlers) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	challengeID := httpx.ChallengeCookieValue(r)
	httpx.ClearChallengeCookie(w, h.secure)
	if challengeID == "" {
		http.Error(w, "no ceremony in progress", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if err := h.passkeys.FinishRegistration(r.Context(), *p, challengeID, r, name); err != nil {
		http.Error(w, "passkey registration failed", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) passkeyDelete(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	id := r.PathValue("id")
	if err := h.passkeys.Delete(r.Context(), id, p.ID); err != nil && !errors.Is(err, store.ErrCredentialNotFound) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings/security", http.StatusSeeOther)
}

type welcomeData struct {
	Principal *identity.Principal
	CSRFToken string
	ReturnTo  string
	Nav       bool
}

func (h *handlers) welcomePasskey(w http.ResponseWriter, r *http.Request) {
	render(w, http.StatusOK, "welcome.html", welcomeData{
		Principal: principalFrom(r.Context()),
		CSRFToken: csrfTokenFrom(r.Context()),
		ReturnTo:  httpx.SafeReturnTo(r.URL.Query().Get("return_to")),
		Nav:       true,
	})
}
