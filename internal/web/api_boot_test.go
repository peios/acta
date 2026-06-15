package web_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAPIAgentBoot(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)
	sess := mcpConnect(t, base, token)

	// Seed memories the boot index should surface, in two different scopes.
	callTool[mcpMemoryT](t, sess, "memory_save", map[string]any{
		"scope": "agent", "name": "release-process",
		"summary": "tag vX.Y.Z, CI builds the image", "body": "Tag and push; CI does the rest.",
	})
	callTool[mcpMemoryT](t, sess, "memory_save", map[string]any{
		"scope": "site", "name": "house-style",
		"summary": "conventional commits, no co-author trailer", "body": "feat(scope): ...",
	})

	// The boot endpoint is bearer-authed and returns injectable markdown.
	resp, body := getWithToken(t, base, token, "/api/v1/agent/boot")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("boot: want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("boot content-type = %q, want text/markdown", ct)
	}
	for _, want := range []string{
		"# Your Acta memory",
		"You are **Jack**",    // human identity (display name), not an agent line
		"General (`general`)", // workspace list rendered inline
		"## agent (your own)",
		"**release-process** — tag vX.Y.Z, CI builds the image",
		"## site (instance-wide)",
		"**house-style** — conventional commits, no co-author trailer",
		"memory_get",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("boot output missing %q:\n%s", want, body)
		}
	}
	// It's an index, not a dump — memory bodies are not inlined.
	if strings.Contains(body, "CI does the rest") {
		t.Fatalf("boot output leaked a memory body:\n%s", body)
	}

	// Without a token it's a 401, like the rest of the API.
	resp2, _ := getWithToken(t, base, "", "/api/v1/agent/boot")
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated boot: want 401, got %d", resp2.StatusCode)
	}
}

// getWithToken does a cookie-less bearer GET to an arbitrary path and returns the
// response (body already drained) plus the body text.
func getWithToken(t *testing.T, base, token, path string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}
