package web_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// boardIDRe is defined in boards_web_test.go (anchored on board-wrap).
var (
	viewSlugRe = regexp.MustCompile(`data-view-slug="([a-z0-9-]+)"`)
	// Anchored on the wrap (a following data-view-slug) so it doesn't also match
	// the Save button's data-view-id.
	viewIDRe = regexp.MustCompile(`data-view-id="([a-z0-9]+)" data-view-slug=`)
)

func boardID(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	body := getBody(t, client, base+"/general", http.StatusOK)
	m := boardIDRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no data-board-id on board page")
	}
	return m[1]
}

func allMatches(re *regexp.Regexp, body string) []string {
	ms := re.FindAllStringSubmatch(body, -1)
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m[1]
	}
	return out
}

// TestViewStripRendersFromRows proves the tab strip is seeded-row-driven: the
// non-release defaults render, the release-oriented ones hide without releases,
// and the active tab tracks the current filter.
func TestViewStripRendersFromRows(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	body := getBody(t, client, base+"/general", http.StatusOK)
	for _, slug := range []string{"all-items", "my-items", "milestones"} {
		if !strings.Contains(body, `data-view-slug="`+slug+`"`) {
			t.Errorf("default view %q missing from strip", slug)
		}
	}
	// No releases yet -> Current Release and Releases tabs are omitted entirely.
	for _, slug := range []string{"current-release", "releases"} {
		if strings.Contains(body, `data-view-slug="`+slug+`"`) {
			t.Errorf("release view %q should be hidden with no releases", slug)
		}
	}
	// All items is the active tab on the bare board (empty query).
	if !strings.Contains(body, `class="view-tab active" href="/general"`) {
		t.Error("All items should be active on the bare board")
	}
	// board.js reads data-view-query to refresh the prefs cache on click (the
	// All-items-clears-the-filter fix). All items carries the empty query.
	if !strings.Contains(body, `data-view-slug="all-items" data-view-query=""`) {
		t.Error("All items must expose an empty data-view-query")
	}
	if !strings.Contains(body, `data-view-slug="my-items" data-view-query="assignee=me"`) {
		t.Error("My items must expose its data-view-query")
	}
	// Filtering to me lights up My items.
	mine := getBody(t, client, base+"/general?assignee=me", http.StatusOK)
	if !strings.Contains(mine, `class="view-tab active" href="/general?assignee=me"`) {
		t.Error("My items should be active when filtered to me")
	}
}

// TestViewCRUD exercises the create/rename/delete HTTP surface end-to-end.
func TestViewCRUD(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	bid := boardID(t, client, base)

	// Create: the raw query is normalised server-side (junk dropped).
	resp := postJSON(t, client, base+"/general/views", token, map[string]any{
		"name": "My Bugs", "query": "?status=zzz&item=open", "board_id": bid,
	})
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("create view: status %d", resp.StatusCode)
	}
	id := decodeID(t, resp)

	if body := getBody(t, client, base+"/general", http.StatusOK); !strings.Contains(body, "My Bugs") || !strings.Contains(body, `data-view-id="`+id+`"`) {
		t.Fatal("created view not on the board")
	}

	// Rename.
	if r := postJSON(t, client, base+"/general/views/"+id+"/rename", token, map[string]any{"name": "Triage"}); r.StatusCode != http.StatusNoContent {
		r.Body.Close()
		t.Fatalf("rename: status %d", r.StatusCode)
	}
	if body := getBody(t, client, base+"/general", http.StatusOK); !strings.Contains(body, "Triage") || strings.Contains(body, "My Bugs") {
		t.Error("rename not reflected on the board")
	}

	// Delete.
	if r := postJSON(t, client, base+"/general/views/"+id+"/delete", token, nil); r.StatusCode != http.StatusNoContent {
		r.Body.Close()
		t.Fatalf("delete: status %d", r.StatusCode)
	}
	if body := getBody(t, client, base+"/general", http.StatusOK); strings.Contains(body, `data-view-id="`+id+`"`) {
		t.Error("deleted view still on the board")
	}
}

