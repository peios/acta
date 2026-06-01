package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/session"
)

type ctxKey int

const (
	ctxPrincipal ctxKey = iota
	ctxCSRF
)

func principalFrom(ctx context.Context) *identity.Principal {
	p, _ := ctx.Value(ctxPrincipal).(*identity.Principal)
	return p
}

func csrfTokenFrom(ctx context.Context) string {
	t, _ := ctx.Value(ctxCSRF).(string)
	return t
}

// requireAuth gates protected routes: unauthenticated requests are redirected
// to the login page with a return_to pointing back at where they were headed.
func requireAuth(sessions *session.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := sessions.Current(r.Context(), r)
			if err != nil {
				slog.Error("session lookup", "err", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if p == nil {
				q := url.Values{}
				q.Set("return_to", r.URL.RequestURI())
				http.Redirect(w, r, "/login?"+q.Encode(), http.StatusSeeOther)
				return
			}
			ctx := context.WithValue(r.Context(), ctxPrincipal, p)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

const csrfCookieName = "acta_csrf"

// csrf implements double-submit-cookie CSRF protection. A random token lives
// in an HttpOnly cookie; templates echo the same token into a hidden field
// (server-side, so HttpOnly is fine). Unsafe requests must present a matching
// token. Works uniformly for the pre-auth login POST and post-auth forms.
func csrf(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ensureCSRFCookie(w, r, secure)
			if unsafeMethod(r.Method) && !validCSRF(r, token) {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
			ctx := context.WithValue(r.Context(), ctxCSRF, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request, secure bool) string {
	if c, err := r.Cookie(csrfCookieName); err == nil && len(c.Value) >= 32 {
		return c.Value
	}
	token := randToken()
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	return token
}

func validCSRF(r *http.Request, token string) bool {
	sent := r.Header.Get("X-CSRF-Token")
	if sent == "" {
		_ = r.ParseForm()
		sent = r.PostFormValue("csrf_token")
	}
	return sent != "" && subtle.ConstantTimeCompare([]byte(sent), []byte(token)) == 1
}

func unsafeMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// secureHeaders sets conservative security headers on every response.
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'")
		next.ServeHTTP(w, r)
	})
}

// requestLogger emits one structured line per request.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("http",
			"method", r.Method, "path", r.URL.Path,
			"status", sw.status, "dur", time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func randToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
