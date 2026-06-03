package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// Milestone columns render in their saved order and can be reordered via the
// /milestones/reorder endpoint that board.js calls after a header drag.
func TestMilestoneColumnReorder(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")

	mk := func(title string) string {
		id := decodeID(t, postJSON(t, client, base+"/general/items", token, map[string]any{
			"status_id": todo, "title": title,
		}))
		resp := postJSON(t, client, base+"/general/items/"+id+"/milestone", token, map[string]any{
			"is_milestone": true,
		})
		resp.Body.Close()
		return id
	}
	a, b, c := mk("Alpha"), mk("Beta"), mk("Gamma")

	// Column headers carry data-open="<id>"; their order in the HTML is the
	// column order. Promotion appends, so it's creation order to start.
	order := func() []int {
		body := getBody(t, client, base+"/general?mode=milestone", http.StatusOK)
		return []int{
			strings.Index(body, `data-open="`+a+`"`),
			strings.Index(body, `data-open="`+b+`"`),
			strings.Index(body, `data-open="`+c+`"`),
		}
	}
	p := order()
	if !(p[0] < p[1] && p[1] < p[2]) {
		t.Fatalf("initial column order should be Alpha, Beta, Gamma; got positions %v", p)
	}

	// Reorder to Gamma, Alpha, Beta.
	resp := postJSON(t, client, base+"/general/milestones/reorder", token, map[string]any{
		"ids": []string{c, a, b},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("reorder: want 204, got %d", resp.StatusCode)
	}

	p = order() // [posAlpha, posBeta, posGamma]
	if !(p[2] < p[0] && p[0] < p[1]) {
		t.Fatalf("after reorder want Gamma, Alpha, Beta; got positions %v", p)
	}
}

// The milestone column header must carry a drag grip so board.js can wire the
// reorder Sortable; the Backlog column must not (it stays pinned first).
func TestMilestoneGripPresent(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	id := decodeID(t, postJSON(t, client, base+"/general/items", token, map[string]any{
		"status_id": todo, "title": "Anchor",
	}))
	postJSON(t, client, base+"/general/items/"+id+"/milestone", token, map[string]any{"is_milestone": true}).Body.Close()

	body := getBody(t, client, base+"/general?mode=milestone", http.StatusOK)
	if !strings.Contains(body, "mcol-grip") {
		t.Fatalf("milestone column missing a drag grip:\n%s", body)
	}
	// The Backlog title shouldn't be wrapped with a grip — its header is the
	// mcol-backlog span with no preceding grip in that column.
	if strings.Contains(body, `mcol-grip" title="Drag to reorder">⠿</span><span class="mcol-title mcol-backlog"`) {
		t.Fatal("Backlog column should not have a drag grip")
	}
}
