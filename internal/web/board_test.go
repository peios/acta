package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
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
var laneRe = regexp.MustCompile(`data-status-id="([0-9a-f]+)"[\s\S]*?value="([^"]+)"`)

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
