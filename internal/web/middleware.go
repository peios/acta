package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"time"

	"github.com/peios/acta/internal/httpx"
	"github.com/peios/acta/internal/id"
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

// secureHeaders sets conservative security headers on every response. HSTS is
// emitted only when hsts is set (i.e. in prod behind real HTTPS), since it's
// meaningless — and a footgun — over plain http during local dev.
func secureHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// recoverPanic turns a panic in any downstream handler into a logged 500 rather
// than a dropped connection. It sits inside requestLogger, so the panic log
// carries the request id that correlates it with the access-log line.
func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic",
					"req_id", httpx.RequestID(r.Context()),
					"method", r.Method, "path", r.URL.Path,
					"err", rec, "stack", string(debug.Stack()))
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requestLogger assigns each request an id and resolves its client IP (honouring
// the trusted-proxy set), stashes both in the context and the X-Request-ID
// response header, and emits one structured line per request. As the outermost
// middleware it covers everything inside it, so handler logs and the access log
// share a request id.
func requestLogger(trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			reqID := id.New()
			ip := clientIP(r, trusted)
			w.Header().Set("X-Request-ID", reqID)
			r = r.WithContext(httpx.WithRequestMeta(r.Context(), reqID, ip))

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			slog.Info("http",
				"req_id", reqID,
				"method", r.Method, "path", r.URL.Path,
				"status", sw.status, "ip", ip,
				"dur", time.Since(start))
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush forwards to the underlying writer when it supports streaming, so the
// logging wrapper stays transparent to SSE / chunked responses.
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func randToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
