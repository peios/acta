package web_test

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// signIn fetches a CSRF token, logs client in as jack, and returns the token
// (still valid for subsequent same-jar form posts).
func signIn(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	return token
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

var (
	tokenValueRe  = regexp.MustCompile(`token-reveal-value">(acta_pat_[A-Za-z0-9_-]+)<`)
	tokenDeleteRe = regexp.MustCompile(`/account/tokens/([^/"]+)/delete`)
	sessRevokeRe  = regexp.MustCompile(`/account/sessions/([^/"]+)/revoke`)
)

// bearerGet does an unauthenticated-by-cookie request (fresh client, no jar)
// carrying only the bearer token — proving the API is token-authenticated.
func bearerGet(t *testing.T, base, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/me", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestAPITokenLifecycle(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)

	// The security page advertises the API tokens section.
	page := getBody(t, client, base+"/account/security", http.StatusOK)
	if !strings.Contains(page, "API tokens") {
		t.Fatal("security page missing API tokens section")
	}

	// Mint a token; the plaintext is revealed exactly once on the response page.
	resp := postForm(t, client, base+"/account/tokens", url.Values{
		"name": {"laptop cli"}, "csrf_token": {csrf},
	})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mint: want 200, got %d", resp.StatusCode)
	}
	m := tokenValueRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("minted token not revealed on page:\n%s", body)
	}
	plaintext := m[1]
	if !strings.Contains(body, "laptop cli") {
		t.Error("token name not listed after mint")
	}

	// The bearer token authenticates the JSON API as jack.
	resp = bearerGet(t, base, plaintext)
	me := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/v1/me with token: want 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(me, `"username":"jack"`) {
		t.Fatalf("/api/v1/me body missing principal: %s", me)
	}

	// No token and a bogus token are both rejected, identically.
	if r := bearerGet(t, base, ""); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/api/v1/me without token: want 401, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
	if r := bearerGet(t, base, "acta_pat_not-a-real-token"); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/api/v1/me bogus token: want 401, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}

	// Revoke the token (scrape its row id from the security page).
	page = getBody(t, client, base+"/account/security", http.StatusOK)
	dm := tokenDeleteRe.FindStringSubmatch(page)
	if dm == nil {
		t.Fatalf("no token delete control on page:\n%s", page)
	}
	resp = postForm(t, client, base+"/account/tokens/"+dm[1]+"/delete", url.Values{"csrf_token": {csrf}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("revoke: want 303, got %d", resp.StatusCode)
	}

	// The revoked token no longer authenticates.
	if r := bearerGet(t, base, plaintext); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/api/v1/me after revoke: want 401, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
}

func TestSessionRevoke(t *testing.T) {
	base, clientA := newTestServer(t)
	csrfA := signIn(t, clientA, base)

	// A second client logs in as the same user — a distinct session.
	jarB, _ := cookiejar.New(nil)
	clientB := &http.Client{
		Jar:           jarB,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	loginB := csrfToken(t, clientB, base)
	login(t, clientB, base, loginB)

	// From A, the security page lists its own session ("this device") plus B's,
	// which carries a revoke control.
	page := getBody(t, clientA, base+"/account/security", http.StatusOK)
	if !strings.Contains(page, "this device") {
		t.Fatal("session list missing current-device marker")
	}
	rm := sessRevokeRe.FindStringSubmatch(page)
	if rm == nil {
		t.Fatalf("no revocable other session listed:\n%s", page)
	}

	// Revoke B's session from A.
	resp := postForm(t, clientA, base+"/account/sessions/"+rm[1]+"/revoke", url.Values{"csrf_token": {csrfA}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("session revoke: want 303, got %d", resp.StatusCode)
	}

	// B is now logged out; A is still in.
	assertLoggedOut(t, clientB, base)
	assertLoggedIn(t, clientA, base)
}

func TestSessionRevokeOthers(t *testing.T) {
	base, clientA := newTestServer(t)
	csrfA := signIn(t, clientA, base)

	jarB, _ := cookiejar.New(nil)
	clientB := &http.Client{
		Jar:           jarB,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	loginB := csrfToken(t, clientB, base)
	login(t, clientB, base, loginB)

	resp := postForm(t, clientA, base+"/account/sessions/revoke-others", url.Values{"csrf_token": {csrfA}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("revoke-others: want 303, got %d", resp.StatusCode)
	}
	assertLoggedOut(t, clientB, base)
	assertLoggedIn(t, clientA, base)
}

// assertLoggedIn expects GET / to redirect into a workspace (303 to /w/…).
func assertLoggedIn(t *testing.T, client *http.Client, base string) {
	t.Helper()
	resp, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/w/") {
		t.Fatalf("expected logged-in redirect to /w/, got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

// assertLoggedOut expects GET / to bounce to the login page.
func assertLoggedOut(t *testing.T, client *http.Client, base string) {
	t.Helper()
	resp, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/login") {
		t.Fatalf("expected logged-out redirect to /login, got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}
