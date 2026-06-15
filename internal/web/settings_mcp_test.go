package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSettingsGuideReadOnly(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)

	// The page previews the hardcoded guide and offers no way to edit it.
	body := getBody(t, client, base+"/settings/guide", http.StatusOK)
	if !strings.Contains(body, "MCP Guide") || !strings.Contains(body, "Using Acta") {
		t.Fatalf("guide page missing the shipped guide")
	}
	if strings.Contains(body, `name="guide"`) || strings.Contains(body, `action="/settings/guide"`) {
		t.Fatalf("guide page still has an editor; it must be read-only")
	}

	// There is no save route — POSTing is method-not-allowed.
	resp := postForm(t, client, base+"/settings/guide", url.Values{
		"guide": {"# House rules\nAlways read me."}, "csrf_token": {csrf},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("guide is read-only: want 405 on POST, got %d", resp.StatusCode)
	}

	// The MCP resource serves the hardcoded default, unaffected by the POST.
	token := mintToken(t, client, base, csrf)
	sess := mcpConnect(t, base, token)
	g := readResource(t, sess, "acta://guide")
	if !strings.Contains(g, "Using Acta") {
		t.Fatalf("MCP guide resource missing the shipped guide:\n%s", g)
	}
	if strings.Contains(g, "House rules") {
		t.Fatalf("MCP guide resource picked up a POSTed override; should be hardcoded")
	}
}

func TestSettingsPromptCRUD(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)

	// The seeded prompts are listed.
	body := getBody(t, client, base+"/settings/prompts", http.StatusOK)
	if !strings.Contains(body, "/mcp__acta__standup") {
		t.Fatalf("seeded standup prompt not listed")
	}

	// Create a new prompt.
	resp := postForm(t, client, base+"/settings/prompts", url.Values{
		"name":        {"release"},
		"title":       {"Release"},
		"description": {"Cut a release"},
		"body":        {"Ship {{version}} now."},
		"args":        {"version*: the tag"},
		"csrf_token":  {csrf},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create prompt: want 303, got %d", resp.StatusCode)
	}
	body = getBody(t, client, base+"/settings/prompts", http.StatusOK)
	if !strings.Contains(body, "/mcp__acta__release") {
		t.Fatalf("new prompt not listed")
	}

	// An invalid name re-renders the form with an error (no redirect).
	resp = postForm(t, client, base+"/settings/prompts", url.Values{
		"name": {"Bad Name"}, "body": {"x"}, "csrf_token": {csrf},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("invalid create: want 200 re-render, got %d", resp.StatusCode)
	}

	// Find the new prompt's id and delete it.
	id := promptIDFor(t, client, base, "release")
	resp = postForm(t, client, base+"/settings/prompts/"+id+"/delete", url.Values{"csrf_token": {csrf}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete prompt: want 303, got %d", resp.StatusCode)
	}
	body = getBody(t, client, base+"/settings/prompts", http.StatusOK)
	if strings.Contains(body, "/mcp__acta__release") {
		t.Fatalf("prompt still listed after delete")
	}
}

// promptIDFor scrapes the prompts list for the edit link of a named prompt.
func promptIDFor(t *testing.T, client *http.Client, base, name string) string {
	t.Helper()
	body := getBody(t, client, base+"/settings/prompts", http.StatusOK)
	marker := "/mcp__acta__" + name
	idx := strings.Index(body, marker)
	if idx < 0 {
		t.Fatalf("prompt %q not found in list", name)
	}
	// The id is in the href just before the slash name: /settings/prompts/{id}".
	href := `href="/settings/prompts/`
	start := strings.LastIndex(body[:idx], href)
	if start < 0 {
		t.Fatalf("edit link for %q not found", name)
	}
	start += len(href)
	end := strings.IndexByte(body[start:], '"')
	return body[start : start+end]
}
