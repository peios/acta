package web_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// makeItem creates an item in the named lane and returns its id.
func makeItem(t *testing.T, client *http.Client, base, token, statusID, title string) string {
	t.Helper()
	return decodeID(t, postJSON(t, client, base+"/general/items", token, map[string]any{
		"status_id": statusID, "title": title,
	}))
}

func TestDeepLinkRendersModal(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	id := makeItem(t, client, base, token, statusID(t, client, base, "To do"), "Deep linked")

	// ?item= server-renders the modal into the board page.
	page := getBody(t, client, base+"/general?item="+id, http.StatusOK)
	if !strings.Contains(page, "data-modal") || !strings.Contains(page, `value="Deep linked"`) {
		t.Fatalf("deep-link did not render the modal:\n%s", page)
	}
	if !strings.Contains(page, "data-feed") || !strings.Contains(page, "data-comment-input") {
		t.Error("modal missing activity feed / comment composer")
	}

	// The fragment endpoint returns just the modal.
	frag := getBody(t, client, base+"/general/items/"+id+"/modal", http.StatusOK)
	if !strings.Contains(frag, "data-modal") || !strings.Contains(frag, `value="Deep linked"`) {
		t.Fatalf("modal fragment wrong:\n%s", frag)
	}
}

func TestUnknownItemModalIs404(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	resp, err := client.Get(base + "/general/items/deadbeef/modal")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown item modal: want 404, got %d", resp.StatusCode)
	}

	// A bogus ?item= just renders the board with no modal.
	page := getBody(t, client, base+"/general?item=deadbeef", http.StatusOK)
	if strings.Contains(page, "data-modal") {
		t.Error("bogus ?item should not render a modal")
	}
}

func TestDescriptionAndComment(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	id := makeItem(t, client, base, token, statusID(t, client, base, "To do"), "Task")

	desc := postJSON(t, client, base+"/general/items/"+id+"/description", token, map[string]any{
		"description": "the full story",
	})
	descBody := readBody(t, desc)
	if desc.StatusCode != http.StatusOK {
		t.Fatalf("description: want 200, got %d", desc.StatusCode)
	}
	// The endpoint returns the rendered, collapsible view fragment.
	if !strings.Contains(descBody, "the full story") {
		t.Errorf("description response missing rendered text:\n%s", descBody)
	}

	cm := postJSON(t, client, base+"/general/items/"+id+"/comment", token, map[string]any{
		"body": "first thoughts",
	})
	defer cm.Body.Close()
	if cm.StatusCode != http.StatusOK {
		t.Fatalf("comment: want 200, got %d", cm.StatusCode)
	}
	var c struct{ Author, Body, At string }
	json.NewDecoder(cm.Body).Decode(&c)
	if c.Author != "Jack" || c.Body != "first thoughts" {
		t.Fatalf("comment echo wrong: %+v", c)
	}

	modal := getBody(t, client, base+"/general/items/"+id+"/modal", http.StatusOK)
	if !strings.Contains(modal, "the full story") {
		t.Error("modal missing saved description")
	}
	if !strings.Contains(modal, "first thoughts") || !strings.Contains(modal, "Jack") {
		t.Error("modal missing the comment")
	}
}

func TestCommentEditDeleteHTTP(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	id := makeItem(t, client, base, token, statusID(t, client, base, "To do"), "Task")

	// Post a comment and capture its id.
	cm := postJSON(t, client, base+"/general/items/"+id+"/comment", token, map[string]any{"body": "draft"})
	var c struct {
		ID string `json:"id"`
	}
	json.NewDecoder(cm.Body).Decode(&c)
	cm.Body.Close()
	if c.ID == "" {
		t.Fatal("comment response missing id")
	}

	// Edit it.
	ed := postJSON(t, client, base+"/general/items/"+id+"/comment/"+c.ID+"/edit", token, map[string]any{"body": "final **edit**"})
	ed.Body.Close()
	if ed.StatusCode != http.StatusOK {
		t.Fatalf("edit: want 200, got %d", ed.StatusCode)
	}
	// Modal shows the edited body rendered as markdown, plus an (edited) tag.
	modal := getBody(t, client, base+"/general/items/"+id+"/modal", http.StatusOK)
	if !strings.Contains(modal, "<strong>edit</strong>") || !strings.Contains(modal, "(edited)") {
		t.Errorf("modal missing edited comment / tag:\n%s", modal)
	}

	// A non-existent comment id is rejected, not silently accepted.
	miss := postJSON(t, client, base+"/general/items/"+id+"/comment/deadbeef/edit", token, map[string]any{"body": "x"})
	miss.Body.Close()
	if miss.StatusCode != http.StatusNotFound {
		t.Fatalf("edit unknown comment: want 404, got %d", miss.StatusCode)
	}

	// Delete it: 204, and the modal renders a tombstone, not the body.
	del := postJSON(t, client, base+"/general/items/"+id+"/comment/"+c.ID+"/delete", token, nil)
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d", del.StatusCode)
	}
	modal = getBody(t, client, base+"/general/items/"+id+"/modal", http.StatusOK)
	if !strings.Contains(modal, "Comment deleted") {
		t.Error("modal missing tombstone after delete")
	}
	if strings.Contains(modal, "final") {
		t.Error("deleted comment body still present in modal")
	}
}

