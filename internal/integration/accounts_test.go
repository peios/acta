// These tests use isolated schemas in an explicitly configured PostgreSQL
// database. No fake store can establish its transactional guarantees.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/config"
	"acta/internal/httpapi"
	"acta/internal/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type fixture struct {
	store *postgres.Store
	conn  *pgx.Conn
	url   string
}

func database(t *testing.T) fixture {
	t.Helper()
	raw := os.Getenv("ACTA_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set ACTA_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := t.Context()
	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	schema := "acta_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = conn.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`); err != nil {
			t.Error(err)
		}
		conn.Close(cleanup)
	})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	if _, err = conn.Exec(ctx, `SET search_path TO `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return fixture{store, conn, u.String()}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func bootstrap(t *testing.T, f fixture, now time.Time) accounts.Account {
	t.Helper()
	a := accounts.Account{ID: uuid.NewString(), Username: "jack", DirectPermissions: []string{accounts.Superuser}}
	grant := auth.Digest(uuid.NewString())
	must(t, f.store.GrantSetup(t.Context(), grant, now.Add(time.Minute)))
	hash, err := auth.HashPassword("silver meadow orbit lamp")
	must(t, err)
	must(t, f.store.CompleteSetup(t.Context(), grant, a, hash, now))
	return a
}
func TestSetupAtomicAndPermanent(t *testing.T) {
	f := database(t)
	ctx := t.Context()
	now := time.Now().UTC()
	grantA, grantB := auth.Digest("grant-a"), auth.Digest("grant-b")
	must(t, f.store.GrantSetup(ctx, grantA, now.Add(time.Minute)))
	must(t, f.store.GrantSetup(ctx, grantB, now.Add(time.Minute)))
	invalid := accounts.Account{ID: uuid.NewString(), Username: "invalid/segment"}
	if err := f.store.CompleteSetup(ctx, grantA, invalid, "irrelevant", now); err == nil {
		t.Fatal("invalid creation succeeded")
	}
	valid, err := f.store.SetupGrantValid(ctx, grantA, now)
	must(t, err)
	if !valid {
		t.Fatal("failed transaction consumed grant")
	}
	second, err := postgres.Open(ctx, f.url)
	must(t, err)
	defer second.Close()
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i, store := range []*postgres.Store{f.store, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			grant := grantA
			if i == 1 {
				grant = grantB
			}
			err := store.CompleteSetup(ctx, grant, accounts.Account{ID: uuid.NewString(), Username: "admin"}, "irrelevant", now)
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, auth.ErrSetupComplete) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("setup winners=%d", wins.Load())
	}
	var count int
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&count))
	if count != 1 {
		t.Fatal("multiple administrators")
	}
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM setup_grants`).Scan(&count))
	if count != 0 {
		t.Fatal("grants survived setup")
	}
	_, err = f.conn.Exec(ctx, `UPDATE accounts SET disabled_at=now(),direct_permissions='{}'`)
	must(t, err)
	complete, err := f.store.SetupComplete(ctx)
	must(t, err)
	if !complete {
		t.Fatal("setup reopened")
	}
	if err = f.store.GrantSetup(ctx, auth.Digest("new"), now.Add(time.Hour)); !errors.Is(err, auth.ErrSetupComplete) {
		t.Fatal(err)
	}
	_, code, err := auth.New(ctx, second)
	must(t, err)
	if code != "" {
		t.Fatal("restart regenerated setup code")
	}
}
func TestExpiredGrant(t *testing.T) {
	f := database(t)
	now := time.Now().UTC()
	grant := auth.Digest("expired")
	must(t, f.store.GrantSetup(t.Context(), grant, now))
	err := f.store.CompleteSetup(t.Context(), grant, accounts.Account{ID: uuid.NewString(), Username: "admin"}, "hash", now)
	if !errors.Is(err, auth.ErrSetupGrant) {
		t.Fatal(err)
	}
	complete, err := f.store.SetupComplete(t.Context())
	must(t, err)
	if complete {
		t.Fatal("expired grant completed setup")
	}
}
func TestSessions(t *testing.T) {
	f := database(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	a := bootstrap(t, f, now)
	cases := []struct {
		name                   string
		created, last, expires time.Time
		valid                  bool
	}{
		{"active", now.Add(-time.Hour), now.Add(-time.Hour), now.Add(time.Hour), true},
		{"idle-boundary", now.Add(-8 * 24 * time.Hour), now.Add(-auth.SessionIdle), now.Add(time.Hour), false},
		{"absolute-boundary", now.Add(-auth.SessionLifetime), now.Add(-time.Minute), now, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hash := auth.Digest(tc.name)
			must(t, f.store.CreateSession(ctx, auth.Session{Digest: hash, AccountID: a.ID, CreatedAt: tc.created, LastSeenAt: tc.last, ExpiresAt: tc.expires}))
			got, err := f.store.UseSession(ctx, hash, now, now.Add(-auth.SessionIdle))
			if tc.valid {
				must(t, err)
				if got.ID != a.ID {
					t.Fatal("wrong account")
				}
				var seen, expires time.Time
				must(t, f.conn.QueryRow(ctx, `SELECT last_seen_at,expires_at FROM browser_sessions WHERE token_hash=$1`, hash).Scan(&seen, &expires))
				if !seen.Equal(now) || !expires.Equal(tc.expires) {
					t.Fatal("activity extended absolute deadline or did not refresh idle")
				}
			} else if !errors.Is(err, auth.ErrNotFound) {
				t.Fatalf("expired session accepted: %v", err)
			}
		})
	}
	must(t, f.store.DeleteSession(ctx, auth.Digest("active")))
	if _, err := f.store.UseSession(ctx, auth.Digest("active"), now, now.Add(-auth.SessionIdle)); !errors.Is(err, auth.ErrNotFound) {
		t.Fatal("revoked session accepted")
	}
	hash := auth.Digest("disabled")
	must(t, f.store.CreateSession(ctx, auth.Session{Digest: hash, AccountID: a.ID, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}))
	_, err := f.conn.Exec(ctx, `UPDATE accounts SET disabled_at=$1 WHERE id=$2`, now, a.ID)
	must(t, err)
	if _, err = f.store.UseSession(ctx, hash, now, now.Add(-auth.SessionIdle)); !errors.Is(err, auth.ErrNotFound) {
		t.Fatal("disabled account authenticated")
	}
	if credential, err := f.store.CredentialByUsername(ctx, "jack"); err != nil || credential.Account.DisabledAt == nil {
		t.Fatal("disabled credential status unavailable to the verifier", err)
	}
	must(t, f.store.Cleanup(ctx, now, now.Add(-auth.SessionIdle)))
	var expired int
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM browser_sessions WHERE expires_at<=$1 OR last_seen_at<=$2`, now, now.Add(-auth.SessionIdle)).Scan(&expired))
	if expired != 0 {
		t.Fatal("expired sessions remain")
	}
}
func TestSharedRateLimit(t *testing.T) {
	f := database(t)
	second, err := postgres.Open(t.Context(), f.url)
	must(t, err)
	defer second.Close()
	var allowed atomic.Int32
	var wg sync.WaitGroup
	now := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := f.store
			if i%2 == 0 {
				store = second
			}
			ok, err := store.Attempt(t.Context(), auth.Digest("shared"), 5, time.Minute, now)
			if err != nil {
				t.Error(err)
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 5 {
		t.Fatalf("allowed=%d", allowed.Load())
	}
	ok, err := second.Attempt(t.Context(), auth.Digest("shared"), 5, time.Minute, now.Add(time.Minute))
	must(t, err)
	if !ok {
		t.Fatal("window did not reset")
	}
}

type client struct {
	t       *testing.T
	handler http.Handler
	cookies map[string]*http.Cookie
}

func (c *client) request(method, path, body string, status int) *httptest.ResponseRecorder {
	c.t.Helper()
	r := httptest.NewRequest(method, "http://localhost:8081/api/"+path, strings.NewReader(body))
	r.Header.Set("Origin", "http://localhost:8081")
	r.Header.Set("Content-Type", "application/json")
	for _, cookie := range c.cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != status {
		c.t.Fatalf("%s %s: %d expected %d: %s", method, path, w.Code, status, w.Body.String())
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(c.cookies, cookie.Name)
		} else {
			c.cookies[cookie.Name] = cookie
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		c.t.Fatal("API can be cached")
	}
	return w
}
func body(v any) string { b, _ := json.Marshal(v); return string(b) }
func TestHTTPAccountLifecycle(t *testing.T) {
	f := database(t)
	service, code, err := auth.New(t.Context(), f.store)
	must(t, err)
	cfg, err := config.Parse("http://localhost:8081", "")
	must(t, err)
	c := client{t, httpapi.New(service, securityForTest(t, f, service, cfg), accounts.NewProfileService(f.store), nil, cfg), map[string]*http.Cookie{}}
	c.request("GET", "account", "", 401)
	c.request("POST", "setup/complete", `{"username":"jack","password":"silver meadow orbit lamp"}`, 403)
	c.request("POST", "setup/unlock", `{"code":"wrong"}`, 422)
	if len(code) != 8 {
		t.Fatalf("operator code length: %d", len(code))
	}
	c.request("POST", "setup/unlock", body(map[string]string{"code": " " + strings.ToLower(code) + " "}), 200)
	if len(c.cookies) != 1 || !c.cookies["acta_setup"].HttpOnly {
		t.Fatal("missing protected setup grant")
	}
	c.request("POST", "setup/complete", `{"username":"jack/reviewer","password":"silver meadow orbit lamp"}`, 422)
	c.request("POST", "setup/complete", `{"username":"jack","password":"short"}`, 422)
	c.request("POST", "setup/complete", `{"username":"jack","password":"silver meadow orbit lamp","display_name":"two\nlines"}`, 422)
	c.request("POST", "setup/complete", `{"username":"JACK","password":"silver meadow orbit lamp","display_name":"  Cafe\u0301 · Jack  "}`, 201)
	if len(c.cookies) != 0 {
		t.Fatal("setup auto-signed in or retained grant")
	}
	c.request("GET", "account", "", 401)
	c.request("POST", "setup/complete", `{"username":"second","password":"silver meadow orbit lamp"}`, 409)
	wrong := c.request("POST", "login", `{"username":"jack","password":"incorrect"}`, 401).Body.String()
	unknown := c.request("POST", "login", `{"username":"unknown","password":"incorrect"}`, 401).Body.String()
	if wrong != unknown {
		t.Fatal("login enumerates accounts")
	}
	c.request("POST", "login", `{"username":"JACK","password":"silver meadow orbit lamp"}`, 200)
	cookie := c.cookies["acta_session"]
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != int(auth.SessionLifetime.Seconds()) {
		t.Fatal("session cookie policy")
	}
	var storedHash []byte
	must(t, f.conn.QueryRow(t.Context(), `SELECT token_hash FROM browser_sessions`).Scan(&storedHash))
	if string(storedHash) != string(auth.Digest(cookie.Value)) {
		t.Fatal("session token not stored as digest")
	}
	var account map[string]any
	must(t, json.Unmarshal(c.request("GET", "account", "", 200).Body.Bytes(), &account))
	if account["username"] != "jack" || account["permissions"] == nil || account["display_name"] != "Café · Jack" {
		t.Fatal(account)
	}
	restarted, code, err := auth.New(t.Context(), f.store)
	must(t, err)
	if code != "" {
		t.Fatal("setup code after restart")
	}
	c.handler = httpapi.New(restarted, securityForTest(t, f, restarted, cfg), accounts.NewProfileService(f.store), nil, cfg)
	c.request("GET", "account", "", 200)
	c.request("POST", "login", `{"username":"jack","password":"silver meadow orbit lamp"}`, 200)
	if _, err = service.Current(t.Context(), cookie.Value); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("replaced session remains valid")
	}
	current := c.cookies["acta_session"].Value
	c.request("POST", "logout", `{}`, 200)
	c.request("GET", "account", "", 401)
	if _, err = service.Current(t.Context(), current); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("logout only cleared cookie")
	}
	c.request("POST", "logout", `{}`, 200)
}
func TestHTTPRejectsForgedAndMalformedRequests(t *testing.T) {
	f := database(t)
	service, _, err := auth.New(t.Context(), f.store)
	must(t, err)
	cfg, err := config.Parse("https://acta.example.test", "")
	must(t, err)
	handler := httpapi.New(service, securityForTest(t, f, service, cfg), accounts.NewProfileService(f.store), nil, cfg)
	for _, tc := range []struct {
		name, origin, host, contentType, body string
		status                                int
	}{
		{"cross-origin", "https://evil.test", "acta.example.test", "application/json", "{}", 403},
		{"missing-origin", "", "acta.example.test", "application/json", "{}", 403},
		{"wrong-host", "https://acta.example.test", "evil.test", "application/json", "{}", 400},
		{"form", "https://acta.example.test", "acta.example.test", "text/plain", "{}", 415},
		{"unknown-field", "https://acta.example.test", "acta.example.test", "application/json", `{"surprise":true}`, 400},
		{"trailing-json", "https://acta.example.test", "acta.example.test", "application/json", `{} {}`, 400},
		{"oversized", "https://acta.example.test", "acta.example.test", "application/json", `{"username":"` + strings.Repeat("a", 70000) + `"}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "https://"+tc.host+"/api/login", strings.NewReader(tc.body))
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
		})
	}
	// HTTPS setup grants must use a host-bound Secure cookie.
	service, code, err := auth.New(t.Context(), f.store)
	must(t, err)
	handler = httpapi.New(service, securityForTest(t, f, service, cfg), accounts.NewProfileService(f.store), nil, cfg)
	r := httptest.NewRequest("POST", "https://acta.example.test/api/setup/unlock", strings.NewReader(body(map[string]string{"code": code})))
	r.Header.Set("Origin", "https://acta.example.test")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "__Host-acta_setup" || !cookies[0].Secure || cookies[0].Domain != "" || cookies[0].Path != "/" {
		t.Fatal("HTTPS cookie not host-bound")
	}
}
func TestMigrationMismatchFailsClosed(t *testing.T) {
	f := database(t)
	_, err := f.conn.Exec(t.Context(), `UPDATE schema_migrations SET checksum='changed'`)
	must(t, err)
	store, err := postgres.Open(t.Context(), f.url)
	if err == nil {
		store.Close()
		t.Fatal("changed migration accepted")
	}
}

func securityForTest(t *testing.T, f fixture, service *auth.Service, cfg config.Config) *auth.Security {
	t.Helper()
	origins := []string{}
	for origin := range cfg.Origins {
		origins = append(origins, origin)
	}
	security, err := auth.NewSecurity(t.Context(), service, f.store, cfg.PublicURL, origins, []byte(strings.Repeat("k", 32)))
	must(t, err)
	return security
}
