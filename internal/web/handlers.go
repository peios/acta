package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/peios/acta/internal/account"
	"github.com/peios/acta/internal/agent"
	"github.com/peios/acta/internal/agentsession"
	"github.com/peios/acta/internal/apitoken"
	"github.com/peios/acta/internal/authn"
	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/httpx"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/live"
	"github.com/peios/acta/internal/mcpcfg"
	"github.com/peios/acta/internal/memory"
	"github.com/peios/acta/internal/passkey"
	"github.com/peios/acta/internal/push"
	"github.com/peios/acta/internal/session"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/workspace"
)

type handlers struct {
	sessions      *session.Manager
	provider      authn.Provider
	passkeys      *passkey.Service
	tokens        *apitoken.Service
	agents        *agent.Service
	accounts      *account.Service
	workspaces    *workspace.Service
	board         *board.Service
	memories      *memory.Service
	mcpcfg        *mcpcfg.Service
	live          live.Broker  // fans mutations to browsers over SSE; nil disables live updates
	push          *push.Sender // Web Push delivery; nil disables push (no VAPID keys)
	agentSessions *agentsession.Service
	agentHub      *agentsession.Hub
	secure        bool
	publicURL     string // browser-facing origin, for building item permalinks
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
	Section    string // "home" | "activity" | "settings" | "account" — drives the nav highlight
	Display    string // the signed-in user's display name, shown in the account menu
	Workspaces []store.Workspace
	Workspace  *store.Workspace // the currently-selected workspace
	// Boards is the current workspace's boards, rendered as the sidebar nav
	// (Tasks, Backlog). ActiveBoard is the slug of the board the page is showing,
	// "" on pages not scoped to one — the sidebar highlights the match.
	Boards      []boardNav
	ActiveBoard string
	// Notification bell. Unread drives the badge; Notifications is the recent
	// slice the dropdown lists. Path is the current request URI, threaded into
	// the "mark all read" form so it can redirect back to this page.
	Unread        int
	Notifications []notifView
	Path          string
	// AgentMode swaps the sidebar body from the workspace nav to a list of the
	// user's agent sessions (the "My Agents" side of the sidebar's mode switch).
	// AgentSessions is that list; ActiveSession is the id of the session the
	// page is showing, "" on the list page.
	AgentMode     bool
	AgentSessions []agentSessionNav
	ActiveSession string
}

// agentSessionNav is one sidebar row in agent mode: the session's label and
// whether a harness currently holds it.
type agentSessionNav struct {
	ID      string
	Title   string
	Live    bool // held by a connected harness (resumable)
	Running bool // a process is running right now
}

// boardNav is one sidebar board link. Href is the board's canonical view URL —
// the bare /{workspace} for the default board, /{workspace}/{board} for the
// rest — and Slug drives the active-state match against chrome.ActiveBoard. ID
// is the drop target for promoting/demoting a card onto this board.
type boardNav struct {
	ID   string
	Name string
	Slug string
	Href string
}

// notifView is one row in the notification bell dropdown. URL points at the
// open-and-redirect endpoint, which marks the row read on click-through. Kind is
// the notification kind ("mention" | "activity") that selects how the line
// reads; Summary is the rendered phrase an activity row shows ("moved to Doing").
type notifView struct {
	ID      string
	Unread  bool
	Kind    string
	Actor   string
	Title   string
	Summary string
	Excerpt string
	When    string
	URL     string
}

