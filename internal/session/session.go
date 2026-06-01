// Package session manages server-side sessions: a row per session, an opaque
// token in an HttpOnly cookie. This layer is provider-agnostic — it knows
// nothing about how a principal authenticated, only how to mint, read, and
// destroy a session for one. That's why logout is a real server-side
// invalidation rather than just clearing a cookie.
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

type Config struct {
	CookieName      string
	Secure          bool
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
	// RefreshInterval is the minimum gap between last_seen writes, so an
	// active session doesn't issue a DB write on literally every request.
	RefreshInterval time.Duration
}

type Manager struct {
	store store.Store
	cfg   Config
	now   func() time.Time // injectable for tests
}

func New(st store.Store, cfg Config) *Manager {
	if cfg.CookieName == "" {
		cfg.CookieName = "acta_session"
	}
	if cfg.RefreshInterval == 0 {
		cfg.RefreshInterval = time.Minute
	}
	return &Manager{store: st, cfg: cfg, now: time.Now}
}

// Establish mints a new session for principal and sets the session cookie.
func (m *Manager) Establish(ctx context.Context, w http.ResponseWriter, p identity.Principal) error {
	return m.establish(ctx, w, p, "")
}

// EstablishWithRequest is Establish that also records the originating request's
// user-agent, so the session is recognisable in the account UI. Prefer it
// wherever the request is at hand (interactive logins).
func (m *Manager) EstablishWithRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, p identity.Principal) error {
	return m.establish(ctx, w, p, r.UserAgent())
}

func (m *Manager) establish(ctx context.Context, w http.ResponseWriter, p identity.Principal, userAgent string) error {
	token, err := newToken()
	if err != nil {
		return err
	}
	now := m.now()
	s := store.Session{
		ID:        token,
		UserID:    p.ID,
		UserAgent: userAgent,
		CreatedAt: now,
		ExpiresAt: now.Add(m.cfg.AbsoluteTimeout),
		LastSeen:  now,
	}
	if err := m.store.CreateSession(ctx, s); err != nil {
		return err
	}
	m.setCookie(w, token, m.cfg.AbsoluteTimeout)
	return nil
}

// Current resolves the session cookie to a Principal, or returns nil if there
// is no valid session. Expired sessions (idle or absolute) are deleted as a
// side effect. A valid session has its last_seen refreshed, throttled by
// RefreshInterval.
func (m *Manager) Current(ctx context.Context, r *http.Request) (*identity.Principal, error) {
	c, err := r.Cookie(m.cfg.CookieName)
	if err != nil || c.Value == "" {
		return nil, nil
	}

	s, err := m.store.SessionByID(ctx, c.Value)
	if err == store.ErrSessionNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := m.now()
	if now.After(s.ExpiresAt) || now.After(s.LastSeen.Add(m.cfg.IdleTimeout)) {
		_ = m.store.DeleteSession(ctx, s.ID)
		return nil, nil
	}

	if now.Sub(s.LastSeen) >= m.cfg.RefreshInterval {
		if err := m.store.TouchSession(ctx, s.ID, now); err != nil {
			return nil, err
		}
	}

	u, err := m.store.UserByID(ctx, s.UserID)
	if err == store.ErrUserNotFound {
		// Session outlived its user; treat as logged out.
		_ = m.store.DeleteSession(ctx, s.ID)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &identity.Principal{ID: u.ID, Username: u.Username, Display: u.Display}, nil
}

// Destroy invalidates the current session server-side and clears the cookie.
// It is safe to call when there is no session.
func (m *Manager) Destroy(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(m.cfg.CookieName); err == nil && c.Value != "" {
		if err := m.store.DeleteSession(ctx, c.Value); err != nil && err != store.ErrSessionNotFound {
			return err
		}
	}
	m.clearCookie(w)
	return nil
}

// --- account management ---

// CurrentToken returns the raw session token from the request cookie — the
// session's secret id. It identifies the current row in a list and is the
// keep-id for RevokeOthers. It is never rendered.
func (m *Manager) CurrentToken(r *http.Request) string {
	c, err := r.Cookie(m.cfg.CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// List returns the principal's currently-valid sessions for the account UI,
// most-recently-seen first.
func (m *Manager) List(ctx context.Context, userID string) ([]store.Session, error) {
	return m.store.SessionsByUserID(ctx, userID, m.now())
}

// Revoke ends one of the user's sessions, addressed by its non-secret PublicID.
func (m *Manager) Revoke(ctx context.Context, publicID, userID string) error {
	return m.store.DeleteUserSession(ctx, publicID, userID)
}

// RevokeOthers ends all of the user's sessions except the current one
// (identified by keepToken), returning how many were removed.
func (m *Manager) RevokeOthers(ctx context.Context, userID, keepToken string) (int64, error) {
	return m.store.DeleteOtherSessions(ctx, userID, keepToken)
}

func (m *Manager) setCookie(w http.ResponseWriter, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.cfg.CookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   m.cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *Manager) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.cfg.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
