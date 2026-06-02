// Package identity defines the core actor model. A Principal is an
// authenticated actor, independent of *how* they authenticated.
//
// This is the stable centre of the auth design: everything downstream of
// login — sessions, middleware, handlers, eventually authorization — deals
// only in Principals. It never sees passwords, tokens, or provider-specific
// details. That separation is what lets us swap the internal (Debian)
// password provider for Peios' kernel-mediated provider without touching
// anything but the provider itself.
package identity

import "context"

// Principal is an authenticated actor.
type Principal struct {
	ID       string
	Username string
	Display  string
}

type ctxKey int

const principalKey ctxKey = 0

// NewContext returns a copy of ctx carrying p as the authenticated principal.
// The auth middleware sets this once per request; everything downstream — HTTP
// handlers, MCP tools, the board service's activity log — reads it back with
// FromContext, so attribution is uniform regardless of how the request arrived.
func NewContext(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// FromContext returns the principal carried by ctx, if any.
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey).(*Principal)
	return p, ok
}
