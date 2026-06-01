// Package web wires the HTTP surface: routes, middleware, and handlers. It
// depends only on the session manager and an authn.Provider — never on the
// store or a specific provider implementation.
package web

import (
	"net/http"

	"github.com/peios/acta/internal/authn"
	"github.com/peios/acta/internal/config"
	"github.com/peios/acta/internal/passkey"
	"github.com/peios/acta/internal/session"
)

// NewHandler builds the application handler.
func NewHandler(cfg config.Config, sessions *session.Manager, provider authn.Provider, passkeys *passkey.Service) http.Handler {
	h := &handlers{
		sessions: sessions,
		provider: provider,
		passkeys: passkeys,
		secure:   cfg.CookieSecure(),
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
	mux.Handle("GET /{$}", protected(h.home))
	mux.Handle("GET /settings", protected(h.settingsIndex))
	mux.Handle("GET /settings/security", protected(h.settingsSecurity))
	mux.Handle("POST /settings/passkeys/register/begin", protected(h.passkeyRegisterBegin))
	mux.Handle("POST /settings/passkeys/register/finish", protected(h.passkeyRegisterFinish))
	mux.Handle("POST /settings/passkeys/{id}/delete", protected(h.passkeyDelete))
	mux.Handle("GET /welcome/passkey", protected(h.welcomePasskey))

	// Global middleware chain (outermost first).
	return requestLogger(secureHeaders(csrf(cfg.CookieSecure())(mux)))
}
