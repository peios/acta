package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/peios/acta/internal/board"
)

// postJSON sends a JSON body with the CSRF token in the header, the way board.js
// does. token must match the csrf cookie already in the client's jar.
func postJSON(t *testing.T, client *http.Client, url, token string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", token)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// laneRe pairs a lane's status id with the name in its rename input.
var laneRe = regexp.MustCompile(`data-status-id="([a-z0-9]+)"[\s\S]*?value="([^"]+)"`)

func statusID(t *testing.T, client *http.Client, base, name string) string {
	t.Helper()
	body := getBody(t, client, base+"/w/general", http.StatusOK)
	for _, m := range laneRe.FindAllStringSubmatch(body, -1) {
		if m[2] == name {
			return m[1]
		}
	}
	t.Fatalf("status %q not found on board", name)
	return ""
}

func decodeID(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	var v struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode id: %v", err)
	}
	return v.ID
}

func TestBoardRendersSeededLanes(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	body := getBody(t, client, base+"/w/general", http.StatusOK)
	if !strings.Contains(body, `id="board"`) {
		t.Error("board page missing the board element")
	}
	for _, lane := range []string{"To do", "Doing", "Done"} {
		if !strings.Contains(body, `value="`+lane+`"`) {
			t.Errorf("board missing seeded lane %q", lane)
		}
	}
}

func TestCreateItemAppearsOnBoard(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	todo := statusID(t, client, base, "To do")
	resp := postJSON(t, client, base+"/w/general/items", token, map[string]any{
		"status_id": todo, "title": "Write the spec",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create item: want 200, got %d", resp.StatusCode)
	}
	if id := decodeID(t, resp); id == "" {
		t.Fatal("create item returned no id")
	}
	board := getBody(t, client, base+"/w/general", http.StatusOK)
	if !strings.Contains(board, "Write the spec") {
		t.Error("new item not rendered on the board")
	}
}

func TestMoveItemAcrossLanes(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	todo := statusID(t, client, base, "To do")
	doing := statusID(t, client, base, "Doing")
	id := decodeID(t, postJSON(t, client, base+"/w/general/items", token, map[string]any{
		"status_id": todo, "title": "Movable",
	}))

	resp := postJSON(t, client, base+"/w/general/items/"+id+"/move", token, map[string]any{
		"status_id": doing, "index": 0,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("move: want 204, got %d", resp.StatusCode)
	}

	// The item should now sit under the Doing lane. Assert by checking the
	// item appears after the Doing lane header and before the next lane.
	board := getBody(t, client, base+"/w/general", http.StatusOK)
	doingIdx := strings.Index(board, `value="Doing"`)
	doneIdx := strings.Index(board, `value="Done"`)
	movIdx := strings.Index(board, "Movable")
	if movIdx < doingIdx || movIdx > doneIdx {
		t.Fatalf("moved item not in the Doing lane (doing=%d item=%d done=%d)", doingIdx, movIdx, doneIdx)
	}
}

func TestDeleteStatusBlockedWhenNonEmpty(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	todo := statusID(t, client, base, "To do")
	postJSON(t, client, base+"/w/general/items", token, map[string]any{
		"status_id": todo, "title": "Blocker",
	}).Body.Close()

	resp := postJSON(t, client, base+"/w/general/statuses/"+todo+"/delete", token, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("delete non-empty lane: want 409, got %d", resp.StatusCode)
	}
	var v struct{ Error string }
	json.NewDecoder(resp.Body).Decode(&v)
	if v.Error != "status_not_empty" {
		t.Fatalf("want error status_not_empty, got %q", v.Error)
	}
}

func TestCreateAndDeleteStatus(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	resp := postJSON(t, client, base+"/w/general/statuses", token, map[string]any{"name": "Blocked"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status: want 200, got %d", resp.StatusCode)
	}
	id := decodeID(t, resp)
	if !strings.Contains(getBody(t, client, base+"/w/general", http.StatusOK), `value="Blocked"`) {
		t.Fatal("new status not on the board")
	}

	// Empty lane deletes cleanly.
	del := postJSON(t, client, base+"/w/general/statuses/"+id+"/delete", token, nil)
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete empty lane: want 204, got %d", del.StatusCode)
	}
	if strings.Contains(getBody(t, client, base+"/w/general", http.StatusOK), `value="Blocked"`) {
		t.Fatal("deleted status still on the board")
	}
}

func TestBoardRejectsMissingCSRF(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")

	// No X-CSRF-Token header -> rejected by the middleware.
	b, _ := json.Marshal(map[string]any{"status_id": todo, "title": "x"})
	req, _ := http.NewRequest(http.MethodPost, base+"/w/general/items", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing csrf: want 403, got %d", resp.StatusCode)
	}
}

func TestStatusColorSetValidatedAndRendered(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	todo := statusID(t, client, base, "To do")

	// A palette colour is accepted.
	want := board.Palette[4]
	resp := postJSON(t, client, base+"/w/general/statuses/"+todo+"/color", token, map[string]any{"color": want})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set colour: want 204, got %d", resp.StatusCode)
	}

	// An off-palette colour is rejected so only known-safe values reach the UI.
	bad := postJSON(t, client, base+"/w/general/statuses/"+todo+"/color", token, map[string]any{"color": "#ff0000"})
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("off-palette colour: want 400, got %d", bad.StatusCode)
	}
	var e struct{ Error string }
	json.NewDecoder(bad.Body).Decode(&e)
	if e.Error != "invalid_color" {
		t.Fatalf("want invalid_color, got %q", e.Error)
	}

	// The board renders the chosen colour as a --lane-color custom property and
	// offers the palette swatches; the old text status badge is gone.
	body := getBody(t, client, base+"/w/general", http.StatusOK)
	if !strings.Contains(body, "--lane-color:"+want) {
		t.Errorf("board missing --lane-color:%s", want)
	}
	if !strings.Contains(body, `class="lane-palette"`) || !strings.Contains(body, `data-color="`+board.Palette[0]+`"`) {
		t.Error("board missing the colour-picker swatches")
	}
	if strings.Contains(body, `class="item-status"`) {
		t.Error("the old text status badge should be gone from cards")
	}
}

