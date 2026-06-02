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
	"github.com/peios/acta/internal/mcpcfg"
	"github.com/peios/acta/internal/passkey"
	"github.com/peios/acta/internal/session"
	"github.com/peios/acta/internal/workspace"
)

// maxBodyBytes caps the request body for every route. Acta's payloads — form
// posts, small JSON mutations, MCP tool calls — are tiny, so a 1 MiB ceiling is
// generous while still bounding memory against an oversized or hung upload.
const maxBodyBytes = 1 << 20

// NewHandler builds the application handler.
func NewHandler(cfg config.Config, sessions *session.Manager, provider authn.Provider, passkeys *passkey.Service, tokens *apitoken.Service, agents *agent.Service, accounts *account.Service, workspaces *workspace.Service, boards *board.Service, mcpConfig *mcpcfg.Service) http.Handler {
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
		secure:     cfg.CookieSecure(),
		publicURL:  strings.TrimRight(cfg.RPOrigin, "/"),
	}
	mux := http.NewServeMux()

	// Static assets (passkey JS).
	mux.Handle("GET /static/", staticHandler())

	// Public routes.
	mux.HandleFunc("GET /login", h.loginPage)
	provider.Mount(mux) // POST /login/password, /login/passkey/{begin,finish}
	mux.HandleFunc("POST /logout", h.logout)

	// Protected routes. "/{$}" matches exactly "/".
	protected := func(fn http.HandlerFunc) http.Handler {
		return requireAuth(sessions)(fn)
	}
	mux.Handle("GET /{$}", protected(h.rootRedirect))

	// A workspace's board and its JSON mutation API (consumed by board.js).
	mux.Handle("GET /w/{slug}", protected(h.boardPage))
	mux.Handle("GET /w/{slug}/archive", protected(h.archivePage))
	mux.Handle("GET /w/{slug}/activity", protected(h.activityPage))
	mux.Handle("POST /w/{slug}/statuses", protected(h.statusCreate))
	mux.Handle("POST /w/{slug}/statuses/reorder", protected(h.statusReorder))
	mux.Handle("POST /w/{slug}/statuses/{id}/rename", protected(h.statusRename))
	mux.Handle("POST /w/{slug}/statuses/{id}/color", protected(h.statusColor))
	mux.Handle("POST /w/{slug}/statuses/{id}/delete", protected(h.statusDelete))
	mux.Handle("POST /w/{slug}/milestones/reorder", protected(h.milestoneReorder))
	mux.Handle("POST /w/{slug}/items", protected(h.itemCreate))
	mux.Handle("GET /w/{slug}/items/{id}/modal", protected(h.itemModal))
	mux.Handle("POST /w/{slug}/items/{id}/rename", protected(h.itemRename))
	mux.Handle("POST /w/{slug}/items/{id}/move", protected(h.itemMove))
	mux.Handle("POST /w/{slug}/items/{id}/description", protected(h.itemDescription))
	mux.Handle("POST /w/{slug}/items/{id}/assignee", protected(h.itemAssignee))
	mux.Handle("POST /w/{slug}/items/{id}/status", protected(h.itemSetStatus))
	mux.Handle("POST /w/{slug}/items/{id}/comment", protected(h.itemComment))
	mux.Handle("POST /w/{slug}/items/{id}/parent", protected(h.itemParent))
	mux.Handle("POST /w/{slug}/items/{id}/milestone", protected(h.itemMilestone))
	mux.Handle("POST /w/{slug}/items/{id}/subtasks", protected(h.subtaskCreate))
	mux.Handle("POST /w/{slug}/items/{id}/subtasks/reorder", protected(h.subtaskReorder))
	mux.Handle("POST /w/{slug}/items/{id}/archive", protected(h.itemArchive))
	mux.Handle("POST /w/{slug}/items/{id}/unarchive", protected(h.itemUnarchive))
	mux.Handle("POST /w/{slug}/items/{id}/delete", protected(h.itemDelete))

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
	mux.Handle("GET /account/agents", protected(h.accountAgents))
	mux.Handle("POST /account/agents", protected(h.agentCreate))
	mux.Handle("GET /account/agents/{id}", protected(h.agentDetail))
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
	root := http.NewServeMux()
	root.Handle("/api/v1/", requireToken(tokens)(api))
	root.Handle("/mcp", mcpEndpoint)
	root.Handle("/", csrf(cfg.CookieSecure())(mux))

	// Outer chain (outermost first): log + tag the request, recover panics into
	// a clean 500, set security headers, cap the request body, then route.
	var chain http.Handler = http.MaxBytesHandler(root, maxBodyBytes)
	chain = secureHeaders(cfg.CookieSecure())(chain)
	chain = recoverPanic(chain)
	return requestLogger(cfg.TrustedProxies)(chain)
}
