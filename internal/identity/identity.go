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

// Principal is an authenticated actor.
type Principal struct {
	ID       string
	Username string
	Display  string
}