var assigneeOptRe = regexp.MustCompile(`<option value="([a-z0-9]+)"[^>]*>Jack</option>`)

func TestAssignFromModal(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	id := makeItem(t, client, base, token, statusID(t, client, base, "To do"), "Assign me")

	modal := getBody(t, client, base+"/general/items/"+id+"/modal", http.StatusOK)
	m := assigneeOptRe.FindStringSubmatch(modal)
	if m == nil {
		t.Fatalf("no Jack option in assignee picker:\n%s", modal)
	}
	jackID := m[1]

	resp := postJSON(t, client, base+"/general/items/"+id+"/assignee", token, map[string]any{
		"assignee_id": jackID,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("assign: want 204, got %d", resp.StatusCode)
	}
	after := getBody(t, client, base+"/general/items/"+id+"/modal", http.StatusOK)
	if !regexp.MustCompile(`value="` + jackID + `"[^>]*selected`).MatchString(after) {
		t.Errorf("assignee not selected after assigning:\n%s", after)
	}
}

func TestArchiveRestoreFlow(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	id := makeItem(t, client, base, token, statusID(t, client, base, "To do"), "Archive me")

	// Archive (JSON, from the board) -> off the board, on the archive page.
	arc := postJSON(t, client, base+"/general/items/"+id+"/archive", token, nil)
	arc.Body.Close()
	if arc.StatusCode != http.StatusNoContent {
		t.Fatalf("archive: want 204, got %d", arc.StatusCode)
	}
	if strings.Contains(getBody(t, client, base+"/general", http.StatusOK), "Archive me") {
		t.Error("archived item still on the board")
	}
	if !strings.Contains(getBody(t, client, base+"/general/archive", http.StatusOK), "Archive me") {
		t.Error("archived item missing from the archive view")
	}

	// Restore (form, from the archive view) redirects and the item returns.
	un := postForm(t, client, base+"/general/items/"+id+"/unarchive", url.Values{"csrf_token": {token}})
	un.Body.Close()
	if un.StatusCode != http.StatusSeeOther {
		t.Fatalf("unarchive form: want 303, got %d", un.StatusCode)
	}
	if !strings.Contains(getBody(t, client, base+"/general", http.StatusOK), "Archive me") {
		t.Error("restored item not back on the board")
	}
}

func subtaskOf(t *testing.T, client *http.Client, base, token, parentID, title string) string {
	t.Helper()
	return decodeID(t, postJSON(t, client, base+"/general/items/"+parentID+"/subtasks", token, map[string]any{"title": title}))
}

func TestSubtaskBadgeAndModal(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	parent := makeItem(t, client, base, token, statusID(t, client, base, "To do"), "Parent task")
	sub := subtaskOf(t, client, base, token, parent, "Subtask one")

	board := getBody(t, client, base+"/general", http.StatusOK)
	if !strings.Contains(board, "0/1") {
		t.Errorf("parent card missing 0/1 subtask badge:\n%s", board)
	}
	if strings.Contains(board, "Subtask one") {
		t.Error("subtask should not appear as a board card")
	}

	pm := getBody(t, client, base+"/general/items/"+parent+"/modal", http.StatusOK)
	if !strings.Contains(pm, "Subtask one") || !strings.Contains(pm, "Subtasks") {
		t.Error("parent modal missing the subtasks section")
	}

	cm := getBody(t, client, base+"/general/items/"+sub+"/modal", http.StatusOK)
	if !strings.Contains(cm, `data-parent-link="`+parent+`"`) || !strings.Contains(cm, "Parent task") {
		t.Errorf("child modal missing the parent link:\n%s", cm)
	}
}

func TestSubtaskDoneBadge(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	parent := makeItem(t, client, base, token, statusID(t, client, base, "To do"), "P")
	sub := subtaskOf(t, client, base, token, parent, "s")

	done := statusID(t, client, base, "Done")
	resp := postJSON(t, client, base+"/general/items/"+sub+"/status", token, map[string]any{"status_id": done})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set subtask status: want 204, got %d", resp.StatusCode)
	}
	if !strings.Contains(getBody(t, client, base+"/general", http.StatusOK), "1/1") {
		t.Error("badge should read 1/1 after the subtask reaches the last status")
	}
}