// TestViewReorder reverses the visible strip and confirms the new order renders.
func TestViewReorder(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	bid := boardID(t, client, base)

	body := getBody(t, client, base+"/general", http.StatusOK)
	ids := allMatches(viewIDRe, body)
	slugs := allMatches(viewSlugRe, body)
	if len(ids) < 2 {
		t.Fatalf("need ≥2 visible views, got %d", len(ids))
	}
	reversed := make([]string, len(ids))
	wantSlugs := make([]string, len(slugs))
	for i := range ids {
		reversed[len(ids)-1-i] = ids[i]
		wantSlugs[len(slugs)-1-i] = slugs[i]
	}
	if r := postJSON(t, client, base+"/general/views/reorder", token, map[string]any{"board_id": bid, "ids": reversed}); r.StatusCode != http.StatusNoContent {
		r.Body.Close()
		t.Fatalf("reorder: status %d", r.StatusCode)
	}
	got := allMatches(viewSlugRe, getBody(t, client, base+"/general", http.StatusOK))
	for i, s := range wantSlugs {
		if i >= len(got) || got[i] != s {
			t.Fatalf("after reorder slugs = %v, want %v", got, wantSlugs)
		}
	}
}

var (
	myItemsIDRe = regexp.MustCompile(`data-view-id="([a-z0-9]+)" data-view-slug="my-items"`)
	updateBtnRe = regexp.MustCompile(`<button class="view-update"[^>]*>`)
)

// TestViewDirtySave covers the server side of "save to an existing view": the
// Save button is rendered hidden on a view whose stored query matches the URL,
// shown when you're on it via ?view= provenance but the filter differs, and the
// save writes the current filter back so the view matches again. (The live dirty
// toggle as you filter without reloading is board.js's job.)
func TestViewDirtySave(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	// Clean My Items: the Save (update) button is present but hidden.
	clean := getBody(t, client, base+"/general?assignee=me", http.StatusOK)
	if b := updateBtnRe.FindString(clean); b == "" || !strings.Contains(b, "hidden") {
		t.Errorf("clean view: Save button should be hidden, got %q", b)
	}

	// Dirty: on My Items (via ?view=) but with a different filter — button shown,
	// and the anchor tab is active.
	dirty := getBody(t, client, base+"/general?status=zzz&view=my-items", http.StatusOK)
	if b := updateBtnRe.FindString(dirty); b == "" || strings.Contains(b, "hidden") {
		t.Errorf("dirty view: Save button should be visible, got %q", b)
	}
	if !strings.Contains(dirty, `class="view-tab active"`) {
		t.Error("dirty view: the anchor tab should be active")
	}

	// Save the new filter onto My Items.
	m := myItemsIDRe.FindStringSubmatch(dirty)
	if m == nil {
		t.Fatal("could not find the My items view id")
	}
	if r := postJSON(t, client, base+"/general/views/"+m[1]+"/save", token, map[string]any{"query": "?status=zzz"}); r.StatusCode != http.StatusNoContent {
		r.Body.Close()
		t.Fatalf("save: status %d", r.StatusCode)
	}

	// My Items now stores status=zzz: visiting that filter shows it active, and the
	// Save button hidden again (no longer dirty).
	saved := getBody(t, client, base+"/general?status=zzz", http.StatusOK)
	if !strings.Contains(saved, `class="view-tab active"`) {
		t.Error("after save, the view should be active on its new query")
	}
	if b := updateBtnRe.FindString(saved); b == "" || !strings.Contains(b, "hidden") {
		t.Errorf("after save: Save button should be hidden again, got %q", b)
	}
}

// TestViewScoping covers the validation edges: an unknown board on create and an
// unknown view id on rename/delete are both 404s.
func TestViewScoping(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	if r := postJSON(t, client, base+"/general/views", token, map[string]any{"name": "x", "board_id": "nope"}); r.StatusCode != http.StatusNotFound {
		r.Body.Close()
		t.Errorf("create with unknown board: status %d, want 404", r.StatusCode)
	}
	if r := postJSON(t, client, base+"/general/views/nope/rename", token, map[string]any{"name": "x"}); r.StatusCode != http.StatusNotFound {
		r.Body.Close()
		t.Errorf("rename unknown view: status %d, want 404", r.StatusCode)
	}
	if r := postJSON(t, client, base+"/general/views/nope/delete", token, nil); r.StatusCode != http.StatusNotFound {
		r.Body.Close()
		t.Errorf("delete unknown view: status %d, want 404", r.StatusCode)
	}
}
