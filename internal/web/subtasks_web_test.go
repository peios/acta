package web_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// createSubtask makes a child of parent and returns its id.
func createSubtask(t *testing.T, client *http.Client, base, token, parent, title string) string {
	t.Helper()
	resp := postJSON(t, client, base+"/general/items/"+parent+"/subtasks", token, map[string]any{"title": title})
	defer resp.Body.Close()
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil || created.ID == "" {
		t.Fatalf("create subtask %q: id=%q err=%v", title, created.ID, err)
	}
	return created.ID
}

// refOf pulls the human ref (e.g. GEN-1) rendered on a card.
func refOf(body, id string) string {
	re := regexp.MustCompile(`data-item-id="` + id + `"[\s\S]*?<span class="item-ref">([^<]+)</span>`)
	if m := re.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	return ""
}

// TestShowSubtasks checks the Show-sub-tasks toggle: off (default) the board is
// root-only; on, children appear as cards with a "↳ <parent-ref>" chip.
func TestShowSubtasks(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	parent := createItem(t, client, base, token, todo, "Parent task")
	child := createSubtask(t, client, base, token, parent, "Child task")

	// Default: root-only — the child is not a card; the toggle reads off.
	off := getBody(t, client, base+"/general", http.StatusOK)
	if strings.Contains(off, `data-item-id="`+child+`"`) {
		t.Error("the child should not be a board card by default")
	}
	if !strings.Contains(off, `data-subtasks-toggle aria-label="Show sub-tasks"`) &&
		!strings.Contains(off, `aria-checked="false"`) {
		t.Error("the Show sub-tasks toggle should render, off by default")
	}

	// On: the child appears with a parent-ref chip naming the parent.
	on := getBody(t, client, base+"/general?subtasks=1", http.StatusOK)
	if !strings.Contains(on, `data-item-id="`+child+`"`) {
		t.Fatal("the child should be a board card when subtasks are shown")
	}
	parentRef := refOf(on, parent)
	if parentRef == "" {
		t.Fatal("could not read the parent's ref from the board")
	}
	if !strings.Contains(on, `class="item-parent" title="Sub-task of `+parentRef+`"`) {
		t.Errorf("the child card should carry a parent-ref chip for %q", parentRef)
	}
	if !strings.Contains(on, `aria-checked="true"`) {
		t.Error("the toggle should read on under ?subtasks=1")
	}
}
