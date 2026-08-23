package web_test

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/peios/acta/internal/account"
	"github.com/peios/acta/internal/agent"
	"github.com/peios/acta/internal/apitoken"
	"github.com/peios/acta/internal/authn/local"
	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/config"
	"github.com/peios/acta/internal/mcpcfg"
	"github.com/peios/acta/internal/memory"
	"github.com/peios/acta/internal/passkey"
	"github.com/peios/acta/internal/push"
	"github.com/peios/acta/internal/session"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
	"github.com/peios/acta/internal/web"
	"github.com/peios/acta/internal/workspace"
)

const testPassword = "s3cret-passw0rd"

// A real (throwaway) VAPID pair so the test handler runs with push enabled —
// enough to exercise the routes and the settings toggle. No payload is ever
// sent in tests (the in-memory store holds no subscriptions unless a test adds
// one), so these keys never reach a push service.
const (
	testVAPIDPublic  = "BFbdFE85plfk-WNX-NuwNFS65a83oB898guAMIfAtbajVFHhUWMdZ5HZtM-Rk63zVELCpwBIcdkXNQU_3nJ-imI"
	testVAPIDPrivate = "Z8MS_QrbTkj8J93i8LWxx1v--sBoPjbLUCws54x_Vdg"
)

func newTestServer(t *testing.T) (string, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(buildTestHandler(t))
	t.Cleanup(srv.Close)
	return srv.URL, newTestClient()
}

// newTestServerWithStore is newTestServer plus the underlying store, for tests
// that need to plant data the HTTP surface can't write — a progress history
// stretching back over days, say.
func newTestServerWithStore(t *testing.T) (string, *http.Client, *memstore.Store) {
	t.Helper()
	handler, ms := buildTestHandlerStore(t)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL, newTestClient(), ms
}

// newTestServerWriteTimeout is newTestServer with the production-style write
// timeout the real server runs (cmd/acta-server sets 15s). httptest's default
// is no timeout, which hides whether long-poll endpoints survive that ceiling.
func newTestServerWriteTimeout(t *testing.T, d time.Duration) (string, *http.Client) {
	t.Helper()
	srv := httptest.NewUnstartedServer(buildTestHandler(t))
	srv.Config.WriteTimeout = d
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL, newTestClient()
}

// buildTestHandler wires the full web handler over a fresh in-memory store with
// one user (jack) and a seeded "General" workspace.
func buildTestHandler(t *testing.T) http.Handler {
	h, _ := buildTestHandlerStore(t)
	return h
}

func buildTestHandlerStore(t *testing.T) (http.Handler, *memstore.Store) {
	t.Helper()
	ms := memstore.New()
	hash, err := local.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.CreateUser(context.Background(), store.NewUser{
		Username: "jack", Display: "Jack", PasswordHash: hash,
	}); err != nil {
		t.Fatal(err)
	}

	workspaces := workspace.New(ms)
	pushSender := push.New(ms, push.Config{
		PublicKey: testVAPIDPublic, PrivateKey: testVAPIDPrivate, Subject: "mailto:test@acta.test",
	})
	t.Cleanup(pushSender.Close)
	boards := board.New(ms, board.WithNotifier(pushSender))
	gen, err := workspaces.Create(context.Background(), "General", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := boards.SeedDefaults(context.Background(), gen.ID); err != nil {
		t.Fatal(err)
	}

	sessions := session.New(ms, session.Config{
		IdleTimeout:     time.Hour,
		AbsoluteTimeout: 24 * time.Hour,
	})
	passkeys, err := passkey.New(ms, passkey.Config{
		RPID: "localhost", RPOrigin: "http://localhost:8080", RPName: "Acta",
	})
	if err != nil {
		t.Fatal(err)
	}
	tokens := apitoken.New(ms)
	agents := agent.New(ms)
	accounts := account.New(ms)
	memories := memory.New(ms)
	mcpConfig := mcpcfg.New(ms)
	if err := mcpConfig.EnsureSeeded(context.Background()); err != nil {
		t.Fatal(err)
	}
	provider := local.NewProvider(ms, sessions, passkeys, false)
	return web.NewHandler(config.Config{Env: "dev", RPOrigin: "http://localhost:8080"}, sessions, provider, passkeys, tokens, agents, accounts, workspaces, boards, memories, mcpConfig, pushSender), ms
}

func newTestClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // don't follow; assert on 3xx directly
		},
	}
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// csrfToken fetches /login and scrapes the token (which also seats the CSRF
// cookie in the client's jar).
func csrfToken(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	body := getBody(t, client, base+"/login", http.StatusOK)
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no csrf token in login page")
	}
	return m[1]
}

