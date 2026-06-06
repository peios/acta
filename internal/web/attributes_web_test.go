package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestItemAttributesHTTP drives the priority/type/size/due setters and checks the
// board card and the modal reflect them.
func TestItemAttributesHTTP(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	id := createItem(t, client, base, token, todo, "triage me")

	set := func(attr string, body map[string]any) {
		t.Helper()
		r := postJSON(t, client, base+"/general/items/"+id+"/"+attr, token, body)
		r.Body.Close()
		if r.StatusCode != http.StatusNoContent {
			t.Fatalf("set %s: status %d", attr, r.StatusCode)
		}
	}
	set("priority", map[string]any{"value": "urgent"})
	set("type", map[string]any{"value": "bug"})
	set("size", map[string]any{"value": "m"})
	set("due", map[string]any{"due": "2030-01-15"}) // future -> not overdue

	// The board card carries the slugs and an un-hidden glyph for each.
	body := getBody(t, client, base+"/general", http.StatusOK)
	for _, want := range []string{
		`data-priority="urgent"`, `data-type="bug"`, `data-size="m"`, `data-has-due="1"`,
		`class="attr prio p-urgent"`, `class="attr type t-bug"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board card missing %q", want)
		}
	}
	// Future due is not overdue.
	if strings.Contains(body, `data-overdue="1"`) {
		t.Error("a future due date should not be overdue")
	}

	// The modal pills and selects reflect the current values.
	modal := getBody(t, client, base+"/general/items/"+id+"/modal", http.StatusOK)
	for _, want := range []string{
		`data-side-priority-name>Urgent<`, `<option value="urgent" selected>Urgent</option>`,
		`data-side-type-name>Bug<`, `data-side-size-name>M<`,
		`class="modal-due side-date" value="2030-01-15"`,
	} {
		if !strings.Contains(modal, want) {
			t.Errorf("modal missing %q", want)
		}
	}
}

// TestItemAttributesOverdueAndClear checks overdue styling on a past date and that
// clearing hides the glyph again.
func TestItemAttributesOverdueAndClear(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	id := createItem(t, client, base, token, todo, "overdue me")

	r := postJSON(t, client, base+"/general/items/"+id+"/due", token, map[string]any{"due": "2020-01-01"})
	r.Body.Close()
	body := getBody(t, client, base+"/general", http.StatusOK)
	if !strings.Contains(body, `data-overdue="1"`) {
		t.Error("a past due date on a non-done item should be overdue")
	}
	if !strings.Contains(body, `class="attr due overdue"`) {
		t.Error("overdue due chip should carry the overdue class")
	}

	// Clear it: the due flag and chip go away.
	r = postJSON(t, client, base+"/general/items/"+id+"/due", token, map[string]any{"due": ""})
	r.Body.Close()
	body = getBody(t, client, base+"/general", http.StatusOK)
	if strings.Contains(body, `data-has-due="1"`) || strings.Contains(body, `data-overdue="1"`) {
		t.Error("cleared due date should drop the data flags")
	}

	// Clear priority back to none -> the glyph is hidden.
	r = postJSON(t, client, base+"/general/items/"+id+"/priority", token, map[string]any{"value": "none"})
	r.Body.Close()
	body = getBody(t, client, base+"/general", http.StatusOK)
	if !strings.Contains(body, `data-priority="none"`) {
		t.Error("cleared priority should read none")
	}
}

// TestItemAttributesValidation covers the rejection edges.
func TestItemAttributesValidation(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	id := createItem(t, client, base, token, todo, "validate me")

	// Unknown enum slug -> 400.
	if r := postJSON(t, client, base+"/general/items/"+id+"/priority", token, map[string]any{"value": "p0"}); r.StatusCode != http.StatusBadRequest {
		r.Body.Close()
		t.Errorf("bad priority slug: status %d, want 400", r.StatusCode)
	}
	// Malformed date -> 400.
	if r := postJSON(t, client, base+"/general/items/"+id+"/due", token, map[string]any{"due": "15/01/2030"}); r.StatusCode != http.StatusBadRequest {
		r.Body.Close()
		t.Errorf("bad date: status %d, want 400", r.StatusCode)
	}
	// Unknown item -> 404.
	if r := postJSON(t, client, base+"/general/items/nope/type", token, map[string]any{"value": "bug"}); r.StatusCode != http.StatusNotFound {
		r.Body.Close()
		t.Errorf("unknown item: status %d, want 404", r.StatusCode)
	}
}
