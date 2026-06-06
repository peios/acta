package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// hiddenByID maps each rendered card's item id to whether it's server-side
// filtered out (the is-filtered class). Uses cardRe from projects_web_test.go.
func hiddenByID(body string) map[string]bool {
	out := map[string]bool{}
	for _, m := range cardRe.FindAllStringSubmatch(body, -1) {
		out[m[2]] = strings.Contains(m[1], "is-filtered")
	}
	return out
}

func TestBoardFiltersByPriority(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	urgent := createItem(t, client, base, token, todo, "urgent item")
	createItem(t, client, base, token, todo, "calm item")

	r := postJSON(t, client, base+"/general/items/"+urgent+"/priority", token, map[string]any{"value": "urgent"})
	r.Body.Close()

	board := getBody(t, client, base+"/general?priority=urgent", http.StatusOK)
	if !strings.Contains(board, `data-facet="priority"`) {
		t.Error("board filter is missing the Priority facet")
	}
	hidden := hiddenByID(board)
	if hidden[urgent] {
		t.Error("the urgent item should be visible under the urgent filter")
	}
	for id, h := range hidden {
		if id != urgent && !h {
			t.Error("a non-urgent item should be hidden under the urgent filter")
		}
	}
}

func TestBoardFiltersByOverdue(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	late := createItem(t, client, base, token, todo, "late item")
	createItem(t, client, base, token, todo, "no-due item")

	r := postJSON(t, client, base+"/general/items/"+late+"/due", token, map[string]any{"due": "2020-01-01"})
	r.Body.Close()

	board := getBody(t, client, base+"/general?due=overdue", http.StatusOK)
	if !strings.Contains(board, `data-facet="due"`) {
		t.Error("board filter is missing the Due facet")
	}
	hidden := hiddenByID(board)
	if hidden[late] {
		t.Error("the overdue item should be visible under the overdue filter")
	}
	for id, h := range hidden {
		if id != late && !h {
			t.Error("an item with no due date should be hidden under the overdue filter")
		}
	}
}

// TestSaveViewWithAttributeFilter proves an attribute filter survives as a saved
// view (normalised) and renders as the active tab on its query.
func TestSaveViewWithAttributeFilter(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	bid := boardID(t, client, base)

	resp := postJSON(t, client, base+"/general/views", token, map[string]any{
		"name": "Urgent bugs", "query": "?priority=urgent&type=bug&item=x", "board_id": bid,
	})
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("create view: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// The tab carries the normalised query (junk dropped, keys sorted).
	body := getBody(t, client, base+"/general", http.StatusOK)
	if !strings.Contains(body, `data-view-query="priority=urgent&amp;type=bug"`) {
		t.Error("saved view did not store the normalised attribute query")
	}
	// Visiting that exact filter lights the tab up as active.
	active := getBody(t, client, base+"/general?priority=urgent&type=bug", http.StatusOK)
	if !strings.Contains(active, `class="view-tab active"`) {
		t.Error("the attribute view should be active on its query")
	}
}
