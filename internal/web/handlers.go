package web

import (
	"net/http"

	"github.com/peios/acta/internal/authn"
	"github.com/peios/acta/internal/httpx"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/session"
)

type handlers struct {
	sessions *session.Manager
	provider authn.Provider
}

type loginData struct {
	Methods   []authn.Method
	CSRFToken string
	ReturnTo  string
	Err       string
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
}

func (h *handlers) home(w http.ResponseWriter, r *http.Request) {
	render(w, http.StatusOK, "home.html", homeData{
		Principal: principalFrom(r.Context()),
		CSRFToken: csrfTokenFrom(r.Context()),
	})
}
