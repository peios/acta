package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestMCPSearchItems drives the q parameter on list_items end-to-end: title and
// description matching through the handler, and the exact-ref float that surfaces
// an item by id even when its text doesn't contain the ref.
func TestMCPSearchItems(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)
	sess := mcpConnect(t, base, token)

	a := callTool[mcpItemT](t, sess, "create_item", map[string]any{"workspace": "general", "title": "loregd device-wiring"})
	b := callTool[mcpItemT](t, sess, "create_item", map[string]any{"workspace": "general", "title": "unrelated card"})
	callTool[mcpItemT](t, sess, "set_item_description", map[string]any{"id": b.ID, "description": "notes on the registry daemon"})
	callTool[mcpItemT](t, sess, "create_item", map[string]any{"workspace": "general", "title": "nothing to see here"})

	// q matches the title.
	if got := callTool[mcpItemsT](t, sess, "list_items", map[string]any{"workspace": "general", "q": "loregd"}); len(got.Items) != 1 || got.Items[0].ID != a.ID {
		t.Fatalf("q=loregd → %+v", got.Items)
	}
	// q matches the description (proves the path runs through the handler).
	if got := callTool[mcpItemsT](t, sess, "list_items", map[string]any{"workspace": "general", "q": "registry"}); len(got.Items) != 1 || got.Items[0].ID != b.ID {
		t.Fatalf("q=registry → %+v", got.Items)
	}

	// A Backlog item is invisible to an unscoped search (the default keeps the
	// agent out of the planning dumping-ground), reachable via board=*, and
	// directly via board=backlog.
	bl := callTool[mcpItemT](t, sess, "create_item", map[string]any{"workspace": "general", "board": "backlog", "title": "loregd in backlog"})
	if got := callTool[mcpItemsT](t, sess, "list_items", map[string]any{"workspace": "general", "q": "loregd"}); len(got.Items) != 1 || got.Items[0].ID != a.ID {
		t.Fatalf("unscoped q=loregd must skip Backlog → %+v", got.Items)
	}
	if got := callTool[mcpItemsT](t, sess, "list_items", map[string]any{"workspace": "general", "q": "loregd", "board": "*"}); len(got.Items) != 2 {
		t.Fatalf("q=loregd board=* must include Backlog → %+v", got.Items)
	}
	if got := callTool[mcpItemsT](t, sess, "list_items", map[string]any{"workspace": "general", "q": "loregd", "board": "backlog"}); len(got.Items) != 1 || got.Items[0].ID != bl.ID {
		t.Fatalf("q=loregd board=backlog → %+v", got.Items)
	}
	// An exact human ref floats to the top, even though the title has no such text.
	if a.Ref == "" {
		t.Fatal("created item has no ref")
	}
	if got := callTool[mcpItemsT](t, sess, "list_items", map[string]any{"workspace": "general", "q": a.Ref}); len(got.Items) == 0 || got.Items[0].ID != a.ID {
		t.Fatalf("q=%q must float the ref hit first → %+v", a.Ref, got.Items)
	}
}

// TestAPISearchItems covers the REST ?q= wiring: it narrows the listing to text
// matches and excludes non-matches; a miss returns nothing.
func TestAPISearchItems(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)

	a := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items", token,
		map[string]string{"title": "loregd wiring"}), http.StatusCreated)
	decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items", token,
		map[string]string{"title": "unrelated thing"}), http.StatusCreated)

	// ?q= narrows to matches (case-insensitive) and drops the rest.
	fb := readBody(t, bearerJSON(t, base, "GET", "/api/v1/w/general/items?q=LOREGD", token, nil))
	if !strings.Contains(fb, a.ID) || strings.Contains(fb, "unrelated thing") {
		t.Fatalf("?q=LOREGD wrong:\n%s", fb)
	}
	// A miss returns no items.
	if mb := readBody(t, bearerJSON(t, base, "GET", "/api/v1/w/general/items?q=zzz-nope", token, nil)); strings.Contains(mb, a.ID) {
		t.Fatalf("?q miss should be empty:\n%s", mb)
	}
	// board=* is accepted as the all-boards scope.
	if sb := readBody(t, bearerJSON(t, base, "GET", "/api/v1/w/general/items?q=loregd&board=*", token, nil)); !strings.Contains(sb, a.ID) {
		t.Fatalf("?q=loregd&board=* should still find it:\n%s", sb)
	}
	// An unknown board scope is a 400.
	if bad := bearerJSON(t, base, "GET", "/api/v1/w/general/items?q=loregd&board=zzz", token, nil); bad.StatusCode != http.StatusBadRequest {
		bad.Body.Close()
		t.Fatalf("unknown board scope: want 400, got %d", bad.StatusCode)
	}
}

// TestSearchSwitcher covers the Cmd-K results fragment: session-authed, returns
// matching hits with a jump URL, excludes non-matches, and is empty for a blank
// query.
func TestSearchSwitcher(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)

	a := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items", token,
		map[string]string{"title": "loregd switcher hit"}), http.StatusCreated)
	decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items", token,
		map[string]string{"title": "an unrelated card"}), http.StatusCreated)

	resp, err := client.Get(base + "/general/search?q=loregd")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "?item="+a.ID) {
		t.Fatalf("switcher fragment missing the hit's jump URL:\n%s", body)
	}
	if strings.Contains(body, "an unrelated card") {
		t.Fatalf("switcher fragment included a non-match:\n%s", body)
	}

	// A blank query yields no hits.
	eresp, err := client.Get(base + "/general/search")
	if err != nil {
		t.Fatal(err)
	}
	if empty := readBody(t, eresp); strings.Contains(empty, "cmdk-hit") {
		t.Fatalf("blank query should yield no hits:\n%s", empty)
	}
}
