package web_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var docIDRe = regexp.MustCompile(`data-doc-id="([a-z0-9]+)"`)

// TestDocumentsCRUD walks the document lifecycle over HTTP: the Documents
// section renders on the modal, create returns a rendered card (markdown ->
// sanitized HTML), the card shows up in the modal, edit replaces it, a blank
// title is rejected, and delete removes it.
func TestDocumentsCRUD(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	item := createItem(t, client, base, token, todo, "Audit task")

	// The modal shows the Documents section and its add affordance, no cards yet.
	modal := getBody(t, client, base+"/general/items/"+item+"/modal", http.StatusOK)
	if !strings.Contains(modal, "data-docs") || !strings.Contains(modal, "Add document") {
		t.Fatal("the Documents section should render on the modal")
	}
	if strings.Contains(modal, `class="doc"`) {
		t.Error("no document cards expected on a fresh item")
	}

	// Create returns the rendered card with the title and rendered markdown body.
	resp := postJSON(t, client, base+"/general/items/"+item+"/documents", token,
		map[string]any{"title": "Compliance Report", "body": "**Pass**"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	card := readBody(t, resp)
	if !strings.Contains(card, "Compliance Report") || !strings.Contains(card, "<strong>Pass</strong>") {
		t.Fatalf("card should carry the title and rendered markdown:\n%s", card)
	}
	m := docIDRe.FindStringSubmatch(card)
	if m == nil {
		t.Fatalf("could not read the new document id from:\n%s", card)
	}
	docID := m[1]

	// It now renders inside the modal.
	modal = getBody(t, client, base+"/general/items/"+item+"/modal", http.StatusOK)
	if !strings.Contains(modal, `data-doc-id="`+docID+`"`) || !strings.Contains(modal, "Compliance Report") {
		t.Fatal("the new document should render on the modal")
	}

	// A blank title is rejected with 400.
	bad := postJSON(t, client, base+"/general/items/"+item+"/documents", token,
		map[string]any{"title": "   ", "body": "x"})
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("blank title status = %d, want 400", bad.StatusCode)
	}
	bad.Body.Close()

	// Edit replaces title and body, returning the re-rendered card.
	er := postJSON(t, client, base+"/general/items/"+item+"/documents/"+docID+"/edit", token,
		map[string]any{"title": "Final Report", "body": "_done_"})
	if er.StatusCode != http.StatusOK {
		t.Fatalf("edit status = %d", er.StatusCode)
	}
	edited := readBody(t, er)
	if !strings.Contains(edited, "Final Report") || !strings.Contains(edited, "<em>done</em>") {
		t.Fatalf("edited card wrong:\n%s", edited)
	}

	// Delete returns 204; the document is then gone from the modal.
	dr := postJSON(t, client, base+"/general/items/"+item+"/documents/"+docID+"/delete", token, nil)
	if dr.StatusCode != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", dr.StatusCode)
	}
	dr.Body.Close()
	modal = getBody(t, client, base+"/general/items/"+item+"/modal", http.StatusOK)
	if strings.Contains(modal, `data-doc-id="`+docID+`"`) {
		t.Error("deleted document should be gone from the modal")
	}

	// Editing a now-deleted document 404s.
	gone := postJSON(t, client, base+"/general/items/"+item+"/documents/"+docID+"/edit", token,
		map[string]any{"title": "T", "body": "B"})
	if gone.StatusCode != http.StatusNotFound {
		t.Errorf("edit of deleted doc status = %d, want 404", gone.StatusCode)
	}
	gone.Body.Close()
}

// TestDocumentRawHTMLSanitized confirms a document body goes through the same
// sanitizer as descriptions and comments: raw <script> is escaped, not executed.
func TestDocumentRawHTMLSanitized(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	item := createItem(t, client, base, token, todo, "task")

	resp := postJSON(t, client, base+"/general/items/"+item+"/documents", token,
		map[string]any{"title": "X", "body": "<script>alert(1)</script>"})
	card := readBody(t, resp)
	if strings.Contains(card, "<script>alert(1)</script>") {
		t.Errorf("script tag should be escaped, not passed through:\n%s", card)
	}
}
