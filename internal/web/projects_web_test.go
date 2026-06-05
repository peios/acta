package web_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// projectIDRe pulls a project's id out of its edit-form action on the project page.
var projectIDRe = regexp.MustCompile(`/general/projects/([a-z0-9]+)/edit`)

// createItem makes a board item via the JSON API and returns its id.
func createItem(t *testing.T, client *http.Client, base, token, statusID, title string) string {
	t.Helper()
	resp := postJSON(t, client, base+"/general/items", token, map[string]any{"status_id": statusID, "title": title})
	defer resp.Body.Close()
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil || created.ID == "" {
		t.Fatalf("create item %q: id=%q err=%v", title, created.ID, err)
	}
	return created.ID
}

func TestProjectsOverviewAndCreate(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	// The sidebar carries the Projects link on board pages.
	if body := getBody(t, client, base+"/general", http.StatusOK); !strings.Contains(body, `href="/general/projects"`) {
		t.Error("sidebar missing Projects link")
	}

	// The overview starts empty.
	if body := getBody(t, client, base+"/general/projects", http.StatusOK); !strings.Contains(body, "No projects yet") {
		t.Error("empty overview missing empty state")
	}

	// Create a project.
	resp := postForm(t, client, base+"/general/projects", url.Values{
		"name": {"Peinit"}, "brief": {"boot work"}, "status": {"active"},
		"lead_id": {""}, "color": {""}, "csrf_token": {token},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create: want 303, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/general/projects?p=peinit") {
		t.Fatalf("create redirect = %q, want the new project page", loc)
	}

	// The overview now lists it; the single page shows its brief.
	if body := getBody(t, client, base+"/general/projects", http.StatusOK); !strings.Contains(body, "Peinit") {
		t.Error("overview missing the created project")
	}
	page := getBody(t, client, base+"/general/projects?p=peinit", http.StatusOK)
	if !strings.Contains(page, "Peinit") || !strings.Contains(page, "boot work") {
		t.Error("project page missing name or brief")
	}
}

func TestProjectInvalidNameRejected(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	resp := postForm(t, client, base+"/general/projects", url.Values{
		"name": {"   "}, "status": {"active"}, "csrf_token": {token},
	})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "err=invalid_name") {
		t.Fatalf("blank name should bounce with err=invalid_name, got %q", loc)
	}
}

// cardRe captures one board card's item id and whether it's filtered out.
var cardRe = regexp.MustCompile(`<article class="item([^"]*)"[^>]*data-item-id="([a-z0-9]+)"`)

func TestBoardFiltersByProject(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	// One project, two items: one filed under it, one left unfiled.
	postForm(t, client, base+"/general/projects", url.Values{
		"name": {"Peinit"}, "status": {"active"}, "csrf_token": {token},
	}).Body.Close()
	page := getBody(t, client, base+"/general/projects?p=peinit", http.StatusOK)
	projectID := projectIDRe.FindStringSubmatch(page)[1]

	todo := statusID(t, client, base, "To do")
	filed := createItem(t, client, base, token, todo, "filed item")
	createItem(t, client, base, token, todo, "loose item")

	r := postJSON(t, client, base+"/general/items/"+filed+"/project", token, map[string]any{"project_id": projectID})
	r.Body.Close()

	// The filter facet is offered, and the badge reflects the active project.
	board := getBody(t, client, base+"/general?project="+projectID, http.StatusOK)
	if !strings.Contains(board, `data-facet="project"`) {
		t.Error("board filter is missing the Project facet")
	}
	// The filed card is shown; the loose card is server-side filtered out.
	for _, m := range cardRe.FindAllStringSubmatch(board, -1) {
		classes, id := m[1], m[2]
		hidden := strings.Contains(classes, "is-filtered")
		if id == filed && hidden {
			t.Error("filed item should be visible under its project filter")
		}
		if id != filed && !hidden {
			t.Error("an item outside the filtered project should be hidden")
		}
	}
}

func TestBoardCardShowsProjectChip(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	postForm(t, client, base+"/general/projects", url.Values{
		"name": {"Peinit"}, "status": {"active"}, "csrf_token": {token},
	}).Body.Close()
	pid := projectIDRe.FindStringSubmatch(getBody(t, client, base+"/general/projects?p=peinit", http.StatusOK))[1]

	todo := statusID(t, client, base, "To do")
	id := createItem(t, client, base, token, todo, "chip me")
	postJSON(t, client, base+"/general/items/"+id+"/project", token, map[string]any{"project_id": pid}).Body.Close()

	board := getBody(t, client, base+"/general", http.StatusOK)
	// The card carries a project chip naming the project.
	if !strings.Contains(board, `class="item-project"`) || !strings.Contains(board, `<span class="item-project-name">Peinit</span>`) {
		t.Error("board card missing its project chip")
	}
	// The display popover offers a Project toggle.
	if !strings.Contains(board, `data-display="project"`) {
		t.Error("display popover missing the Project toggle")
	}
}

func TestItemProjectAssignment(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	// A project to file work under.
	postForm(t, client, base+"/general/projects", url.Values{
		"name": {"Peinit"}, "status": {"active"}, "csrf_token": {token},
	}).Body.Close()
	page := getBody(t, client, base+"/general/projects?p=peinit", http.StatusOK)
	m := projectIDRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("could not find project id on its page")
	}
	projectID := m[1]

	// An item on the board.
	todo := statusID(t, client, base, "To do")
	cr := postJSON(t, client, base+"/general/items", token, map[string]any{"status_id": todo, "title": "ship boot"})
	var created struct {
		ID string `json:"id"`
	}
	json.NewDecoder(cr.Body).Decode(&created)
	cr.Body.Close()
	if created.ID == "" {
		t.Fatal("item create returned no id")
	}

	// File it under the project.
	r := postJSON(t, client, base+"/general/items/"+created.ID+"/project", token, map[string]any{"project_id": projectID})
	r.Body.Close()
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("set project: want 204, got %d", r.StatusCode)
	}

	// The project page now lists the item.
	if page = getBody(t, client, base+"/general/projects?p=peinit", http.StatusOK); !strings.Contains(page, "ship boot") {
		t.Error("project page missing the filed item")
	}

	// The modal renders the project picker with the project selected.
	modal := getBody(t, client, base+"/general/items/"+created.ID+"/modal", http.StatusOK)
	if !strings.Contains(modal, `class="modal-project"`) {
		t.Error("item modal missing project picker")
	}
	if !strings.Contains(modal, `value="`+projectID+`" data-color`) {
		t.Error("item modal project option missing the selected project")
	}

	// Clearing it works too.
	r2 := postJSON(t, client, base+"/general/items/"+created.ID+"/project", token, map[string]any{"project_id": ""})
	r2.Body.Close()
	if r2.StatusCode != http.StatusNoContent {
		t.Fatalf("clear project: want 204, got %d", r2.StatusCode)
	}
	if page = getBody(t, client, base+"/general/projects?p=peinit", http.StatusOK); strings.Contains(page, "ship boot") {
		t.Error("project page still lists the item after clearing its project")
	}
}
