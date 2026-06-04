package web_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// Scrape the board-wrap's id specifically — the sidebar board links also carry
// data-board-id, so an unanchored match would grab the wrong one.
var boardIDRe = regexp.MustCompile(`board-wrap[^>]*data-board-id="([a-z0-9]+)"`)

// TestSidebarListsBoards checks the two boards surface as sidebar nav links,
// the default board at the bare workspace URL and Backlog under its slug.
func TestSidebarListsBoards(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	body := getBody(t, client, base+"/general", http.StatusOK)
	if !strings.Contains(body, `href="/general"`) || !strings.Contains(body, `>Tasks<`) {
		t.Error("sidebar missing Tasks board link")
	}
	if !strings.Contains(body, `href="/general/backlog"`) || !strings.Contains(body, `>Backlog<`) {
		t.Error("sidebar missing Backlog board link")
	}
}

// TestBacklogBoardRenders confirms /{slug}/backlog renders the Backlog board —
// its single seeded "Backlog" lane — and that its header Activity/Archived links
// are board-scoped.
func TestBacklogBoardRenders(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	body := getBody(t, client, base+"/general/backlog", http.StatusOK)
	if !strings.Contains(body, `value="Backlog"`) {
		t.Error("Backlog board missing its seeded Backlog lane")
	}
	// The Tasks lanes must NOT appear on the Backlog board.
	if strings.Contains(body, `value="Doing"`) {
		t.Error("Backlog board leaked a Tasks lane")
	}
	if !strings.Contains(body, `href="/general/activity?board=backlog"`) {
		t.Error("Backlog header Activity link not board-scoped")
	}
	if !strings.Contains(body, `href="/general/archive?board=backlog"`) {
		t.Error("Backlog header Archived link not board-scoped")
	}

	// An unknown board slug is a 404.
	getBody(t, client, base+"/general/nope", http.StatusNotFound)
}

// TestAddLaneTargetsViewedBoard confirms a lane added while viewing Backlog
// joins Backlog (board derived from the request's board_id), not Tasks.
func TestAddLaneTargetsViewedBoard(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	page := getBody(t, client, base+"/general/backlog", http.StatusOK)
	m := boardIDRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no data-board-id on the Backlog board")
	}
	backlogID := m[1]

	resp := postJSON(t, client, base+"/general/statuses", token, map[string]any{
		"name": "Triage", "board_id": backlogID,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create lane: want 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// The new lane shows on Backlog, not on the Tasks board.
	if back := getBody(t, client, base+"/general/backlog", http.StatusOK); !strings.Contains(back, `value="Triage"`) {
		t.Error("new lane did not land on the Backlog board")
	}
	if tasks := getBody(t, client, base+"/general", http.StatusOK); strings.Contains(tasks, `value="Triage"`) {
		t.Error("new Backlog lane leaked onto the Tasks board")
	}
}

// TestModalStatusPickerSpansBoards confirms the modal status picker offers every
// board's lanes, grouped — picking a Backlog status is how an item moves boards.
func TestModalStatusPickerSpansBoards(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	id := makeItem(t, client, base, token, statusID(t, client, base, "To do"), "Spanning")
	modal := getBody(t, client, base+"/general/items/"+id+"/modal", http.StatusOK)
	if !strings.Contains(modal, `class="status-group">Tasks`) || !strings.Contains(modal, `class="status-group">Backlog`) {
		t.Error("modal status picker not grouped by both boards")
	}
	if !strings.Contains(modal, `<optgroup label="Backlog">`) {
		t.Error("modal status select missing the Backlog optgroup")
	}
}

// TestDropOntoBoardMovesItem exercises what a sidebar drag posts: moving an item
// onto a board lands it in that board's entry lane and off its old board.
func TestDropOntoBoardMovesItem(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	id := makeItem(t, client, base, token, statusID(t, client, base, "To do"), "Promote me")

	backlogID := boardIDRe.FindStringSubmatch(getBody(t, client, base+"/general/backlog", http.StatusOK))[1]
	resp := postJSON(t, client, base+"/general/items/"+id+"/board", token, map[string]any{"board_id": backlogID})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set board: want 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	if !strings.Contains(getBody(t, client, base+"/general/backlog", http.StatusOK), "Promote me") {
		t.Error("item did not land on the Backlog board after the drop")
	}
	if strings.Contains(getBody(t, client, base+"/general", http.StatusOK), "Promote me") {
		t.Error("item still on the Tasks board after the drop")
	}

	// A board from another workspace is rejected (no cross-workspace reach).
	if bad := postJSON(t, client, base+"/general/items/"+id+"/board", token, map[string]any{"board_id": "deadbeef"}); bad.StatusCode != http.StatusNotFound {
		t.Errorf("unknown board: want 404, got %d", bad.StatusCode)
		bad.Body.Close()
	}
}

// TestBoardScopedActivity confirms the board-scoped activity feed renders.
func TestBoardScopedActivity(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	if body := getBody(t, client, base+"/general/activity?board=backlog", http.StatusOK); !strings.Contains(body, "Backlog activity") {
		t.Error("board-scoped activity page missing its title")
	}
	// The bare feed stays workspace-wide.
	if body := getBody(t, client, base+"/general/activity", http.StatusOK); !strings.Contains(body, ">Activity<") {
		t.Error("workspace activity page missing its title")
	}
}
