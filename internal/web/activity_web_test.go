package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// Creating and moving an item over the web routes should surface on the
// workspace activity feed and in the item's modal history.
func TestActivityFeedAndModalHistory(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	todo := statusID(t, client, base, "To do")
	doing := statusID(t, client, base, "Doing")
	id := decodeID(t, postJSON(t, client, base+"/general/items", token, map[string]any{
		"status_id": todo, "title": "Trace activity",
	}))
	resp := postJSON(t, client, base+"/general/items/"+id+"/move", token, map[string]any{
		"status_id": doing, "index": 0,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("move: want 204, got %d", resp.StatusCode)
	}

	// The feed shows the move, the item title, and links back to the item.
	feed := getBody(t, client, base+"/general/activity", http.StatusOK)
	if !strings.Contains(feed, "Trace activity") {
		t.Fatalf("activity feed missing item title:\n%s", feed)
	}
	if !strings.Contains(feed, "moved from To do to Doing") {
		t.Fatalf("activity feed missing the status-change summary:\n%s", feed)
	}
	if !strings.Contains(feed, "?item="+id) {
		t.Fatalf("activity feed entry doesn't link to the item")
	}

	// The modal carries the same history under an Activity section.
	modal := getBody(t, client, base+"/general/items/"+id+"/modal", http.StatusOK)
	if !strings.Contains(modal, "Activity") || !strings.Contains(modal, "created this in To do") {
		t.Fatalf("item modal missing activity history:\n%s", modal)
	}
}
