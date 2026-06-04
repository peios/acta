package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestCardArchiveRevealedOnTouch guards the mobile-archive fix (ACT-15). The
// card archive button (.item-del) is hover-revealed on the desktop board; touch
// devices have no hover, so it must be shown via @media (hover: none) or there's
// no card-level way to archive on mobile.
func TestCardArchiveRevealedOnTouch(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	body := getBody(t, client, base+"/general", http.StatusOK)
	if !strings.Contains(body, ".item-del { opacity: 1; padding:") {
		t.Error("card archive button not revealed on touch (@media (hover: none) .item-del) — archiving is unreachable on mobile")
	}
}