// buildNotifViews turns stored notifications into bell rows, building each
// row's open URL with the item deep-link as a validated redirect target.
func buildNotifViews(notes []store.Notification) []notifView {
	out := make([]notifView, 0, len(notes))
	for _, n := range notes {
		to := "/"
		if n.Kind == store.NotificationSession && n.ItemID != "" {
			to = "/account/sessions/" + n.ItemID
		} else if n.WorkspaceSlug != "" && n.ItemID != "" {
			to = "/" + n.WorkspaceSlug + "?item=" + n.ItemID
		}
		out = append(out, notifView{
			ID:      n.ID,
			Unread:  n.ReadAt == nil,
			Kind:    n.Kind,
			Actor:   n.ActorName,
			Title:   n.ItemTitle,
			Summary: n.Summary,
			Excerpt: n.Excerpt,
			When:    formatWhen(n.CreatedAt),
			URL:     "/notifications/" + n.ID + "/open?to=" + url.QueryEscape(to),
		})
	}
	return out
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
	var boards []boardNav
	if current != nil {
		bs, err := h.board.Boards(r.Context(), current.ID)
		if err != nil {
			return chrome{}, err
		}
		for i, b := range bs {
			href := "/" + current.Slug // default board lives at the bare workspace URL
			if i > 0 {
				href = "/" + current.Slug + "/" + b.Slug
			}
			boards = append(boards, boardNav{ID: b.ID, Name: b.Name, Slug: b.Slug, Href: href})
		}
	}
	who := ""
	var unread int
	var notes []notifView
	if p := principalFrom(r.Context()); p != nil {
		who = p.Display
		if ns, err := h.board.Notifications(r.Context(), p.ID, 15); err == nil {
			notes = buildNotifViews(ns)
		}
		if n, err := h.board.UnreadCount(r.Context(), p.ID); err == nil {
			unread = n
		}
	}
	return chrome{
		CSRFToken:     csrfTokenFrom(r.Context()),
		Nav:           true,
		Section:       section,
		Display:       who,
		Workspaces:    list,
		Workspace:     current,
		Boards:        boards,
		Unread:        unread,
		Notifications: notes,
		Path:          r.URL.RequestURI(),
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
	case "account_disabled":
		return "This account has been disabled. Contact an administrator."
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
// a workspace is /{slug}; "/" is just a convenience entry point.
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
	http.Redirect(w, r, "/"+current.Slug, http.StatusSeeOther)
}

// legacyWorkspaceRedirect 301s the retired /w/{path...} URLs to their new
// /{path...} home, preserving the path tail and query string so old bookmarks,
// notification deep-links, and shared permalinks keep resolving. The {path...}
// wildcard captures the whole tail after /w/ (slug plus any sub-path) in one go.
func (h *handlers) legacyWorkspaceRedirect(w http.ResponseWriter, r *http.Request) {
	target := "/" + r.PathValue("path")
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// (The workspace landing at /{slug} is the board — see board.go.)

// --- account: security ---

type securityData struct {
	chrome
	Principal    *identity.Principal
	Credentials  []store.Credential
	TokenSection tokensView
	Sessions     []sessionView
	Push         pushSettings
}

// pushSettings drives the account page's notification toggle. Enabled is false
// when the server has no VAPID keys, in which case the section hides. VAPIDKey
// is the public key the browser needs to subscribe.
type pushSettings struct {
	Enabled  bool
	VAPIDKey string
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
		Push:     h.pushSettings(),
	}, nil
}

// pushSettings reports whether Web Push is available and, if so, the public key
// the browser subscribes with. A nil sender means push is disabled.
func (h *handlers) pushSettings() pushSettings {
	if h.push == nil {
		return pushSettings{}
	}
	return pushSettings{Enabled: true, VAPIDKey: h.push.PublicKey()}
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
	// One form drives the settings modal: "name" is the display label, the
	// optional "slug" re-slugs the workspace (blank/unchanged keeps the URL), and
	// "item_prefix" relabels its human ids (blank/unchanged keeps the prefix).
	err := h.workspaces.Update(r.Context(), r.PathValue("id"),
		r.PostFormValue("name"), r.PostFormValue("slug"), r.PostFormValue("item_prefix"))
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
	case errors.Is(err, workspace.ErrInvalidSlug):
		code = "invalid_slug"
	case errors.Is(err, workspace.ErrSlugReserved):
		code = "slug_reserved"
	case errors.Is(err, store.ErrWorkspaceSlugTaken):
		code = "slug_taken"
	case errors.Is(err, workspace.ErrInvalidPrefix):
		code = "invalid_prefix"
	case errors.Is(err, store.ErrWorkspacePrefixTaken):
		code = "prefix_taken"
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
	case "invalid_slug":
		return "Enter a URL slug with at least one letter or number."
	case "slug_reserved":
		return "That URL is reserved — pick another slug."
	case "slug_taken":
		return "A workspace already uses that URL slug."
	case "invalid_prefix":
		return "Enter an ID prefix with at least one letter or number."
	case "prefix_taken":
		return "Another workspace already uses that ID prefix."
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
	// A focused onboarding step, not an app page: render it chrome-less like the
	// login screen (Nav stays false) so there's no sidebar to wander off into
	// mid-flow. CSRFToken is still threaded through for the <meta> tag the
	// passkey-registration JS reads.
	render(w, http.StatusOK, "welcome.html", welcomeData{
		chrome:    chrome{CSRFToken: csrfTokenFrom(r.Context())},
		Principal: principalFrom(r.Context()),
		ReturnTo:  httpx.SafeReturnTo(r.URL.Query().Get("return_to")),
	})
}