func getBody(t *testing.T, client *http.Client, url string, wantStatus int) string {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s: want %d, got %d", url, wantStatus, resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func postForm(t *testing.T, client *http.Client, url string, vals url.Values) *http.Response {
	t.Helper()
	resp, err := client.PostForm(url, vals)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestProtectedRedirectsWhenLoggedOut(t *testing.T) {
	base, client := newTestServer(t)
	resp, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/login?return_to=") {
		t.Fatalf("want redirect to login carrying return_to, got %q", loc)
	}
}

func TestLoginPageRenders(t *testing.T) {
	base, client := newTestServer(t)
	body := getBody(t, client, base+"/login", http.StatusOK)
	if !strings.Contains(body, `action="/login/password"`) {
		t.Error("login page missing password form")
	}
	if !strings.Contains(body, `name="csrf_token"`) {
		t.Error("login page missing csrf field")
	}
}

func TestLoginMissingCSRFRejected(t *testing.T) {
	base, client := newTestServer(t)
	_ = csrfToken(t, client, base) // seat the csrf cookie, then omit the field
	resp := postForm(t, client, base+"/login/password", url.Values{
		"username": {"jack"}, "password": {testPassword},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403 without csrf token, got %d", resp.StatusCode)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	resp := postForm(t, client, base+"/login/password", url.Values{
		"username": {"jack"}, "password": {"wrong"}, "csrf_token": {token},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/login?err=invalid_credentials") {
		t.Fatalf("want invalid_credentials redirect, got %q", loc)
	}
	// Still not authenticated.
	resp2, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected still-logged-out after bad password, got %d", resp2.StatusCode)
	}
}

func TestUnknownUserSameOutcome(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	resp := postForm(t, client, base+"/login/password", url.Values{
		"username": {"nobody"}, "password": {"whatever"}, "csrf_token": {token},
	})
	defer resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/login?err=invalid_credentials") {
		t.Fatalf("unknown user should look identical to wrong password, got %q", loc)
	}
}

func TestLoginLogoutFlow(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)

	// Correct login -> 303 to /.
	resp := postForm(t, client, base+"/login/password", url.Values{
		"username": {"jack"}, "password": {testPassword}, "csrf_token": {token},
	})
	resp.Body.Close()
	// A user with no passkeys is sent to the "add a passkey" interstitial
	// first; the session is already established, so the protected pages serve.
	if resp.StatusCode != http.StatusSeeOther ||
		!strings.HasPrefix(resp.Header.Get("Location"), "/welcome/passkey") {
		t.Fatalf("login: want 303 to /welcome/passkey, got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	// "/" now redirects to the user's current workspace; follow it and confirm
	// the workspace page serves the signed-in identity.
	root, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	root.Body.Close()
	if root.StatusCode != http.StatusSeeOther {
		t.Fatalf("GET / after login: want 303, got %d", root.StatusCode)
	}
	home := getBody(t, client, base+root.Header.Get("Location"), http.StatusOK)
	if !strings.Contains(home, `id="board"`) {
		t.Fatalf("post-login landing is not the board:\n%s", home)
	}

	// Log out -> 303 to /login, session invalidated server-side.
	resp = postForm(t, client, base+"/logout", url.Values{"csrf_token": {token}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Fatalf("logout: want 303 to /login, got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	// After logout the protected page redirects again.
	resp2, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("after logout want 303, got %d", resp2.StatusCode)
	}
}

func TestAlreadyLoggedInSkipsLoginPage(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	resp := postForm(t, client, base+"/login/password", url.Values{
		"username": {"jack"}, "password": {testPassword}, "csrf_token": {token},
	})
	resp.Body.Close()

	// GET /login while authenticated -> redirect to /.
	r, err := client.Get(base + "/login")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/" {
		t.Fatalf("want /login to redirect to / when signed in, got %d %q", r.StatusCode, r.Header.Get("Location"))
	}
}
