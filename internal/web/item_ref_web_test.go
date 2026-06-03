package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestItemHumanIDDisplayAndResolution covers the web side of human-readable
// ids: the board card shows PREFIX-N, and ?item= resolves by the human ref, a
// bare number, and the opaque id alike. The seeded "General" workspace derives
// the prefix GEN, so its first item is GEN-1.
func TestItemHumanIDDisplayAndResolution(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	id := makeItem(t, client, base, token, statusID(t, client, base, "To do"), "First task")

	board := getBody(t, client, base+"/general", http.StatusOK)
	if !strings.Contains(board, "GEN-1") {
		t.Errorf("board card missing human id GEN-1:\n%s", board)
	}

	for _, ref := range []string{"GEN-1", "gen-1", "1", id} {
		page := getBody(t, client, base+"/general?item="+ref, http.StatusOK)
		if !strings.Contains(page, "First task") {
			t.Errorf("?item=%s did not open the item modal", ref)
		}
	}

	// A wrong prefix doesn't resolve to another workspace's numbering.
	noModal := getBody(t, client, base+"/general?item=WRONG-1", http.StatusOK)
	if strings.Contains(noModal, `data-item-modal-id`) {
		t.Error("?item=WRONG-1 should not open a modal in this workspace")
	}
}
