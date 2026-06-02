// Package httpx holds small HTTP helpers shared across the web and auth
// layers, with no dependencies of its own.
package httpx

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxClientIP
)

// WithRequestMeta attaches the per-request id and resolved client IP to ctx, so
// any layer — handlers, the auth provider, panic recovery — can read them for
// correlated logging. The web request logger sets these at the edge.
func WithRequestMeta(ctx context.Context, requestID, clientIP string) context.Context {
	ctx = context.WithValue(ctx, ctxRequestID, requestID)
	return context.WithValue(ctx, ctxClientIP, clientIP)
}

// RequestID returns the request id attached by WithRequestMeta, or "".
func RequestID(ctx context.Context) string {
	s, _ := ctx.Value(ctxRequestID).(string)
	return s
}

// ClientIP returns the resolved originating client IP, or "".
func ClientIP(ctx context.Context) string {
	s, _ := ctx.Value(ctxClientIP).(string)
	return s
}

// ChallengeCookie carries the id of an in-flight WebAuthn ceremony between its
// begin and finish requests. Short-lived and HttpOnly.
const ChallengeCookie = "acta_wauth"

// SetChallengeCookie stores the ceremony id for ~5 minutes.
func SetChallengeCookie(w http.ResponseWriter, id string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     ChallengeCookie,
		Value:    id,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ChallengeCookieValue returns the ceremony id, or "" if absent.
func ChallengeCookieValue(r *http.Request) string {
	c, err := r.Cookie(ChallengeCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// ClearChallengeCookie removes the ceremony cookie.
func ClearChallengeCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     ChallengeCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// WorkspaceCookie remembers the slug of the workspace the user last viewed, so
// / and the top-bar switcher default back to it. It's only a UI hint — every
// signed-in user may view every workspace, and the URL is the source of truth —
// so it carries no security weight and is deliberately not HttpOnly-sensitive.
const WorkspaceCookie = "acta_ws"

// SetWorkspaceCookie records the last-viewed workspace slug for a year.
func SetWorkspaceCookie(w http.ResponseWriter, slug string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     WorkspaceCookie,
		Value:    slug,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// WorkspaceCookieValue returns the last-viewed workspace slug, or "" if absent.
func WorkspaceCookieValue(r *http.Request) string {
	c, err := r.Cookie(WorkspaceCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// SafeReturnTo sanitises a post-login redirect target. It only permits local,
// absolute paths — anything that could redirect off-site (scheme-relative
// "//host", absolute URLs, backslash tricks) collapses to "/". This is what
// keeps the ?return_to= parameter from becoming an open-redirect.
func SafeReturnTo(raw string) string {
	if raw == "" {
		return "/"
	}
	if !strings.HasPrefix(raw, "/") {
		return "/"
	}
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") {
		return "/"
	}
	if strings.ContainsAny(raw, "\\") || strings.Contains(raw, "://") {
		return "/"
	}
	return raw
}
