package web_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// disableFormRe pulls a user's id out of their "disable" form action. Only
// other users (not the viewer) get a disable form, so on a two-user page it
// uniquely identifies the non-self user.
var disableFormRe = regexp.MustCompile(`/settings/principals/([^/"]+)/disable`)

// attemptLogin logs in on a fresh client and returns the redirect response so
// the caller can tell success (-> away from /login) from a blocked sign-in.
func attemptLogin(t *testing.T, base, username, password string) (*http.Client, *http.Response) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	token := csrfToken(t, c, base)
	resp := postForm(t, c, base+"/login/password", url.Values{
		"username": {username}, "password": {password}, "csrf_token": {token},
	})
	return c, resp
}

func TestPrincipalCreateListAndLogin(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)

	resp := postForm(t, client, base+"/settings/principals", url.Values{
		"username": {"Bob"}, "display": {"Bob Jones"}, "password": {"bob-passw0rd"}, "csrf_token": {csrf},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/settings/principals" {
		t.Fatalf("create: want 303 to /settings/principals, got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	page := getBody(t, client, base+"/settings/principals", http.StatusOK)
	if !strings.Contains(page, "Bob Jones") || !strings.Contains(page, "bob") {
		t.Fatalf("new user missing from list:\n%s", page)
	}

	// The created user can sign in (lands away from the login page).
	_, login := attemptLogin(t, base, "bob", "bob-passw0rd")
	login.Body.Close()
	if loc := login.Header.Get("Location"); strings.HasPrefix(loc, "/login") {
		t.Fatalf("new user could not sign in, bounced to %q", loc)
	}
}

func TestPrincipalDisableBlocksAccess(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)

	postForm(t, client, base+"/settings/principals", url.Values{
		"username": {"bob"}, "display": {"Bob"}, "password": {"bob-passw0rd"}, "csrf_token": {csrf},
	}).Body.Close()

	// Bob signs in and holds a live session.
	bob, login := attemptLogin(t, base, "bob", "bob-passw0rd")
	login.Body.Close()

	// Admin disables Bob (find his id from his row's disable form).
	page := getBody(t, client, base+"/settings/principals", http.StatusOK)
	m := disableFormRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no disable form on the page:\n%s", page)
	}
	bobID := m[1]
	resp := postForm(t, client, base+"/settings/principals/"+bobID+"/disable", url.Values{"csrf_token": {csrf}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("disable: want 303, got %d", resp.StatusCode)
	}

	// Bob's existing session is now dead — a protected page bounces to login.
	r, err := bob.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if loc := r.Header.Get("Location"); !strings.HasPrefix(loc, "/login") {
		t.Fatalf("disabled user's session still live, got %d %q", r.StatusCode, loc)
	}

	// And a fresh sign-in is refused with the disabled message.
	_, login2 := attemptLogin(t, base, "bob", "bob-passw0rd")
	login2.Body.Close()
	if loc := login2.Header.Get("Location"); !strings.Contains(loc, "account_disabled") {
		t.Fatalf("disabled login: want account_disabled redirect, got %q", loc)
	}

	// Re-enable restores sign-in.
	postForm(t, client, base+"/settings/principals/"+bobID+"/enable", url.Values{"csrf_token": {csrf}}).Body.Close()
	_, login3 := attemptLogin(t, base, "bob", "bob-passw0rd")
	login3.Body.Close()
	if loc := login3.Header.Get("Location"); strings.HasPrefix(loc, "/login") {
		t.Fatalf("re-enabled user could not sign in, got %q", loc)
	}
}

func TestPrincipalSelfDisableBlocked(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)

	// A second active human so the last-active guard isn't what stops self-disable.
	postForm(t, client, base+"/settings/principals", url.Values{
		"username": {"bob"}, "display": {"Bob"}, "password": {"bob-passw0rd"}, "csrf_token": {csrf},
	}).Body.Close()

	bobID := disableFormRe.FindStringSubmatch(getBody(t, client, base+"/settings/principals", http.StatusOK))[1]

	// Bob signs in. Grab his CSRF while logged out (the token survives login;
	// /login 303-redirects once authenticated, so it can't be scraped after).
	jar, _ := cookiejar.New(nil)
	bob := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	bobCSRF := csrfToken(t, bob, base)
	postForm(t, bob, base+"/login/password", url.Values{
		"username": {"bob"}, "password": {"bob-passw0rd"}, "csrf_token": {bobCSRF},
	}).Body.Close()

	// Bob, signed in, tries to disable his own account.
	resp := postForm(t, bob, base+"/settings/principals/"+bobID+"/disable", url.Values{"csrf_token": {bobCSRF}})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "err=self") {
		t.Fatalf("self-disable: want err=self, got %q", loc)
	}
}

func TestPrincipalCreateErrors(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)

	weak := postForm(t, client, base+"/settings/principals", url.Values{
		"username": {"shorty"}, "password": {"short"}, "csrf_token": {csrf},
	})
	weak.Body.Close()
	if loc := weak.Header.Get("Location"); !strings.Contains(loc, "weak_password") {
		t.Fatalf("weak password: want weak_password redirect, got %q", loc)
	}

	// "jack" is the seeded admin — creating it again collides.
	dup := postForm(t, client, base+"/settings/principals", url.Values{
		"username": {"jack"}, "password": {"another-passw0rd"}, "csrf_token": {csrf},
	})
	dup.Body.Close()
	if loc := dup.Header.Get("Location"); !strings.Contains(loc, "username_taken") {
		t.Fatalf("duplicate: want username_taken redirect, got %q", loc)
	}
}
