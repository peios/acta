package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestPullToRefreshScriptLoaded guards ACT-17: ptr.js (pull-to-refresh) is wired
// on nav pages like the board, and absent from the chrome-less login page.
func TestPullToRefreshScriptLoaded(t *testing.T) {
	base, client := newTestServer(t)

	if body := getBody(t, client, base+"/login", http.StatusOK); strings.Contains(body, "/static/ptr.js") {
		t.Error("ptr.js should not load on the login page (no nav)")
	}

	token := csrfToken(t, client, base)
	login(t, client, base, token)
	if body := getBody(t, client, base+"/general", http.StatusOK); !strings.Contains(body, "/static/ptr.js") {
		t.Error("ptr.js should load on nav pages (the board)")
	}
}
