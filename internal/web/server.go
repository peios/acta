// Package web wires the HTTP surface: routes, middleware, and handlers. It
// depends only on the session manager and an authn.Provider — never on the
// store or a specific provider implementation.
package web

import (
	"net/http"

	"github.com/peios/acta/internal/agent"
	"github.com/peios/acta/internal/apitoken"
	"github.com/peios/acta/internal/authn"
	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/config"
	"github.com/peios/acta/internal/passkey"
	"github.com/peios/acta/internal/session"
	"github.com/peios/acta/internal/workspace"
)

// NewHandler builds the application handler.
func NewHandler(cfg config.Config, sessions *session.Manager, provider authn.Provider, passkeys *passkey.Service, tokens *apitoken.Service, agents *agent.Service, workspaces *workspace.Service, boards *board.Service) http.Handler {
	h := &handlers{
		sessions:   sessions,
		provider:   provider,
		passkeys:   passkeys,
		tokens:     tokens,
		agents:     agents,
		workspaces: workspaces,
		board:      boards,
		secure:     cfg.CookieSecure(),
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
	mux.Handle("POST /w/{slug}/statuses", protected(h.statusCreate))
	mux.Handle("POST /w/{slug}/statuses/reorder", protected(h.statusReorder))
	mux.Handle("POST /w/{slug}/statuses/{id}/rename", protected(h.statusRename))
	mux.Handle("POST /w/{slug}/statuses/{id}/delete", protected(h.statusDelete))
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

	mux.Handle("GET /settings", protected(h.settingsIndex))
	mux.Handle("GET /settings/security", protected(h.settingsSecurity))
	mux.Handle("POST /settings/passkeys/register/begin", protected(h.passkeyRegisterBegin))
	mux.Handle("POST /settings/passkeys/register/finish", protected(h.passkeyRegisterFinish))
	mux.Handle("POST /settings/passkeys/{id}/delete", protected(h.passkeyDelete))
	mux.Handle("POST /settings/tokens", protected(h.tokenCreate))
	mux.Handle("POST /settings/tokens/{id}/delete", protected(h.tokenDelete))
	mux.Handle("POST /settings/sessions/revoke-others", protected(h.sessionRevokeOthers))
	mux.Handle("POST /settings/sessions/{id}/revoke", protected(h.sessionRevoke))
	mux.Handle("GET /settings/workspaces", protected(h.settingsWorkspaces))
	mux.Handle("POST /settings/workspaces", protected(h.workspaceCreate))
	mux.Handle("POST /settings/workspaces/{id}/rename", protected(h.workspaceRename))
	mux.Handle("POST /settings/workspaces/{id}/delete", protected(h.workspaceDelete))
	mux.Handle("GET /settings/agents", protected(h.settingsAgents))
	mux.Handle("POST /settings/agents", protected(h.agentCreate))
	mux.Handle("GET /settings/agents/{id}", protected(h.agentDetail))
	mux.Handle("POST /settings/agents/{id}/delete", protected(h.agentDelete))
	mux.Handle("POST /settings/agents/{id}/tokens", protected(h.agentTokenCreate))
	mux.Handle("POST /settings/agents/{id}/tokens/{tokenID}/delete", protected(h.agentTokenDelete))
	mux.Handle("GET /welcome/passkey", protected(h.welcomePasskey))

	// JSON API, authenticated by personal access token. Bearer auth carries no
	// cookies, so CSRF doesn't apply; the GET below is safe under the global
	// CSRF check regardless. API mutations (a later slice) will mount outside
	// the cookie/CSRF chain.
	mux.Handle("GET /api/v1/me", requireToken(tokens)(http.HandlerFunc(h.apiMe)))

	// Global middleware chain (outermost first).
	return requestLogger(secureHeaders(csrf(cfg.CookieSecure())(mux)))
}
