// Package web wires the HTTP surface: routes, middleware, and handlers. It
// depends only on the session manager and an authn.Provider — never on the
// store or a specific provider implementation.
package web

import (
	"net/http"

	"github.com/peios/acta/internal/authn"
	"github.com/peios/acta/internal/config"
	"github.com/peios/acta/internal/session"
)

// NewHandler builds the application handler.
func NewHandler(cfg config.Config, sessions *session.Manager, provider authn.Provider) http.Handler {
	h := &handlers{sessions: sessions, provider: provider}
	mux := http.NewServeMux()

	// Public routes.
	mux.HandleFunc("GET /login", h.loginPage)
	provider.Mount(mux) // e.g. POST /login/password
	mux.HandleFunc("POST /logout", h.logout)

	// Protected routes. "/{$}" matches exactly "/".
	mux.Handle("GET /{$}", requireAuth(sessions)(http.HandlerFunc(h.home)))

	// Global middleware chain (outermost first).
	return requestLogger(secureHeaders(csrf(cfg.CookieSecure())(mux)))
}
