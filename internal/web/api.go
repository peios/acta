package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/peios/acta/internal/apitoken"
)

// requireToken authenticates API requests by bearer token (a personal access
// token) and injects the resolved principal. It is the token-auth counterpart
// to the cookie-session requireAuth: unlike that gate it never redirects —
// API clients get a 401 JSON body. Because auth is the bearer token, these
// routes carry no cookies and need no CSRF check.
func requireToken(tokens *apitoken.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				apiError(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			p, err := tokens.Authenticate(r.Context(), raw)
			if err != nil {
				// Malformed, unknown, and orphaned tokens are indistinguishable
				// to the client by design.
				apiError(w, http.StatusUnauthorized, "invalid token")
				return
			}
			ctx := context.WithValue(r.Context(), ctxPrincipal, &p)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(r *http.Request) string {
	const pfx = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(pfx) && strings.EqualFold(h[:len(pfx)], pfx) {
		return strings.TrimSpace(h[len(pfx):])
	}
	return ""
}

// apiMe returns the authenticated principal: the minimal endpoint that proves a
// token is valid, and the seed of the wider JSON API to come.
func (h *handlers) apiMe(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{
		"id":       p.ID,
		"username": p.Username,
		"display":  p.Display,
	})
}

// apiLogout revokes the token presented on this request — the CLI's `logout`.
func (h *handlers) apiLogout(w http.ResponseWriter, r *http.Request) {
	if err := h.tokens.RevokeByPlaintext(r.Context(), bearerToken(r)); err != nil {
		apiError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
