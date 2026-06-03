package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAgentLifecycle(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)

	// Create an agent; the owner lands on its detail page.
	resp := postForm(t, client, base+"/account/agents", url.Values{
		"name": {"deploybot"}, "display": {"Deploy Bot"}, "csrf_token": {csrf},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create agent: want 303, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/account/agents/") {
		t.Fatalf("unexpected redirect %q", loc)
	}

	// The detail page shows the composed handle.
	page := getBody(t, client, base+loc, http.StatusOK)
	if !strings.Contains(page, "jack/deploybot") {
		t.Fatalf("agent detail missing handle:\n%s", page)
	}

	// Mint a token for the agent; the plaintext is revealed once.
	mint := postForm(t, client, base+loc+"/tokens", url.Values{"name": {"ci"}, "csrf_token": {csrf}})
	body := readBody(t, mint)
	m := tokenValueRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("agent token not revealed:\n%s", body)
	}

	// That token authenticates the API as the AGENT, not the owner.
	me := readBody(t, bearerGet(t, base, m[1]))
	if !strings.Contains(me, `"username":"jack/deploybot"`) {
		t.Fatalf("/api/v1/me is not the agent: %s", me)
	}
}

func TestAgentCreateRejectsBadName(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	resp := postForm(t, client, base+"/account/agents", url.Values{
		"name": {"Bad Name!"}, "csrf_token": {csrf},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "err=invalid_name") {
		t.Fatalf("want invalid_name redirect, got %q", loc)
	}
}

func TestItemRecordsCreator(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)

	// Creating an item over the JSON API (status_id omitted -> first lane).
	id := decodeID(t, postJSON(t, client, base+"/general/items", csrf, map[string]any{"title": "Ship it"}))

	modal := getBody(t, client, base+"/general/items/"+id+"/modal", http.StatusOK)
	if !strings.Contains(modal, "Created by") || !strings.Contains(modal, "Jack") {
		t.Fatalf("modal missing creator attribution:\n%s", modal)
	}
}