func TestArchiveParentCascadesWeb(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	parent := makeItem(t, client, base, token, statusID(t, client, base, "To do"), "Parent")
	subtaskOf(t, client, base, token, parent, "the child")

	postJSON(t, client, base+"/general/items/"+parent+"/archive", token, nil).Body.Close()

	if strings.Contains(getBody(t, client, base+"/general", http.StatusOK), "Parent") {
		t.Error("archived parent still on the board")
	}
	arch := getBody(t, client, base+"/general/archive", http.StatusOK)
	if !strings.Contains(arch, "Parent") {
		t.Error("archived parent missing from the archive view")
	}
	if strings.Contains(arch, "the child") {
		t.Error("a cascade-archived child should not be listed as its own archive root")
	}
}

func TestPromoteAndDemoteViaModal(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	parent := makeItem(t, client, base, token, todo, "Parent")
	sub := subtaskOf(t, client, base, token, parent, "Floater")

	if strings.Contains(getBody(t, client, base+"/general", http.StatusOK), "Floater") {
		t.Fatal("subtask should not start on the board")
	}

	// Promote: parent_id "" lifts it to the board.
	resp := postJSON(t, client, base+"/general/items/"+sub+"/parent", token, map[string]any{"parent_id": ""})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("promote: want 204, got %d", resp.StatusCode)
	}
	if !strings.Contains(getBody(t, client, base+"/general", http.StatusOK), "Floater") {
		t.Error("promoted item not on the board")
	}

	// Demote it back under the parent.
	resp2 := postJSON(t, client, base+"/general/items/"+sub+"/parent", token, map[string]any{"parent_id": parent})
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNoContent {
		t.Fatalf("demote: want 204, got %d", resp2.StatusCode)
	}
	if strings.Contains(getBody(t, client, base+"/general", http.StatusOK), "Floater") {
		t.Error("demoted item should be off the board again")
	}
}

func TestReparentCycleRejectedWeb(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	parent := makeItem(t, client, base, token, statusID(t, client, base, "To do"), "Parent")
	child := subtaskOf(t, client, base, token, parent, "Child")

	// Parenting the parent under its own child is a cycle -> 409.
	resp := postJSON(t, client, base+"/general/items/"+parent+"/parent", token, map[string]any{"parent_id": child})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("cycle: want 409, got %d", resp.StatusCode)
	}
}

func TestMilestoneModeColumns(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")

	ms := makeItem(t, client, base, token, todo, "Phase One")
	subtaskOf(t, client, base, token, ms, "Build the thing")
	makeItem(t, client, base, token, todo, "Loose task")

	// Mark "Phase One" as a milestone.
	mk := postJSON(t, client, base+"/general/items/"+ms+"/milestone", token, map[string]any{"is_milestone": true})
	mk.Body.Close()
	if mk.StatusCode != http.StatusNoContent {
		t.Fatalf("set milestone: want 204, got %d", mk.StatusCode)
	}

	page := getBody(t, client, base+"/general?mode=milestone", http.StatusOK)
	if !strings.Contains(page, "Backlog") {
		t.Error("milestone mode missing the Backlog column")
	}
	// The milestone is a column header (data-open), not a card.
	if !strings.Contains(page, `data-open="`+ms+`"`) || !strings.Contains(page, `data-parent-id="`+ms+`"`) {
		t.Error("Phase One is not rendered as a milestone column")
	}
	// Its child shows in its column; the loose root task shows in Backlog.
	if !strings.Contains(page, "Build the thing") {
		t.Error("milestone child missing from its column")
	}
	if !strings.Contains(page, "Loose task") {
		t.Error("loose root task missing from Backlog")
	}

	// Status mode is unaffected: the milestone is still a normal card there, and
	// its child stays off-board.
	status := getBody(t, client, base+"/general", http.StatusOK)
	if !strings.Contains(status, "Phase One") {
		t.Error("milestone should still be a card in status mode")
	}
	if strings.Contains(status, "Build the thing") {
		t.Error("milestone child should stay off the status board")
	}
}

func TestDeletePermanentFromArchive(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	id := makeItem(t, client, base, token, statusID(t, client, base, "To do"), "Gone soon")

	postJSON(t, client, base+"/general/items/"+id+"/archive", token, nil).Body.Close()
	del := postForm(t, client, base+"/general/items/"+id+"/delete", url.Values{"csrf_token": {token}})
	del.Body.Close()
	if del.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete form: want 303, got %d", del.StatusCode)
	}
	if strings.Contains(getBody(t, client, base+"/general/archive", http.StatusOK), "Gone soon") {
		t.Error("permanently deleted item still in the archive")
	}
}
