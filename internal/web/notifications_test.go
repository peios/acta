package web_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// newClient returns a fresh cookie-jar client that doesn't auto-follow
// redirects, so a second principal can hold their own session.
func newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// loginAs signs the client in as username (password testPassword) and returns
// the CSRF token seated for that client.
func loginAs(t *testing.T, client *http.Client, base, username string) string {
	t.Helper()
	token := csrfToken(t, client, base)
	resp := postForm(t, client, base+"/login/password", url.Values{
		"username": {username}, "password": {testPassword}, "csrf_token": {token},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("loginAs %s: want 303, got %d", username, resp.StatusCode)
	}
	return token
}

// the badge is a <span class="bell-dot">; matching the attribute (not the bare
// string) avoids colliding with the ".bell-dot" CSS rule in the same document.
const badgeMarker = `class="bell-dot"`

var notifOpenRe = regexp.MustCompile(`href="(/notifications/[^/"]+/open\?to=[^"]+)"`)

func TestNotificationBellEndToEnd(t *testing.T) {
	base, jack := newTestServer(t)
	jackTok := signIn(t, jack, base)

	// Jack creates a second human, Robin, who can be mentioned and sign in.
	resp := postForm(t, jack, base+"/settings/principals", url.Values{
		"username": {"robin"}, "display": {"Robin"},
		"password": {testPassword}, "csrf_token": {jackTok},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create principal: want 303, got %d", resp.StatusCode)
	}

	// Jack opens an item and @-mentions Robin in a comment.
	todo := statusID(t, jack, base, "To do")
	item := makeItem(t, jack, base, jackTok, todo, "Ship the bell")
	cr := postJSON(t, jack, base+"/w/general/items/"+item+"/comment", jackTok,
		map[string]any{"body": "hey @robin take a look"})
	if cr.StatusCode != http.StatusOK {
		t.Fatalf("comment: want 200, got %d", cr.StatusCode)
	}
	cr.Body.Close()

	// Robin signs in on a fresh client and sees the bell light up.
	robin := newClient(t)
	robinTok := loginAs(t, robin, base, "robin")
	board := getBody(t, robin, base+"/w/general", http.StatusOK)
	if !strings.Contains(board, badgeMarker) {
		t.Fatal("robin's bell shows no unread badge")
	}
	if !strings.Contains(board, "mentioned you") || !strings.Contains(board, "<b>Jack</b>") {
		t.Fatal("bell missing the mention row (actor/verb)")
	}
	m := notifOpenRe.FindStringSubmatch(board)
	if m == nil {
		t.Fatal("no notification open link in the bell")
	}

	// Clicking it marks the row read and redirects to the item's deep link.
	open, err := robin.Get(base + m[1])
	if err != nil {
		t.Fatal(err)
	}
	open.Body.Close()
	if open.StatusCode != http.StatusSeeOther {
		t.Fatalf("open: want 303, got %d", open.StatusCode)
	}
	if loc := open.Header.Get("Location"); loc != "/w/general?item="+item {
		t.Fatalf("open redirect = %q, want /w/general?item=%s", loc, item)
	}

	// The badge is gone once the notification is read.
	board = getBody(t, robin, base+"/w/general", http.StatusOK)
	if strings.Contains(board, badgeMarker) {
		t.Fatal("badge still present after opening the notification")
	}

	// A fresh mention re-lights it; mark-all-read clears the batch and returns
	// to the page it was submitted from.
	cr = postJSON(t, jack, base+"/w/general/items/"+item+"/comment", jackTok,
		map[string]any{"body": "@robin one more"})
	cr.Body.Close()
	board = getBody(t, robin, base+"/w/general", http.StatusOK)
	if !strings.Contains(board, badgeMarker) {
		t.Fatal("second mention did not light the bell")
	}
	ra := postForm(t, robin, base+"/notifications/read-all", url.Values{
		"csrf_token": {robinTok}, "return_to": {"/w/general"},
	})
	ra.Body.Close()
	if ra.StatusCode != http.StatusSeeOther || ra.Header.Get("Location") != "/w/general" {
		t.Fatalf("read-all: want 303 to /w/general, got %d %q", ra.StatusCode, ra.Header.Get("Location"))
	}
	board = getBody(t, robin, base+"/w/general", http.StatusOK)
	if strings.Contains(board, badgeMarker) {
		t.Fatal("badge still present after mark-all-read")
	}
}
