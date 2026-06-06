package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestSubgroupWithinStatus checks sub-grouping: with primary=Status (default) and
// sub-group=Priority, each lane splits into priority sub-sections (Urgent → None),
// and a card files under the sub-header matching its priority.
func TestSubgroupWithinStatus(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	hot := createItem(t, client, base, token, todo, "hot item")
	calm := createItem(t, client, base, token, todo, "calm item")
	postJSON(t, client, base+"/general/items/"+hot+"/priority", token, map[string]any{"value": "urgent"}).Body.Close()

	body := getBody(t, client, base+"/general?subgroup=priority", http.StatusOK)
	if !strings.Contains(body, `class="subgroup-head"`) {
		t.Fatal("sub-grouping should render sub-section headers")
	}
	if !strings.Contains(body, `data-sub-key="urgent"`) || !strings.Contains(body, ">Urgent</span>") {
		t.Error("an Urgent sub-section should render")
	}
	// The urgent card sits under the Urgent header, before the No-priority header.
	if !between(body, `data-sub-key="urgent"`, `data-item-id="`+hot+`"`, `data-sub-key="none"`) {
		t.Error("the urgent item should be under the Urgent sub-header")
	}
	// The unset card sits under the No-priority header (the last sub-section).
	if !between(body, `data-sub-key="none"`, `data-item-id="`+calm+`"`, "") {
		t.Error("the unset item should be under the No-priority sub-header")
	}
	// The Display menu offers the sub-group axes.
	if !strings.Contains(body, `data-subgroup="priority"`) || !strings.Contains(body, `data-subgroup="none"`) {
		t.Error("the Sub-group dropdown should list the axes and a None option")
	}
}

// TestSubgroupComposesWithGrouping checks sub-grouping nests inside a non-status
// primary grouping (group by Assignee, sub-group by Priority).
func TestSubgroupComposesWithGrouping(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	id := createItem(t, client, base, token, todo, "an item")
	postJSON(t, client, base+"/general/items/"+id+"/priority", token, map[string]any{"value": "high"}).Body.Close()

	body := getBody(t, client, base+"/general?mode=assignee&subgroup=priority", http.StatusOK)
	if !strings.Contains(body, `data-mode="assignee"`) {
		t.Fatal("primary grouping should still be assignee")
	}
	if !strings.Contains(body, `data-sub-key="high"`) || !strings.Contains(body, ">High</span>") {
		t.Error("the High sub-section should render inside an assignee column")
	}
}

// TestSubgroupIgnoredWhenMatchingPrimary confirms subgroup==primary is a no-op:
// the board renders the primary grouping without any sub-sections.
func TestSubgroupIgnoredWhenMatchingPrimary(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	body := getBody(t, client, base+"/general?mode=priority&subgroup=priority", http.StatusOK)
	if !strings.Contains(body, `data-mode="priority"`) {
		t.Fatal("primary grouping should be priority")
	}
	if strings.Contains(body, `class="subgroup-head"`) {
		t.Error("a sub-group matching the primary should be ignored (no sub-sections)")
	}
}
