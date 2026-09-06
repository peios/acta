// Package web wires the HTTP surface: routes, middleware, and handlers. It
// depends only on the session manager and an authn.Provider — never on the
// store or a specific provider implementation.
package web

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/peios/acta/internal/account"
	"github.com/peios/acta/internal/agent"
	"github.com/peios/acta/internal/agentsession"
	"github.com/peios/acta/internal/apitoken"
	"github.com/peios/acta/internal/authn"
	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/config"
	"github.com/peios/acta/internal/live"
	"github.com/peios/acta/internal/mcpcfg"
	"github.com/peios/acta/internal/memory"
	"github.com/peios/acta/internal/passkey"
	"github.com/peios/acta/internal/push"
	"github.com/peios/acta/internal/session"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/workspace"
)

// maxBodyBytes caps the request body for every route. Acta's payloads — form
// posts, small JSON mutations, MCP tool calls — are tiny, so a 1 MiB ceiling is
// generous while still bounding memory against an oversized or hung upload.
const maxBodyBytes = 1 << 20

// NewHandler builds the application handler.
func NewHandler(cfg config.Config, sessions *session.Manager, provider authn.Provider, passkeys *passkey.Service, tokens *apitoken.Service, agents *agent.Service, agentSessions *agentsession.Service, agentHub *agentsession.Hub, accounts *account.Service, workspaces *workspace.Service, boards *board.Service, memories *memory.Service, mcpConfig *mcpcfg.Service, pushSender *push.Sender) http.Handler {
	h := &handlers{
		sessions:      sessions,
		provider:      provider,
		passkeys:      passkeys,
		tokens:        tokens,
		agents:        agents,
		agentSessions: agentSessions,
		agentHub:      agentHub,
		accounts:      accounts,
		workspaces:    workspaces,
		board:         boards,
		memories:      memories,
		mcpcfg:        mcpConfig,
		live:          live.NewHub(),
		push:          pushSender,
		secure:        cfg.CookieSecure(),
		publicURL:     strings.TrimRight(cfg.RPOrigin, "/"),
	}
	// Attach the live bell as a board notifier now that the hub exists, so
	// subscription notifications (filed deep in the board) reach the bell over
	// SSE the same way Web Push reaches it out of band.
	boards.AddNotifier(newLiveNotifier(h.live, boards))
	// Session presence (held/running) rides the owner's SSE user topic so the
	// sidebar dots and list badges update without a reload.
	if agentHub != nil {
		agentHub.SetRenameNotifier(func(ownerID, sessionID, title string) {
			h.publishLive(userTopic(ownerID), "session.renamed", "", map[string]any{"id": sessionID, "title": title})
		})
		// A session needing its owner files a notification, which the bell,
		// the live stream and Web Push then deliver exactly as they would a
		// mention — no channel of its own.
		agentHub.SetAlertNotifier(func(ownerID string, as store.AgentSession, verb, summary string) {
			title := as.Title
			if title == "" {
				title = as.Backend + " session"
			}
			if _, err := boards.File(context.Background(), store.Notification{
				RecipientID: ownerID, Kind: store.NotificationSession,
				ItemID: as.ID, ItemTitle: title, ActorName: "Claude", Verb: verb, Summary: summary,
			}); err != nil {
				slog.Error("session alert", "session", as.ID, "err", err)
			}
		})
		agentHub.SetPresenceNotifier(func(ownerID, sessionID string, held, running bool) {
			h.publishLive(userTopic(ownerID), "session.presence", "", map[string]any{
				"id": sessionID, "held": held, "running": running,
			})
		})
	}
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
	// Project-scoped memories, nested under the project (id in the path).
	mux.Handle("GET /{slug}/projects/{id}/memories", protected(h.projectMemories))
	mux.Handle("POST /{slug}/projects/{id}/memories", protected(h.projectMemoryCreate))
	mux.Handle("GET /{slug}/projects/{id}/memories/{mid}/edit", protected(h.projectMemoryEdit))
	mux.Handle("POST /{slug}/projects/{id}/memories/{mid}/edit", protected(h.projectMemoryUpdate))
	mux.Handle("POST /{slug}/projects/{id}/memories/{mid}/delete", protected(h.projectMemoryDelete))
	// Releases: a workspace's versioned cut-lines. Same routing shape as projects —
	// /{slug}/releases is more specific than /{slug}/{board}; the single-release
	// view rides ?r=<id>; mutations keep the id in the path.
	mux.Handle("GET /{slug}/releases", protected(h.releases))
	mux.Handle("POST /{slug}/releases", protected(h.releaseCreate))
	mux.Handle("POST /{slug}/releases/{id}/edit", protected(h.releaseUpdate))
	mux.Handle("POST /{slug}/releases/{id}/status", protected(h.releaseSetStatus))
	mux.Handle("POST /{slug}/releases/{id}/delete", protected(h.releaseDelete))
	// Workspace-scoped memories: a literal /{slug}/memories beats /{slug}/{board}.
	// The edit page and mutations use 4-segment paths so they never collide with
	// /notifications/{id}/open the way a 3-segment /{slug}/memories/{mid} would.
	mux.Handle("GET /{slug}/memories", protected(h.workspaceMemories))
	mux.Handle("POST /{slug}/memories", protected(h.workspaceMemoryCreate))
	mux.Handle("GET /{slug}/memories/{mid}/edit", protected(h.workspaceMemoryEdit))
	mux.Handle("POST /{slug}/memories/{mid}/edit", protected(h.workspaceMemoryUpdate))
	mux.Handle("POST /{slug}/memories/{mid}/delete", protected(h.workspaceMemoryDelete))
	// Cmd-K quick-switcher results fragment. Literal segment, matched ahead of
	// /{slug}/{board} so it never shadows a board.
	mux.Handle("GET /{slug}/search", protected(h.searchResults))
	// A second path segment selects a non-default board (e.g. /{slug}/backlog);
	// the literal sub-routes above are more specific, so they always win.
	mux.Handle("GET /{slug}/{board}", protected(h.boardPage))
	mux.Handle("POST /{slug}/statuses", protected(h.statusCreate))
	mux.Handle("POST /{slug}/statuses/reorder", protected(h.statusReorder))
	mux.Handle("POST /{slug}/statuses/{id}/rename", protected(h.statusRename))
	mux.Handle("POST /{slug}/statuses/{id}/color", protected(h.statusColor))
	mux.Handle("POST /{slug}/statuses/{id}/delete", protected(h.statusDelete))
	mux.Handle("POST /{slug}/views", protected(h.viewCreate))
	mux.Handle("POST /{slug}/views/reorder", protected(h.viewReorder))
	mux.Handle("POST /{slug}/views/{id}/rename", protected(h.viewRename))
	mux.Handle("POST /{slug}/views/{id}/save", protected(h.viewSave))
	mux.Handle("POST /{slug}/views/{id}/delete", protected(h.viewDelete))
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
	mux.Handle("POST /{slug}/items/{id}/documents", protected(h.itemDocumentCreate))
	mux.Handle("POST /{slug}/items/{id}/documents/{did}/edit", protected(h.itemDocumentEdit))
	mux.Handle("POST /{slug}/items/{id}/documents/{did}/delete", protected(h.itemDocumentDelete))
	mux.Handle("POST /{slug}/items/{id}/parent", protected(h.itemParent))
	mux.Handle("POST /{slug}/items/{id}/project", protected(h.itemSetProject))
	mux.Handle("POST /{slug}/items/{id}/release", protected(h.itemSetRelease))
	mux.Handle("POST /{slug}/items/{id}/priority", protected(h.itemSetPriority))
	mux.Handle("POST /{slug}/items/{id}/type", protected(h.itemSetType))
	mux.Handle("POST /{slug}/items/{id}/size", protected(h.itemSetSize))
	mux.Handle("POST /{slug}/items/{id}/due", protected(h.itemSetDue))
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
	// The signed-in user's own memories (scope "user").
	mux.Handle("GET /account/memories", protected(h.accountMemories))
	mux.Handle("POST /account/memories", protected(h.accountMemoryCreate))
	mux.Handle("GET /account/memories/{mid}", protected(h.accountMemoryEdit))
	mux.Handle("POST /account/memories/{mid}", protected(h.accountMemoryUpdate))
	mux.Handle("POST /account/memories/{mid}/delete", protected(h.accountMemoryDelete))
	// Agent sessions: browser-driven Claude Code (and later other backends)
	// sessions relayed to a harness on the owner's machine. The list + chat
	// pages and the browser chat websocket are cookie-authed UI; the harness
	// websocket is Bearer-authed and mounts on the root mux (below), like the
	// REST API, since a harness carries a token, not a cookie.
	mux.Handle("GET /account/sessions", protected(h.agentSessionsPage))
	mux.Handle("POST /account/sessions", protected(h.agentSessionCreate))
	mux.Handle("GET /account/harnesses/{id}/dirs", protected(h.agentHarnessDirs))
	mux.Handle("GET /account/harnesses/{id}/transcripts", protected(h.agentHarnessTranscripts))
	mux.Handle("POST /account/sessions/import", protected(h.agentSessionImport))
	mux.Handle("GET /account/sessions/lookup", protected(h.agentSessionLookup))
	mux.Handle("GET /account/sessions/{id}", protected(h.agentSessionPage))
	mux.Handle("POST /account/sessions/{id}/delete", protected(h.agentSessionDelete))
	mux.Handle("POST /account/sessions/{id}/title", protected(h.agentSessionRename))
	mux.Handle("GET /account/sessions/{id}/frames", protected(h.agentSessionFrames))
	mux.Handle("GET /account/sessions/{id}/ws", protected(h.agentSessionBrowserWS))
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
	// MCP: the conventions guide (acta://guide, read-only) and user-defined prompts.
	mux.Handle("GET /settings/guide", protected(h.settingsGuide))
	mux.Handle("GET /settings/prompts", protected(h.settingsPrompts))
	mux.Handle("GET /settings/prompts/new", protected(h.promptNew))
	mux.Handle("POST /settings/prompts", protected(h.promptCreate))
	mux.Handle("GET /settings/prompts/{id}", protected(h.promptEdit))
	mux.Handle("POST /settings/prompts/{id}", protected(h.promptUpdate))
	mux.Handle("POST /settings/prompts/{id}/delete", protected(h.promptDelete))
	// Site-wide memories (scope "site"): instance-global notes managed from settings.
	mux.Handle("GET /settings/memories", protected(h.settingsMemories))
	mux.Handle("POST /settings/memories", protected(h.settingsMemoryCreate))
	mux.Handle("GET /settings/memories/{mid}", protected(h.settingsMemoryEdit))
	mux.Handle("POST /settings/memories/{mid}", protected(h.settingsMemoryUpdate))
	mux.Handle("POST /settings/memories/{mid}/delete", protected(h.settingsMemoryDelete))
	mux.Handle("GET /welcome/passkey", protected(h.welcomePasskey))

	// CLI login (gh-style loopback): a browser page that mints a token and hands
	// it back to a local `acta login` listener. Cookie-authed + CSRF like any UI.
	mux.Handle("GET /cli/authorize", protected(h.cliAuthorize))
	mux.Handle("POST /cli/authorize", protected(h.cliAuthorizeSubmit))

	// JSON API, authenticated by personal access token (Bearer). It carries no
	// cookies, so it mounts outside the CSRF chain — the token is the auth.
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/me", h.apiMe)
	api.HandleFunc("GET /api/v1/agent/boot", h.apiAgentBoot)
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
	api.HandleFunc("POST /api/v1/w/{slug}/releases/{name}/target", h.apiSetReleaseTarget)
	api.HandleFunc("GET /api/v1/subscriptions", h.apiListSubscriptions)
	api.HandleFunc("POST /api/v1/subscriptions", h.apiSubscribe)
	api.HandleFunc("DELETE /api/v1/subscriptions", h.apiUnsubscribe)
	// Agent + token management, so the CLI can provision an MCP integration.
	api.HandleFunc("GET /api/v1/agents", h.apiListAgents)
	api.HandleFunc("POST /api/v1/agents", h.apiCreateAgent)
	api.HandleFunc("POST /api/v1/agents/{id}/tokens", h.apiCreateAgentToken)
	api.HandleFunc("POST /api/v1/tokens", h.apiCreateSelfToken)
	// Harness relay: a harness dials in here and holds the connection open.
	api.HandleFunc("GET /api/v1/harness/ws", h.harnessWS)

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
