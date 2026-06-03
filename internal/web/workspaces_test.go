package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// login signs the seeded "jack" account in (seating the session cookie). The
// password flow redirects first-time users with no passkey to the welcome
// interstitial; either way the session is established.
func login(t *testing.T, client *http.Client, base, token string) {
	t.Helper()
	resp := postForm(t, client, base+"/login/password", url.Values{
		"username": {"jack"}, "password": {testPassword}, "csrf_token": {token},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login: want 303, got %d", resp.StatusCode)
	}
}

// wsRowRe pairs a workspace's row id (from its rename form action) with the
// name shown in that row's input, so a test can find the id for a given name.
var wsRowRe = regexp.MustCompile(`/settings/workspaces/([a-z0-9]+)/rename"[\s\S]*?name="name" value="([^"]+)"`)

func workspaceID(t *testing.T, client *http.Client, base, name string) string {
	t.Helper()
	body := getBody(t, client, base+"/settings/workspaces", http.StatusOK)
	for _, m := range wsRowRe.FindAllStringSubmatch(body, -1) {
		if m[2] == name {
			return m[1]
		}
	}
	t.Fatalf("workspace %q not found on settings page", name)
	return ""
}

func TestRootRedirectsToWorkspace(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	resp, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("want 303 from /, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/general" {
		t.Fatalf("want redirect to /general, got %q", loc)
	}
}

func TestWorkspacePageRenders(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	body := getBody(t, client, base+"/general", http.StatusOK)
	if !strings.Contains(body, "General") {
		t.Error("workspace page missing the workspace name")
	}
	// The switcher and nav are present on a signed-in workspace page.
	if !strings.Contains(body, `class="wsmenu"`) {
		t.Error("workspace page missing the switcher dropdown")
	}
	if !strings.Contains(body, `href="/settings/workspaces"`) {
		t.Error("switcher missing the manage-workspaces link")
	}
}

func TestUnknownWorkspaceIs404(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	resp, err := client.Get(base + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for unknown workspace, got %d", resp.StatusCode)
	}
}

func TestWorkspaceCreateAndVisit(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	resp := postForm(t, client, base+"/settings/workspaces", url.Values{
		"name": {"Engineering"}, "csrf_token": {token},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/settings/workspaces" {
		t.Fatalf("create: want 303 to /settings/workspaces, got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	// It now lists on the settings page and is reachable at its slug.
	list := getBody(t, client, base+"/settings/workspaces", http.StatusOK)
	if !strings.Contains(list, "Engineering") || !strings.Contains(list, "/engineering") {
		t.Fatalf("settings page missing the new workspace:\n%s", list)
	}
	page := getBody(t, client, base+"/engineering", http.StatusOK)
	if !strings.Contains(page, "Engineering") {
		t.Error("new workspace page not rendering its name")
	}
}

func TestWorkspaceDuplicateNameRejected(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	// "general" collides case-insensitively with the seeded "General".
	resp := postForm(t, client, base+"/settings/workspaces", url.Values{
		"name": {"general"}, "csrf_token": {token},
	})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/settings/workspaces?err=name_taken" {
		t.Fatalf("want name_taken redirect, got %q", loc)
	}
}

func TestWorkspaceRenameKeepsSlug(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	postForm(t, client, base+"/settings/workspaces", url.Values{
		"name": {"Engineering"}, "csrf_token": {token},
	}).Body.Close()
	id := workspaceID(t, client, base, "Engineering")

	resp := postForm(t, client, base+"/settings/workspaces/"+id+"/rename", url.Values{
		"name": {"Platform"}, "csrf_token": {token},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("rename: want 303, got %d", resp.StatusCode)
	}

	list := getBody(t, client, base+"/settings/workspaces", http.StatusOK)
	if !strings.Contains(list, "Platform") {
		t.Error("renamed workspace not showing new name")
	}
	// A name-only edit (no slug field posted) leaves the slug untouched, so the
	// original URL still resolves.
	getBody(t, client, base+"/engineering", http.StatusOK)
}

func TestCannotDeleteLastWorkspace(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	id := workspaceID(t, client, base, "General")
	resp := postForm(t, client, base+"/settings/workspaces/"+id+"/delete", url.Values{
		"csrf_token": {token},
	})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/settings/workspaces?err=last" {
		t.Fatalf("deleting the only workspace: want err=last redirect, got %q", loc)
	}
	// Still there.
	getBody(t, client, base+"/general", http.StatusOK)
}

func TestDeleteWorkspaceRemovesIt(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	postForm(t, client, base+"/settings/workspaces", url.Values{
		"name": {"Engineering"}, "csrf_token": {token},
	}).Body.Close()
	id := workspaceID(t, client, base, "Engineering")

	resp := postForm(t, client, base+"/settings/workspaces/"+id+"/delete", url.Values{
		"csrf_token": {token},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/settings/workspaces" {
		t.Fatalf("delete: want 303 to /settings/workspaces, got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	r, err := client.Get(base + "/engineering")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted workspace should 404, got %d", r.StatusCode)
	}
}

// TestLegacyWorkspaceRedirect checks the retired /w/{slug} URLs 301 to their new
// /{slug} home with the path tail and query preserved, so old links survive.
func TestLegacyWorkspaceRedirect(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	cases := []struct{ from, want string }{
		{"/w/general", "/general"},
		{"/w/general/activity", "/general/activity"},
		{"/w/general?item=abc123", "/general?item=abc123"},
		{"/w/general/items/xyz/modal", "/general/items/xyz/modal"},
	}
	for _, c := range cases {
		resp, err := client.Get(base + c.from)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMovedPermanently {
			t.Fatalf("%s: want 301, got %d", c.from, resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != c.want {
			t.Fatalf("%s: want redirect to %q, got %q", c.from, c.want, loc)
		}
	}
}

// TestWorkspaceSlugRename re-slugs a workspace and confirms the board moves to
// the new URL (with the typed value normalised) while the old slug 404s.
func TestWorkspaceSlugRename(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	postForm(t, client, base+"/settings/workspaces", url.Values{
		"name": {"Engineering"}, "csrf_token": {token},
	}).Body.Close()
	id := workspaceID(t, client, base, "Engineering")

	// "Platform Team" must normalise to the slug "platform-team".
	resp := postForm(t, client, base+"/settings/workspaces/"+id+"/rename", url.Values{
		"name": {"Engineering"}, "slug": {"Platform Team"}, "csrf_token": {token},
	})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/settings/workspaces" {
		t.Fatalf("slug rename: want clean redirect, got %q", loc)
	}

	getBody(t, client, base+"/platform-team", http.StatusOK)
	r, err := client.Get(base + "/engineering")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("old slug should 404 after re-slug, got %d", r.StatusCode)
	}
}

// TestWorkspaceSlugRenameRejectsReserved refuses a slug that would shadow a
// built-in route, leaving the existing slug in place.
func TestWorkspaceSlugRenameRejectsReserved(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	postForm(t, client, base+"/settings/workspaces", url.Values{
		"name": {"Engineering"}, "csrf_token": {token},
	}).Body.Close()
	id := workspaceID(t, client, base, "Engineering")

	resp := postForm(t, client, base+"/settings/workspaces/"+id+"/rename", url.Values{
		"name": {"Engineering"}, "slug": {"settings"}, "csrf_token": {token},
	})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/settings/workspaces?err=slug_reserved" {
		t.Fatalf("want slug_reserved redirect, got %q", loc)
	}
	// Slug unchanged — the original still resolves.
	getBody(t, client, base+"/engineering", http.StatusOK)
}

// TestWorkspaceSlugRenameRejectsTaken refuses a slug already used by another
// workspace.
func TestWorkspaceSlugRenameRejectsTaken(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	postForm(t, client, base+"/settings/workspaces", url.Values{
		"name": {"Engineering"}, "csrf_token": {token},
	}).Body.Close()
	id := workspaceID(t, client, base, "Engineering")

	// "general" is the seeded workspace's slug — taking it must be refused.
	resp := postForm(t, client, base+"/settings/workspaces/"+id+"/rename", url.Values{
		"name": {"Engineering"}, "slug": {"general"}, "csrf_token": {token},
	})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/settings/workspaces?err=slug_taken" {
		t.Fatalf("want slug_taken redirect, got %q", loc)
	}
	getBody(t, client, base+"/engineering", http.StatusOK)
}

// TestWorkspaceCreateAvoidsReservedSlug confirms auto-derived slugs dodge the
// reserved set: "API" can't take /api, so it falls through to /api-2.
func TestWorkspaceCreateAvoidsReservedSlug(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	postForm(t, client, base+"/settings/workspaces", url.Values{
		"name": {"API"}, "csrf_token": {token},
	}).Body.Close()

	list := getBody(t, client, base+"/settings/workspaces", http.StatusOK)
	if strings.Contains(list, `href="/api"`) {
		t.Error("workspace took the reserved /api slug")
	}
	if !strings.Contains(list, "/api-2") {
		t.Fatalf("expected reserved slug to fall through to /api-2:\n%s", list)
	}
	getBody(t, client, base+"/api-2", http.StatusOK)
}
