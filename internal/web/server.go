// Package web wires the HTTP surface: routes, middleware, and handlers. It
// depends only on the session manager and an authn.Provider — never on the
// store or a specific provider implementation.
package web

import (
	"net/http"
	"strings"

	"github.com/peios/acta/internal/account"
	"github.com/peios/acta/internal/agent"
	"github.com/peios/acta/internal/apitoken"
	"github.com/peios/acta/internal/authn"
	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/config"
	"github.com/peios/acta/internal/live"
	"github.com/peios/acta/internal/mcpcfg"
	"github.com/peios/acta/internal/passkey"
	"github.com/peios/acta/internal/push"
	"github.com/peios/acta/internal/session"
	"github.com/peios/acta/internal/workspace"
)

// maxBodyBytes caps the request body for every route. Acta's payloads — form
// posts, small JSON mutations, MCP tool calls — are tiny, so a 1 MiB ceiling is
// generous while still bounding memory against an oversized or hung upload.
const maxBodyBytes = 1 << 20

// NewHandler builds the application handler.
func NewHandler(cfg config.Config, sessions *session.Manager, provider authn.Provider, passkeys *passkey.Service, tokens *apitoken.Service, agents *agent.Service, accounts *account.Service, workspaces *workspace.Service, boards *board.Service, mcpConfig *mcpcfg.Service, pushSender *push.Sender) http.Handler {
	h := &handlers{
		sessions:   sessions,
		provider:   provider,
		passkeys:   passkeys,
		tokens:     tokens,
		agents:     agents,
		accounts:   accounts,
		workspaces: workspaces,
		board:      boards,
		mcpcfg:     mcpConfig,
		live:       live.NewHub(),
		push:       pushSender,
		secure:     cfg.CookieSecure(),
		publicURL:  strings.TrimRight(cfg.RPOrigin, "/"),
	}
	// Attach the live bell as a board notifier now that the hub exists, so
	// subscription notifications (filed deep in the board) reach the bell over
	// SSE the same way Web Push reaches it out of band.
	boards.AddNotifier(newLiveNotifier(h.live, boards))
	mux := http.NewServeMux()

	// Public routes.
	mux.HandleFunc("GET /login", h.loginPage)
	provider.Mount(mux) // POST /login/password, /login/passkey/{begin,finish}
	mux.HandleFunc("POST /logout", h.logout)

	// Protected routes. "/{$}" matches exactly "/".
	protected := func(fn http.HandlerFunc) http.Handler {
		return requireAuth(sessions)(fn)
	}
	mux.Handle("GET /{$}", protected(h.rootRedirect))

	// Live updates: one Server-Sent Events stream per page. A literal first
	// segment, so it's more specific than /{slug} and never shadows a board (and
	// "events" is a reserved slug, so no workspace can claim it).
	mux.Handle("GET /events", protected(h.events))

	// A workspace's board and its JSON mutation API (consumed by board.js). The
	// board lives at the path root (/{slug}); slugs that would shadow a built-in
	// route are reserved (see workspace.reservedSlugs).
	mux.Handle("GET /{slug}", protected(h.boardPage))
	// A bare /{slug}/activity and /{slug}/archive are workspace-wide; a ?board=
	// query scopes them to one board (the header toolbar always passes it). They
	// stay a query rather than a third path segment because a two-wildcard prefix
	// like /{slug}/{board}/activity collides with the literal /account, /settings
	// trees (Go's mux can't rank them).
	mux.Handle("GET /{slug}/archive", protected(h.archivePage))
	mux.Handle("GET /{slug}/activity", protected(h.activityPage))
	// Projects: a workspace's cross-cutting initiatives. The literal
	// /{slug}/projects is more specific than the /{slug}/{board} board view, so it
	// wins. The single-project view rides a ?p=<slug> query rather than a third
	// path segment: a /{slug}/projects/{project} pattern would be ambiguous with
	// /notifications/{id}/open (both match /notifications/projects/open) — the same
	// reason the activity/archive feeds use ?board=. Mutations keep the id in the
	// path (a 4-segment POST collides with nothing).
	mux.Handle("GET /{slug}/projects", protected(h.projects))
	mux.Handle("POST /{slug}/projects", protected(h.projectCreate))
	mux.Handle("POST /{slug}/projects/{id}/edit", protected(h.projectUpdate))
	mux.Handle("POST /{slug}/projects/{id}/archive", protected(h.projectArchive))
	mux.Handle("POST /{slug}/projects/{id}/unarchive", protected(h.projectUnarchive))
	mux.Handle("POST /{slug}/projects/{id}/subscribe", protected(h.projectSubscribe))
	// Releases: a workspace's versioned cut-lines. Same routing shape as projects —
	// /{slug}/releases is more specific than /{slug}/{board}; the single-release
	// view rides ?r=<id>; mutations keep the id in the path.
	mux.Handle("GET /{slug}/releases", protected(h.releases))
	mux.Handle("POST /{slug}/releases", protected(h.releaseCreate))
	mux.Handle("POST /{slug}/releases/{id}/edit", protected(h.releaseUpdate))
	mux.Handle("POST /{slug}/releases/{id}/status", protected(h.releaseSetStatus))
	mux.Handle("POST /{slug}/releases/{id}/delete", protected(h.releaseDelete))
	// A second path segment selects a non-default board (e.g. /{slug}/backlog);
	// the literal sub-routes above are more specific, so they always win.
	mux.Handle("GET /{slug}/{board}", protected(h.boardPage))
	mux.Handle("POST /{slug}/statuses", protected(h.statusCreate))
	mux.Handle("POST /{slug}/statuses/reorder", protected(h.statusReorder))
	mux.Handle("POST /{slug}/statuses/{id}/rename", protected(h.statusRename))
	mux.Handle("POST /{slug}/statuses/{id}/color", protected(h.statusColor))
	mux.Handle("POST /{slug}/statuses/{id}/delete", protected(h.statusDelete))
	mux.Handle("GET /{slug}/statuses/{id}/checklist", protected(h.statusChecklist))
	mux.Handle("POST /{slug}/statuses/{id}/checklist", protected(h.statusChecklistSave))
	mux.Handle("POST /{slug}/milestones/reorder", protected(h.milestoneReorder))
	mux.Handle("POST /{slug}/items", protected(h.itemCreate))
	mux.Handle("GET /{slug}/items/{id}/modal", protected(h.itemModal))
	mux.Handle("GET /{slug}/mentionables", protected(h.mentionables))
	mux.Handle("POST /{slug}/items/{id}/rename", protected(h.itemRename))
	mux.Handle("POST /{slug}/items/{id}/move", protected(h.itemMove))
	mux.Handle("POST /{slug}/items/{id}/board", protected(h.itemSetBoard))
	mux.Handle("POST /{slug}/items/{id}/description", protected(h.itemDescription))
	mux.Handle("POST /{slug}/items/{id}/assignee", protected(h.itemAssignee))
	mux.Handle("POST /{slug}/items/{id}/status", protected(h.itemSetStatus))
	mux.Handle("POST /{slug}/items/{id}/facts/{fact}", protected(h.itemFactToggle))
	mux.Handle("POST /{slug}/items/{id}/pending/force", protected(h.itemPendingForce))
	mux.Handle("POST /{slug}/items/{id}/pending/cancel", protected(h.itemPendingCancel))
	mux.Handle("POST /{slug}/items/{id}/comment", protected(h.itemComment))
	mux.Handle("POST /{slug}/items/{id}/comment/{cid}/edit", protected(h.itemCommentEdit))
	mux.Handle("POST /{slug}/items/{id}/comment/{cid}/delete", protected(h.itemCommentDelete))
	mux.Handle("POST /{slug}/items/{id}/parent", protected(h.itemParent))
	mux.Handle("POST /{slug}/items/{id}/project", protected(h.itemSetProject))
	mux.Handle("POST /{slug}/items/{id}/release", protected(h.itemSetRelease))
	mux.Handle("POST /{slug}/items/{id}/convert-release", protected(h.itemConvertToRelease))
	mux.Handle("POST /{slug}/items/{id}/subscribe", protected(h.itemSubscribe))
	mux.Handle("POST /{slug}/items/{id}/milestone", protected(h.itemMilestone))
	mux.Handle("POST /{slug}/items/{id}/subtasks", protected(h.subtaskCreate))
	mux.Handle("POST /{slug}/items/{id}/subtasks/reorder", protected(h.subtaskReorder))
	mux.Handle("POST /{slug}/items/{id}/archive", protected(h.itemArchive))
	mux.Handle("POST /{slug}/items/{id}/unarchive", protected(h.itemUnarchive))
	mux.Handle("POST /{slug}/items/{id}/delete", protected(h.itemDelete))

	// Notification bell. Open marks one read then redirects to the item;
	// read-all clears the inbox. Both are workspace-agnostic (a notification
	// can point anywhere), so they live outside the /{slug} tree.
	mux.Handle("GET /notifications/{id}/open", protected(h.notificationOpen))
	mux.Handle("POST /notifications/read-all", protected(h.notificationsReadAll))

	// Account (user-specific) settings, reached from the top-bar account menu:
	// the things that belong to *you* — sign-in security and your agents.
	mux.Handle("GET /account", protected(h.accountIndex))
	mux.Handle("GET /account/security", protected(h.account))
	mux.Handle("POST /account/passkeys/register/begin", protected(h.passkeyRegisterBegin))
	mux.Handle("POST /account/passkeys/register/finish", protected(h.passkeyRegisterFinish))
	mux.Handle("POST /account/passkeys/{id}/delete", protected(h.passkeyDelete))
	mux.Handle("POST /account/tokens", protected(h.tokenCreate))
	mux.Handle("POST /account/tokens/{id}/delete", protected(h.tokenDelete))
	mux.Handle("POST /account/sessions/revoke-others", protected(h.sessionRevokeOthers))
	mux.Handle("POST /account/sessions/{id}/revoke", protected(h.sessionRevoke))
	// Web Push: register/forget this browser's subscription (fetch + CSRF).
	mux.Handle("POST /account/push/subscribe", protected(h.pushSubscribe))
	mux.Handle("POST /account/push/unsubscribe", protected(h.pushUnsubscribe))
	mux.Handle("GET /account/agents", protected(h.accountAgents))
	mux.Handle("POST /account/agents", protected(h.agentCreate))
	mux.Handle("GET /account/agents/{id}", protected(h.agentDetail))
	mux.Handle("POST /account/agents/{id}/subscribe", protected(h.agentSubscribe))
	mux.Handle("POST /account/agents/{id}/delete", protected(h.agentDelete))
	mux.Handle("POST /account/agents/{id}/tokens", protected(h.agentTokenCreate))
	mux.Handle("POST /account/agents/{id}/tokens/{tokenID}/delete", protected(h.agentTokenDelete))

	// Global (admin/workspace) settings, reached from the top-bar Settings link.
	mux.Handle("GET /settings", protected(h.settingsIndex))
	mux.Handle("GET /settings/workspaces", protected(h.settingsWorkspaces))
	mux.Handle("POST /settings/workspaces", protected(h.workspaceCreate))
	mux.Handle("POST /settings/workspaces/{id}/rename", protected(h.workspaceRename))
	mux.Handle("POST /settings/workspaces/{id}/delete", protected(h.workspaceDelete))
	mux.Handle("GET /settings/principals", protected(h.settingsPrincipals))
	mux.Handle("POST /settings/principals", protected(h.principalCreate))
	mux.Handle("POST /settings/principals/{id}/disable", protected(h.principalDisable))
	mux.Handle("POST /settings/principals/{id}/enable", protected(h.principalEnable))
	// MCP customisation: the guide (acta://guide) and user-defined prompts.
	mux.Handle("GET /settings/guide", protected(h.settingsGuide))
	mux.Handle("POST /settings/guide", protected(h.guideSave))
	mux.Handle("GET /settings/prompts", protected(h.settingsPrompts))
	mux.Handle("GET /settings/prompts/new", protected(h.promptNew))
	mux.Handle("POST /settings/prompts", protected(h.promptCreate))
	mux.Handle("GET /settings/prompts/{id}", protected(h.promptEdit))
	mux.Handle("POST /settings/prompts/{id}", protected(h.promptUpdate))
	mux.Handle("POST /settings/prompts/{id}/delete", protected(h.promptDelete))
	mux.Handle("GET /welcome/passkey", protected(h.welcomePasskey))

	// CLI login (gh-style loopback): a browser page that mints a token and hands
	// it back to a local `acta login` listener. Cookie-authed + CSRF like any UI.
	mux.Handle("GET /cli/authorize", protected(h.cliAuthorize))
	mux.Handle("POST /cli/authorize", protected(h.cliAuthorizeSubmit))

	// JSON API, authenticated by personal access token (Bearer). It carries no
	// cookies, so it mounts outside the CSRF chain — the token is the auth.
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/me", h.apiMe)
	api.HandleFunc("POST /api/v1/logout", h.apiLogout)
	api.HandleFunc("GET /api/v1/workspaces", h.apiWorkspaces)
	api.HandleFunc("GET /api/v1/w/{slug}/items", h.apiListItems)
	api.HandleFunc("POST /api/v1/w/{slug}/items", h.apiCreateItem)
	api.HandleFunc("GET /api/v1/w/{slug}/items/{id}", h.apiItem)
	api.HandleFunc("POST /api/v1/w/{slug}/items/{id}/subtasks", h.apiCreateSubtask)
	api.HandleFunc("POST /api/v1/w/{slug}/items/{id}/transition", h.apiTransition)
	api.HandleFunc("POST /api/v1/w/{slug}/items/{id}/project", h.apiSetItemProject)
	api.HandleFunc("POST /api/v1/w/{slug}/items/{id}/release", h.apiSetItemRelease)
	api.HandleFunc("GET /api/v1/w/{slug}/projects", h.apiListProjects)
	api.HandleFunc("POST /api/v1/w/{slug}/projects", h.apiCreateProject)
	api.HandleFunc("GET /api/v1/w/{slug}/releases", h.apiListReleases)
	api.HandleFunc("POST /api/v1/w/{slug}/releases", h.apiCreateRelease)
	api.HandleFunc("POST /api/v1/w/{slug}/releases/{name}/status", h.apiSetReleaseStatus)
	api.HandleFunc("GET /api/v1/subscriptions", h.apiListSubscriptions)
	api.HandleFunc("POST /api/v1/subscriptions", h.apiSubscribe)
	api.HandleFunc("DELETE /api/v1/subscriptions", h.apiUnsubscribe)
	// Agent + token management, so the CLI can provision an MCP integration.
	api.HandleFunc("GET /api/v1/agents", h.apiListAgents)
	api.HandleFunc("POST /api/v1/agents", h.apiCreateAgent)
	api.HandleFunc("POST /api/v1/agents/{id}/tokens", h.apiCreateAgentToken)
	api.HandleFunc("POST /api/v1/tokens", h.apiCreateSelfToken)

	// Model Context Protocol endpoint (Streamable HTTP). Like the REST API it is
	// Bearer-authed and carries no cookies, so it mounts outside CSRF.
	mcpEndpoint := requireToken(tokens)(h.mcpHandler())

	// Top-level dispatch: token-auth (no CSRF) for the API and MCP, cookie + CSRF
	// for the browser UI. All share request logging and the security headers.
	// Static assets live here, not on the UI mux: a `/static/` subtree pattern
	// would conflict with the flat /{slug}/… board routes (both match e.g.
	// /static/archive) and make ServeMux panic at registration.
	root := http.NewServeMux()
	root.Handle("GET /static/", staticHandler())
	// The service worker is served from the root so it can control the whole
	// origin (a worker's scope can't exceed its own path). Public, like /static.
	root.HandleFunc("GET /sw.js", h.serviceWorker)
	root.Handle("/api/v1/", requireToken(tokens)(api))
	root.Handle("/mcp", mcpEndpoint)
	// Retired /w/{slug}/… URLs — boards used to live under /w. 301 them to the
	// new /{slug}/… home (path tail + query preserved) so old bookmarks and
	// shared permalinks keep resolving. It lives on the root mux, not the UI mux:
	// a /w/{…} pattern there would conflict with the flat /{slug}/… board routes
	// (both match e.g. /w/archive). GET-only and auth-free — a pure path rewrite
	// whose destination still enforces auth.
	root.HandleFunc("GET /w/{path...}", h.legacyWorkspaceRedirect)
	root.Handle("/", csrf(cfg.CookieSecure())(mux))

	// Outer chain (outermost first): log + tag the request, recover panics into
	// a clean 500, set security headers, cap the request body, then route.
	var chain http.Handler = http.MaxBytesHandler(root, maxBodyBytes)
	chain = secureHeaders(cfg.CookieSecure())(chain)
	chain = recoverPanic(chain)
	return requestLogger(cfg.TrustedProxies)(chain)
}
