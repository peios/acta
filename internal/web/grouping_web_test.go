package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// between reports whether needle sits after start and before end in body — used to
// assert a card landed in a specific grouping column.
func between(body, start, needle, end string) bool {
	s := strings.Index(body, start)
	n := strings.Index(body, needle)
	e := -1
	if end != "" {
		e = strings.Index(body, end)
	}
	return s >= 0 && n > s && (e < 0 || n < e)
}

// TestBoardGroupByPriority checks priority grouping: the Display menu offers it,
// the board regroups into priority columns (Urgent first), and a card files into
// the column matching its priority (a clear one into the "No priority" column).
func TestBoardGroupByPriority(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	hot := createItem(t, client, base, token, todo, "hot item")
	calm := createItem(t, client, base, token, todo, "calm item")

	r := postJSON(t, client, base+"/general/items/"+hot+"/priority", token, map[string]any{"value": "urgent"})
	r.Body.Close()

	board := getBody(t, client, base+"/general?mode=priority", http.StatusOK)
	if !strings.Contains(board, `data-mode="priority"`) {
		t.Fatal("?mode=priority should regroup the board")
	}
	if !strings.Contains(board, `data-mode="priority">Priority</a>`) {
		t.Error("the Display menu should offer Priority grouping")
	}
	// Urgent is the first column; the hot card sits between it and the High column.
	if !between(board, `data-group-col="urgent"`, `data-item-id="`+hot+`"`, `data-group-col="high"`) {
		t.Error("the urgent item should be in the Urgent column")
	}
	// The calm card has no priority, so it files under "No priority" (the last col).
	if !between(board, `data-group-col="none"`, `data-item-id="`+calm+`"`, "") {
		t.Error("the unset item should be in the No-priority column")
	}
}

// TestBoardGroupByAssigneeProjectDue smoke-tests the other new axes render their
// columns, and that due buckets are marked read-only (data-no-drop).
func TestBoardGroupByAssigneeProjectDue(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	late := createItem(t, client, base, token, todo, "late item")
	postJSON(t, client, base+"/general/items/"+late+"/due", token, map[string]any{"due": "2020-01-01"}).Body.Close()

	assignee := getBody(t, client, base+"/general?mode=assignee", http.StatusOK)
	if !strings.Contains(assignee, `data-group-col=""`) || !strings.Contains(assignee, ">Unassigned</span>") {
		t.Error("assignee grouping should lead with an Unassigned column")
	}

	project := getBody(t, client, base+"/general?mode=project", http.StatusOK)
	if !strings.Contains(project, ">No project</span>") {
		t.Error("project grouping should lead with a No-project column")
	}

	due := getBody(t, client, base+"/general?mode=due", http.StatusOK)
	if !strings.Contains(due, `data-group-col="overdue"`) || !strings.Contains(due, ">No due date</span>") {
		t.Error("due grouping should render the date buckets")
	}
	if !strings.Contains(due, `data-no-drop`) {
		t.Error("due buckets should be marked read-only (data-no-drop)")
	}
	// The overdue item files into the Overdue bucket (before the Today bucket).
	if !between(due, `data-group-col="overdue"`, `data-item-id="`+late+`"`, `data-group-col="today"`) {
		t.Error("the past-due item should be in the Overdue bucket")
	}
}

// TestBoardGroupModePersistsInView confirms grouping is captured by a saved view
// (so a "By priority" tab works) and lights up as active on its query.
func TestBoardGroupModePersistsInView(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	bid := boardID(t, client, base)

	resp := postJSON(t, client, base+"/general/views", token, map[string]any{
		"name": "By priority", "query": "?mode=priority", "board_id": bid,
	})
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("create view: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	body := getBody(t, client, base+"/general?mode=priority", http.StatusOK)
	if !strings.Contains(body, `data-view-query="mode=priority"`) {
		t.Error("a grouping view should store its mode")
	}
	if !strings.Contains(body, `class="view-tab active"`) {
		t.Error("the grouping view should be active on its query")
	}
}
