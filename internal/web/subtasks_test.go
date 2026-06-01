package web_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestAPISubtasks(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)

	parent := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items", token,
		map[string]string{"title": "Parent"}), http.StatusCreated)

	sub := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items/"+parent.ID+"/subtasks", token,
		map[string]string{"title": "Child"}), http.StatusCreated)
	if sub.CreatedBy != "jack" {
		t.Fatalf("subtask created_by = %q, want jack", sub.CreatedBy)
	}
	if sub.ID == parent.ID {
		t.Fatal("subtask should be a distinct item")
	}

	// A subtask is not on the board listing (which is top-level only), but the
	// parent's listing entry carries the subtask count.
	listBody := readBody(t, bearerJSON(t, base, "GET", "/api/v1/w/general/items", token, nil))
	if strings.Contains(listBody, sub.ID) {
		t.Fatalf("subtask must not appear on the board listing:\n%s", listBody)
	}
	if !strings.Contains(listBody, `"subtasks_total":1`) {
		t.Fatalf("board listing should show the parent's subtask count:\n%s", listBody)
	}

	// item show returns the parent with the subtask nested and parent_id linked.
	showBody := readBody(t, bearerJSON(t, base, "GET", "/api/v1/w/general/items/"+parent.ID, token, nil))
	for _, want := range []string{`"subtasks"`, sub.ID, "Child", `"parent_id":"` + parent.ID + `"`} {
		if !strings.Contains(showBody, want) {
			t.Fatalf("item show missing %q:\n%s", want, showBody)
		}
	}

	// Unknown item / parent -> 404.
	if r := bearerJSON(t, base, "GET", "/api/v1/w/general/items/deadbeef", token, nil); r.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown item: want 404, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
	if r := bearerJSON(t, base, "POST", "/api/v1/w/general/items/deadbeef/subtasks", token, map[string]string{"title": "x"}); r.StatusCode != http.StatusNotFound {
		t.Fatalf("subtask on unknown parent: want 404, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
}
