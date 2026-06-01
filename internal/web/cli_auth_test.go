package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func authURL(base, redirect, state string) string {
	return base + "/cli/authorize?" + url.Values{"redirect_uri": {redirect}, "state": {state}}.Encode()
}

func TestCLIAuthorizePageLoopbackOnly(t *testing.T) {
	base, client := newTestServer(t)
	signIn(t, client, base)

	// A loopback redirect renders the authorize page.
	page := getBody(t, client, authURL(base, "http://127.0.0.1:5000/callback", "x"), http.StatusOK)
	if !strings.Contains(page, "Authorize") {
		t.Fatalf("authorize page missing Authorize control:\n%s", page)
	}

	// A non-loopback redirect is refused — the linchpin against token exfiltration.
	resp, err := client.Get(authURL(base, "http://evil.example.com/callback", "x"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-loopback redirect: want 400, got %d", resp.StatusCode)
	}
}

func TestCLIAuthorizeMintsAndRedirects(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)

	resp := postForm(t, client, base+"/cli/authorize", url.Values{
		"action":       {"authorize"},
		"redirect_uri": {"http://127.0.0.1:54321/callback"},
		"state":        {"st8"},
		"label":        {"acta CLI @ test"},
		"csrf_token":   {csrf},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("authorize: want 303, got %d", resp.StatusCode)
	}
	u, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "127.0.0.1:54321" {
		t.Fatalf("redirect host = %q, want loopback", u.Host)
	}
	if u.Query().Get("state") != "st8" {
		t.Fatalf("state not echoed back: %q", u.Query().Get("state"))
	}
	token := u.Query().Get("token")
	if !strings.HasPrefix(token, "acta_pat_") {
		t.Fatalf("no token in redirect: %q", resp.Header.Get("Location"))
	}

	// The handed-back token authenticates the API as jack.
	me := readBody(t, bearerGet(t, base, token))
	if !strings.Contains(me, `"username":"jack"`) {
		t.Fatalf("minted token doesn't authenticate: %s", me)
	}
}

func TestCLIAuthorizeCancel(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)

	resp := postForm(t, client, base+"/cli/authorize", url.Values{
		"action":       {"cancel"},
		"redirect_uri": {"http://127.0.0.1:54321/callback"},
		"state":        {"st8"},
		"csrf_token":   {csrf},
	})
	resp.Body.Close()
	u, _ := url.Parse(resp.Header.Get("Location"))
	if u.Query().Get("error") != "access_denied" {
		t.Fatalf("cancel should redirect with error=access_denied, got %q", resp.Header.Get("Location"))
	}
	if u.Query().Get("token") != "" {
		t.Fatal("cancel must not mint a token")
	}
}

func TestAPILogoutRevokesToken(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)

	if r := bearerJSON(t, base, "GET", "/api/v1/me", token, nil); r.StatusCode != http.StatusOK {
		t.Fatalf("token should work before logout, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
	if r := bearerJSON(t, base, "POST", "/api/v1/logout", token, nil); r.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: want 204, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
	if r := bearerJSON(t, base, "GET", "/api/v1/me", token, nil); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token should be revoked after logout, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
}
