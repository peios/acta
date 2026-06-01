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
	"github.com/peios/acta/internal/workspace"
)

type handlers struct {
	sessions   *session.Manager
	provider   authn.Provider
	passkeys   *passkey.Service
	workspaces *workspace.Service
	secure     bool
}

// chrome is the shared top-bar context every signed-in page needs: the CSRF
// token, the contents of the workspace switcher, and which nav section is
// active. Page data structs embed it so templates can reach these fields
// directly (e.g. .CSRFToken, .Workspaces) via field promotion.
type chrome struct {
	CSRFToken  string
	Nav        bool
	Section    string // "home" | "settings" — drives the nav highlight
	Workspaces []store.Workspace
	Workspace  *store.Workspace // the currently-selected workspace
}

// chromeFor builds the top-bar context. current is the workspace the page is
// scoped to (the /w/{slug} landing); pass nil for pages that aren't tied to one
// (settings, the welcome interstitial) and the last-viewed or first workspace
// is used instead.
func (h *handlers) chromeFor(r *http.Request, section string, current *store.Workspace) (chrome, error) {
	list, err := h.workspaces.List(r.Context())
	if err != nil {
		return chrome{}, err
	}
	if current == nil {
		current = pickWorkspace(list, httpx.WorkspaceCookieValue(r))
	}
	return chrome{
		CSRFToken:  csrfTokenFrom(r.Context()),
		Nav:        true,
		Section:    section,
		Workspaces: list,
		Workspace:  current,
	}, nil
}

// pickWorkspace selects the default workspace for pages not scoped to one: the
// cookie's slug if it still resolves, otherwise the first (oldest) workspace.
func pickWorkspace(list []store.Workspace, slug string) *store.Workspace {
	if slug != "" {
		for i := range list {
			if list[i].Slug == slug {
				return &list[i]
			}
		}
	}
	if len(list) > 0 {
		return &list[0]
	}
	return nil
}

// --- login / logout ---

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

// --- workspace landing ---

type workspaceData struct {
	chrome
	Principal *identity.Principal
}

// rootRedirect sends "/" to the user's current workspace. The canonical URL for
// a workspace is /w/{slug}; "/" is just a convenience entry point.
func (h *handlers) rootRedirect(w http.ResponseWriter, r *http.Request) {
	list, err := h.workspaces.List(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	current := pickWorkspace(list, httpx.WorkspaceCookieValue(r))
	if current == nil {
		// No workspaces exist (shouldn't happen — one is seeded, and the last
		// can't be deleted). Send the user somewhere they can make one.
		http.Redirect(w, r, "/settings/workspaces", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/w/"+current.Slug, http.StatusSeeOther)
}

func (h *handlers) workspaceHome(w http.ResponseWriter, r *http.Request) {
	ws, err := h.workspaces.BySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, store.ErrWorkspaceNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	httpx.SetWorkspaceCookie(w, ws.Slug, h.secure)

	ch, err := h.chromeFor(r, "home", &ws)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "workspace.html", workspaceData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
	})
}

// --- settings: security ---

type securityData struct {
	chrome
	Principal   *identity.Principal
	Credentials []store.Credential
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
	ch, err := h.chromeFor(r, "settings", nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "security.html", securityData{
		chrome:      ch,
		Principal:   p,
		Credentials: creds,
	})
}

// --- settings: workspaces ---

type workspacesData struct {
	chrome
	Principal *identity.Principal
	Err       string
}

func (h *handlers) settingsWorkspaces(w http.ResponseWriter, r *http.Request) {
	ch, err := h.chromeFor(r, "settings", nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "workspaces.html", workspacesData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		Err:       workspaceError(r.URL.Query().Get("err")),
	})
}

func (h *handlers) workspaceCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p := principalFrom(r.Context())
	if _, err := h.workspaces.Create(r.Context(), r.PostFormValue("name"), p.ID); err != nil {
		redirectWorkspaceErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings/workspaces", http.StatusSeeOther)
}

func (h *handlers) workspaceRename(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	err := h.workspaces.Rename(r.Context(), r.PathValue("id"), r.PostFormValue("name"))
	if err != nil && !errors.Is(err, store.ErrWorkspaceNotFound) {
		redirectWorkspaceErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings/workspaces", http.StatusSeeOther)
}

func (h *handlers) workspaceDelete(w http.ResponseWriter, r *http.Request) {
	err := h.workspaces.Delete(r.Context(), r.PathValue("id"))
	switch {
	case err == nil, errors.Is(err, store.ErrWorkspaceNotFound):
		http.Redirect(w, r, "/settings/workspaces", http.StatusSeeOther)
	case errors.Is(err, workspace.ErrLastWorkspace):
		http.Redirect(w, r, "/settings/workspaces?err=last", http.StatusSeeOther)
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// redirectWorkspaceErr bounces back to the workspaces page with a known error
// code. Only recognised errors map to a code; anything else is a 500.
func redirectWorkspaceErr(w http.ResponseWriter, r *http.Request, err error) {
	var code string
	switch {
	case errors.Is(err, workspace.ErrInvalidName):
		code = "invalid_name"
	case errors.Is(err, store.ErrWorkspaceNameTaken):
		code = "name_taken"
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings/workspaces?err="+code, http.StatusSeeOther)
}

func workspaceError(code string) string {
	switch code {
	case "invalid_name":
		return "Enter a name (1–60 characters)."
	case "name_taken":
		return "A workspace with that name already exists."
	case "last":
		return "You can't delete your only workspace."
	default:
		return ""
	}
}

// --- settings: passkeys ---

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

// --- welcome interstitial ---

type welcomeData struct {
	chrome
	Principal *identity.Principal
	ReturnTo  string
}

func (h *handlers) welcomePasskey(w http.ResponseWriter, r *http.Request) {
	ch, err := h.chromeFor(r, "home", nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "welcome.html", welcomeData{
		chrome:    ch,
		Principal: principalFrom(r.Context()),
		ReturnTo:  httpx.SafeReturnTo(r.URL.Query().Get("return_to")),
	})
}
