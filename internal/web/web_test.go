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

	"github.com/peios/acta/internal/authn/local"
	"github.com/peios/acta/internal/config"
	"github.com/peios/acta/internal/session"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
	"github.com/peios/acta/internal/web"
)

const testPassword = "s3cret-passw0rd"

func newTestServer(t *testing.T) (string, *http.Client) {
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

	sessions := session.New(ms, session.Config{
		IdleTimeout:     time.Hour,
		AbsoluteTimeout: 24 * time.Hour,
	})
	provider := local.NewProvider(ms, sessions)
	handler := web.NewHandler(config.Config{Env: "dev"}, sessions, provider)

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // don't follow; assert on 3xx directly
		},
	}
	return srv.URL, client
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
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Fatalf("login: want 303 to /, got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	// Protected page now serves and renders the signed-in identity.
	home := getBody(t, client, base+"/", http.StatusOK)
	if !strings.Contains(home, "Jack") || !strings.Contains(home, "@jack") {
		t.Fatalf("home page not showing signed-in identity:\n%s", home)
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
