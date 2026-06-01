// Package authn defines the authentication seam.
//
// A Provider authenticates principals via its own login routes and ceremony,
// and on success establishes a session. The seam is deliberately *not*
// "verify these credentials" — that shape only fits passwords. It's one step
// later: "I have authenticated some principal; now establish a session." A
// password form (1 step), a passkey challenge (2 steps), and a Peios
// kernel-mediated lookup (0 steps, ambient) all reach that same point by
// different routes, so each provider owns whatever ceremony it needs.
//
// The active provider is chosen at startup. The internal provider (this repo,
// Debian) owns the users table and verifies passwords. A future Peios
// provider would defer identity to the kernel and own no users table at all —
// and nothing downstream of the session layer would change.
package authn

import "net/http"

// Method describes one login option for the shared login page to render.
type Method struct {
	ID    string // e.g. "password"
	Label string
}

// Provider is an authentication mechanism.
type Provider interface {
	// Name identifies the provider (for logging/diagnostics).
	Name() string
	// Methods lists the login options the login page should render.
	Methods() []Method
	// Mount installs the provider's own submit routes (e.g.
	// "POST /login/password"). On success a route calls session.Establish
	// and redirects; on failure it redirects back to the login page.
	Mount(mux *http.ServeMux)
}
