package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// The board page must load board-prefs.js (which restores the saved view mode +
// filters from localStorage) and the view-toggle controls must use explicit
// ?mode= links, so a deliberate click is never mistaken for a bare visit and
// bounced back by the restore.
func TestBoardPrefsWiring(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	body := getBody(t, client, base+"/general", http.StatusOK)

	if !strings.Contains(body, "/static/board-prefs.js") {
		t.Fatalf("board page doesn't load board-prefs.js:\n%s", body)
	}
	// Status toggle is explicit, not a bare /general link.
	if !strings.Contains(body, `href="/general?mode=status"`) {
		t.Fatalf("Status mode link isn't explicit (?mode=status):\n%s", body)
	}
	if !strings.Contains(body, `href="/general?mode=milestone"`) {
		t.Fatalf("Milestone mode link missing:\n%s", body)
	}
	// Clear carries the current mode so the no-JS reset isn't a bare visit.
	if !strings.Contains(body, `class="facet-clear" href="/general?mode=status"`) {
		t.Fatalf("Clear link doesn't carry the mode:\n%s", body)
	}
}
