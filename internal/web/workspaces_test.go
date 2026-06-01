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
	if loc := resp.Header.Get("Location"); loc != "/w/general" {
		t.Fatalf("want redirect to /w/general, got %q", loc)
	}
}

func TestWorkspacePageRenders(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	body := getBody(t, client, base+"/w/general", http.StatusOK)
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

	resp, err := client.Get(base + "/w/nope")
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
	if !strings.Contains(list, "Engineering") || !strings.Contains(list, "/w/engineering") {
		t.Fatalf("settings page missing the new workspace:\n%s", list)
	}
	page := getBody(t, client, base+"/w/engineering", http.StatusOK)
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
	// Slug is immutable, so the original URL still resolves.
	getBody(t, client, base+"/w/engineering", http.StatusOK)
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
	getBody(t, client, base+"/w/general", http.StatusOK)
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

	r, err := client.Get(base + "/w/engineering")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted workspace should 404, got %d", r.StatusCode)
	}
}