func TestCreateItemReturnsColor(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	todo := statusID(t, client, base, "To do") // first lane, no explicit colour -> palette[0]
	resp := postJSON(t, client, base+"/w/general/items", token, map[string]any{
		"status_id": todo, "title": "Paint my bar",
	})
	defer resp.Body.Close()
	var v struct{ Color string }
	json.NewDecoder(resp.Body).Decode(&v)
	if v.Color != board.Palette[0] {
		t.Fatalf("create item colour: want %q, got %q (board.js needs it to paint the new card)", board.Palette[0], v.Color)
	}
}

func TestCreateStatusReturnsColor(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	resp := postJSON(t, client, base+"/w/general/statuses", token, map[string]any{"name": "Review"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status: want 200, got %d", resp.StatusCode)
	}
	var v struct{ Color string }
	json.NewDecoder(resp.Body).Decode(&v)
	if v.Color == "" {
		t.Fatal("create status returned no colour (board.js needs it to paint the new lane)")
	}
}

func TestBoardStatusFilterHidesLanesAndCards(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	todo := statusID(t, client, base, "To do")
	doing := statusID(t, client, base, "Doing")
	id := decodeID(t, postJSON(t, client, base+"/w/general/items", token, map[string]any{
		"status_id": todo, "title": "Hide me",
	}))

	// Filter to Doing only: the To do lane (and its card) are filtered out.
	body := getBody(t, client, base+"/w/general?status="+doing, http.StatusOK)
	if !strings.Contains(body, `class="lane is-filtered" data-status-id="`+todo+`"`) {
		t.Error("To do lane should be filtered out")
	}
	if !strings.Contains(body, `class="lane" data-status-id="`+doing+`"`) {
		t.Error("Doing lane should remain visible")
	}
	if !strings.Contains(body, `class="item is-filtered" data-item-id="`+id+`"`) {
		t.Error("the To do card should be filtered out")
	}
	// The Status trigger shows a selected-count badge.
	if !strings.Contains(body, `facet-count`) {
		t.Error("expected a filter count badge")
	}
}

func TestBoardAssigneeFilterByUnassignedAndMe(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	todo := statusID(t, client, base, "To do")
	id := decodeID(t, postJSON(t, client, base+"/w/general/items", token, map[string]any{
		"status_id": todo, "title": "Solo", // freshly created => unassigned
	}))

	// Unassigned selected: the card shows.
	shown := getBody(t, client, base+"/w/general?assignee=unassigned", http.StatusOK)
	if !strings.Contains(shown, `class="item" data-item-id="`+id+`"`) {
		t.Error("unassigned card should be visible under assignee=unassigned")
	}
	// Me selected: an unassigned card is hidden.
	hidden := getBody(t, client, base+"/w/general?assignee=me", http.StatusOK)
	if !strings.Contains(hidden, `class="item is-filtered" data-item-id="`+id+`"`) {
		t.Error("unassigned card should be hidden under assignee=me")
	}
}

func TestBoardNoFilterShowsAll(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	body := getBody(t, client, base+"/w/general", http.StatusOK)
	// "is-filtered" also appears in the stylesheet, so assert on the element
	// classes specifically — nothing should carry the filtered modifier.
	if strings.Contains(body, `class="lane is-filtered`) || strings.Contains(body, `class="item is-filtered`) {
		t.Error("an unfiltered board should mark no lane or card as filtered")
	}
	// The facet scaffolding is always present (Me / Unassigned tokens).
	if !strings.Contains(body, `value="me"`) || !strings.Contains(body, `value="unassigned"`) {
		t.Error("assignee facet should always offer Me and Unassigned")
	}
}

func TestStaticAssetsAreVersioned(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	// The board page must reference board.js with a cache-busting ?v=, so a CDN
	// can't keep serving a stale copy across deploys.
	body := getBody(t, client, base+"/w/general", http.StatusOK)
	if !strings.Contains(body, "/static/board.js?v=") {
		t.Error("board.js should be referenced with a ?v= cache-buster")
	}
}

func TestStaticHandlerSetsImmutableCache(t *testing.T) {
	base, client := newTestServer(t)
	resp, err := client.Get(base + "/static/board.js?v=anything")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("static assets should be cached immutable, got Cache-Control %q", cc)
	}
}
