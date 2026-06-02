package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/peios/acta/internal/agent"
	"github.com/peios/acta/internal/apitoken"
	"github.com/peios/acta/internal/authn"
	"github.com/peios/acta/internal/board"
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
	tokens     *apitoken.Service
	agents     *agent.Service
	workspaces *workspace.Service
	board      *board.Service
	secure     bool
	publicURL  string // browser-facing origin, for building item permalinks
}

// tokensView is the data a token-management section renders from. The same
// partial drives a human's own tokens (on the Security page) and an agent's
// tokens (on the agent detail page) — only the action URLs differ. NewToken is
// a freshly minted plaintext to reveal exactly once.
type tokensView struct {
	CSRFToken    string
	Tokens       []store.APIToken
	NewToken     string
	CreateAction string // POST target to mint a token
	DeleteBase   string // revoke posts to DeleteBase + "/{id}/delete"
	Placeholder  string
}

// chrome is the shared top-bar context every signed-in page needs: the CSRF
// token, the contents of the workspace switcher, and which nav section is
// active. Page data structs embed it so templates can reach these fields
// directly (e.g. .CSRFToken, .Workspaces) via field promotion.
type chrome struct {
	CSRFToken  string
	Nav        bool
	Section    string // "home" | "settings" | "account" — drives the nav highlight
	Display    string // the signed-in user's display name, shown in the account menu
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
	who := ""
	if p := principalFrom(r.Context()); p != nil {
		who = p.Display
	}
	return chrome{
		CSRFToken:  csrfTokenFrom(r.Context()),
		Nav:        true,
		Section:    section,
		Display:    who,
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
	case "too_many":
		return "Too many attempts. Please wait a few minutes and try again."
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

// (The workspace landing at /w/{slug} is the board — see board.go.)

// --- account: security ---

type securityData struct {
	chrome
	Principal    *identity.Principal
	Credentials  []store.Credential
	TokenSection tokensView
	Sessions     []sessionView
}

// sessionView is a session as shown in the account UI. The secret token is
// deliberately absent: a session is addressed only by its non-secret PublicID,
// so the bearer credential never reaches the page.
type sessionView struct {
	PublicID  string
	Label     string // friendly user-agent summary
	Current   bool   // the session making this request
	CreatedAt time.Time
	LastSeen  time.Time
}

func (h *handlers) settingsIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/settings/workspaces", http.StatusSeeOther)
}

// accountIndex lands the account menu on its first section.
func (h *handlers) accountIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/account/security", http.StatusSeeOther)
}

func (h *handlers) account(w http.ResponseWriter, r *http.Request) {
	data, err := h.buildSecurityData(r, "")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "account.html", data)
}

// buildSecurityData assembles the Security page: passkeys, API tokens, and
// active sessions (with the current one marked). newToken, when non-empty, is a
// freshly minted token's plaintext to reveal exactly once.
func (h *handlers) buildSecurityData(r *http.Request, newToken string) (securityData, error) {
	p := principalFrom(r.Context())
	creds, err := h.passkeys.List(r.Context(), p.ID)
	if err != nil {
		return securityData{}, err
	}
	tokens, err := h.tokens.List(r.Context(), p.ID)
	if err != nil {
		return securityData{}, err
	}
	sessions, err := h.sessions.List(r.Context(), p.ID)
	if err != nil {
		return securityData{}, err
	}
	current := h.sessions.CurrentToken(r)
	views := make([]sessionView, 0, len(sessions))
	for _, s := range sessions {
		views = append(views, sessionView{
			PublicID:  s.PublicID,
			Label:     userAgentLabel(s.UserAgent),
			Current:   s.ID == current,
			CreatedAt: s.CreatedAt,
			LastSeen:  s.LastSeen,
		})
	}
	ch, err := h.chromeFor(r, "account", nil)
	if err != nil {
		return securityData{}, err
	}
	return securityData{
		chrome:      ch,
		Principal:   p,
		Credentials: creds,
		TokenSection: tokensView{
			CSRFToken:    ch.CSRFToken,
			Tokens:       tokens,
			NewToken:     newToken,
			CreateAction: "/account/tokens",
			DeleteBase:   "/account/tokens",
			Placeholder:  "Token name (e.g. laptop CLI)",
		},
		Sessions: views,
	}, nil
}

// --- account: api tokens ---

func (h *handlers) tokenCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p := principalFrom(r.Context())
	plaintext, _, err := h.tokens.Mint(r.Context(), p.ID, r.PostFormValue("name"))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Re-render with the plaintext rather than redirecting: this is the only
	// moment the user can copy it, and a redirect would throw it away.
	data, err := h.buildSecurityData(r, plaintext)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "account.html", data)
}

func (h *handlers) tokenDelete(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	err := h.tokens.Revoke(r.Context(), r.PathValue("id"), p.ID)
	if err != nil && !errors.Is(err, store.ErrAPITokenNotFound) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/account/security", http.StatusSeeOther)
}

// --- account: sessions ---

func (h *handlers) sessionRevoke(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if err := h.sessions.Revoke(r.Context(), r.PathValue("id"), p.ID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/account/security", http.StatusSeeOther)
}

func (h *handlers) sessionRevokeOthers(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if _, err := h.sessions.RevokeOthers(r.Context(), p.ID, h.sessions.CurrentToken(r)); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/account/security", http.StatusSeeOther)
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
	ws, err := h.workspaces.Create(r.Context(), r.PostFormValue("name"), p.ID)
	if err != nil {
		redirectWorkspaceErr(w, r, err)
		return
	}
	// Give the new workspace its starter lanes so its board is usable at once.
	if err := h.board.SeedDefaults(r.Context(), ws.ID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
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

// --- account: passkeys ---

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
	http.Redirect(w, r, "/account/security", http.StatusSeeOther)
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
