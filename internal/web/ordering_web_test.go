package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestBoardOrdering checks the Ordering control sorts cards within a lane: Title
// is A–Z, Priority is urgent-first, and the Display menu offers the options.
func TestBoardOrdering(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	// Created in this order; titles deliberately out of alphabetical sequence.
	apple := createItem(t, client, base, token, todo, "Apple")
	banana := createItem(t, client, base, token, todo, "Banana")
	cherry := createItem(t, client, base, token, todo, "Cherry")
	postJSON(t, client, base+"/general/items/"+cherry+"/priority", token, map[string]any{"value": "urgent"}).Body.Close()

	idx := func(body, id string) int { return strings.Index(body, `data-item-id="`+id+`"`) }

	// Title order: Apple < Banana < Cherry.
	titled := getBody(t, client, base+"/general?order=title", http.StatusOK)
	if !(idx(titled, apple) < idx(titled, banana) && idx(titled, banana) < idx(titled, cherry)) {
		t.Error("order=title should sort cards alphabetically by title")
	}
	// The Display menu offers the Ordering options.
	for _, want := range []string{`data-order="manual"`, `data-order="title"`, `data-order="priority"`, `data-order="due"`, `data-order="created"`} {
		if !strings.Contains(titled, want) {
			t.Errorf("Ordering dropdown missing %q", want)
		}
	}

	// Priority order: the urgent card leads the unset ones.
	prio := getBody(t, client, base+"/general?order=priority", http.StatusOK)
	if !(idx(prio, cherry) < idx(prio, apple) && idx(prio, cherry) < idx(prio, banana)) {
		t.Error("order=priority should put the urgent card first")
	}
}

// TestOrderingComposesWithSubgroup confirms ordering sorts cards within each
// sub-section when sub-grouping is also active.
func TestOrderingComposesWithSubgroup(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	zed := createItem(t, client, base, token, todo, "Zed")
	ann := createItem(t, client, base, token, todo, "Ann")
	// Same priority so they share a sub-section; ordering decides their order in it.
	for _, id := range []string{zed, ann} {
		postJSON(t, client, base+"/general/items/"+id+"/priority", token, map[string]any{"value": "high"}).Body.Close()
	}

	body := getBody(t, client, base+"/general?subgroup=priority&order=title", http.StatusOK)
	if !strings.Contains(body, `data-sub-key="high"`) {
		t.Fatal("the High sub-section should render")
	}
	ia := strings.Index(body, `data-item-id="`+ann+`"`)
	iz := strings.Index(body, `data-item-id="`+zed+`"`)
	if !(ia >= 0 && ia < iz) {
		t.Error("within the sub-section, title order should put Ann before Zed")
	}
}
