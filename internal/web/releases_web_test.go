package web_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// createRelease makes a release via the form post and returns its id (read from
// the redirect to its detail page, /general/releases?r=<id>).
func createRelease(t *testing.T, client *http.Client, base, token, name, desc string) string {
	t.Helper()
	resp := postForm(t, client, base+"/general/releases", url.Values{
		"name": {name}, "description": {desc}, "csrf_token": {token},
	})
	resp.Body.Close()
	return releaseIDFromRedirect(t, resp)
}

// createReleaseAs is createRelease with an explicit lifecycle status (planned).
func createReleaseAs(t *testing.T, client *http.Client, base, token, name, status string) string {
	t.Helper()
	resp := postForm(t, client, base+"/general/releases", url.Values{
		"name": {name}, "status": {status}, "csrf_token": {token},
	})
	resp.Body.Close()
	return releaseIDFromRedirect(t, resp)
}

func releaseIDFromRedirect(t *testing.T, resp *http.Response) string {
	t.Helper()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create release: want 303, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	const pfx = "/general/releases?r="
	if !strings.HasPrefix(loc, pfx) {
		t.Fatalf("create redirect = %q, want %s<id>", loc, pfx)
	}
	return strings.TrimPrefix(loc, pfx)
}

func TestReleasesOverviewAndCreate(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	// The sidebar carries the Releases link on board pages.
	if body := getBody(t, client, base+"/general", http.StatusOK); !strings.Contains(body, `href="/general/releases"`) {
		t.Error("sidebar missing Releases link")
	}

	// The overview starts with no active releases.
	if body := getBody(t, client, base+"/general/releases", http.StatusOK); !strings.Contains(body, "No active releases") {
		t.Error("empty overview missing empty state")
	}

	id := createRelease(t, client, base, token, "v0.27.0", "the next cut")

	// The overview lists it as active; the detail page shows its notes.
	if body := getBody(t, client, base+"/general/releases", http.StatusOK); !strings.Contains(body, "v0.27.0") {
		t.Error("overview missing the created release")
	}
	page := getBody(t, client, base+"/general/releases?r="+id, http.StatusOK)
	if !strings.Contains(page, "v0.27.0") || !strings.Contains(page, "the next cut") {
		t.Error("release page missing name or notes")
	}
	if !strings.Contains(page, `data-status="active"`) {
		t.Error("a fresh release should read as active")
	}
}

func TestReleaseInvalidNameRejected(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	resp := postForm(t, client, base+"/general/releases", url.Values{
		"name": {"   "}, "csrf_token": {token},
	})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "err=invalid_name") {
		t.Fatalf("blank name should bounce with err=invalid_name, got %q", loc)
	}
}

func TestReleaseShipFlow(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	id := createRelease(t, client, base, token, "v0.27.0", "")

	// Ship it: detail now reads as shipped and is listed under Shipped.
	resp := postForm(t, client, base+"/general/releases/"+id+"/status", url.Values{"status": {"shipped"}, "csrf_token": {token}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("ship: want 303, got %d", resp.StatusCode)
	}
	if page := getBody(t, client, base+"/general/releases?r="+id, http.StatusOK); !strings.Contains(page, `data-status="shipped"`) {
		t.Error("shipped release should read as shipped")
	}
	if body := getBody(t, client, base+"/general/releases", http.StatusOK); !strings.Contains(body, "Shipped ·") {
		t.Error("overview missing the Shipped section")
	}

	// Reopen it: back to active.
	postForm(t, client, base+"/general/releases/"+id+"/status", url.Values{"status": {"active"}, "csrf_token": {token}}).Body.Close()
	if page := getBody(t, client, base+"/general/releases?r="+id, http.StatusOK); !strings.Contains(page, `data-status="active"`) {
		t.Error("reopened release should read as active again")
	}
}

func TestReleaseCreatePlanned(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	// Create directly as Planned.
	resp := postForm(t, client, base+"/general/releases", url.Values{
		"name": {"v1.0"}, "status": {"planned"}, "csrf_token": {token},
	})
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	id := strings.TrimPrefix(loc, "/general/releases?r=")

	// Its detail page reads as planned, and the overview has a Planned section.
	if page := getBody(t, client, base+"/general/releases?r="+id, http.StatusOK); !strings.Contains(page, `data-status="planned"`) {
		t.Error("release created as planned should read as planned")
	}
	if body := getBody(t, client, base+"/general/releases", http.StatusOK); !strings.Contains(body, "Planned ·") {
		t.Error("overview missing the Planned section")
	}

	// Activating it moves it to active, and the active page offers a way back to
	// planned (a hidden status=planned form).
	postForm(t, client, base+"/general/releases/"+id+"/status", url.Values{"status": {"active"}, "csrf_token": {token}}).Body.Close()
	page := getBody(t, client, base+"/general/releases?r="+id, http.StatusOK)
	if !strings.Contains(page, `data-status="active"`) {
		t.Error("activated release should read as active")
	}
	if !strings.Contains(page, `value="planned"`) {
		t.Error("an active release should offer Move to planned")
	}

	// Demoting it back to planned works.
	postForm(t, client, base+"/general/releases/"+id+"/status", url.Values{"status": {"planned"}, "csrf_token": {token}}).Body.Close()
	if page := getBody(t, client, base+"/general/releases?r="+id, http.StatusOK); !strings.Contains(page, `data-status="planned"`) {
		t.Error("release demoted back to planned should read as planned")
	}
}

func TestItemReleaseAssignment(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	relID := createRelease(t, client, base, token, "v0.27.0", "")
	todo := statusID(t, client, base, "To do")
	id := createItem(t, client, base, token, todo, "ship boot")

	// Add it to the release.
	r := postJSON(t, client, base+"/general/items/"+id+"/release", token, map[string]any{"release_id": relID})
	r.Body.Close()
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("set release: want 204, got %d", r.StatusCode)
	}

	// The release page (changelog) now lists the item.
	if page := getBody(t, client, base+"/general/releases?r="+relID, http.StatusOK); !strings.Contains(page, "ship boot") {
		t.Error("release page missing the added item")
	}

	// The modal renders the release picker with the release selected.
	modal := getBody(t, client, base+"/general/items/"+id+"/modal", http.StatusOK)
	if !strings.Contains(modal, `class="modal-release"`) {
		t.Error("item modal missing release picker")
	}
	if !strings.Contains(modal, `value="`+relID+`" data-color`) {
		t.Error("item modal release option missing the selected release")
	}

	// Clearing it works too.
	r2 := postJSON(t, client, base+"/general/items/"+id+"/release", token, map[string]any{"release_id": ""})
	r2.Body.Close()
	if r2.StatusCode != http.StatusNoContent {
		t.Fatalf("clear release: want 204, got %d", r2.StatusCode)
	}
	if page := getBody(t, client, base+"/general/releases?r="+relID, http.StatusOK); strings.Contains(page, "ship boot") {
		t.Error("release page still lists the item after clearing its release")
	}
}

func TestBoardFiltersByRelease(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	relID := createRelease(t, client, base, token, "v0.27.0", "")
	todo := statusID(t, client, base, "To do")
	tagged := createItem(t, client, base, token, todo, "tagged item")
	createItem(t, client, base, token, todo, "loose item")
	postJSON(t, client, base+"/general/items/"+tagged+"/release", token, map[string]any{"release_id": relID}).Body.Close()

	board := getBody(t, client, base+"/general?release="+relID, http.StatusOK)
	if !strings.Contains(board, `data-facet="release"`) {
		t.Error("board filter is missing the Release facet")
	}
	for _, m := range cardRe.FindAllStringSubmatch(board, -1) {
		classes, cid := m[1], m[2]
		hidden := strings.Contains(classes, "is-filtered")
		if cid == tagged && hidden {
			t.Error("tagged item should be visible under its release filter")
		}
		if cid != tagged && !hidden {
			t.Error("an item outside the filtered release should be hidden")
		}
	}
}

func TestBoardFiltersByCurrentRelease(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	// An active release, a shipped one, and a planned one.
	active := createRelease(t, client, base, token, "v0.28.0", "")
	shipped := createRelease(t, client, base, token, "v0.27.0", "")
	planned := createReleaseAs(t, client, base, token, "v1.0", "planned")

	todo := statusID(t, client, base, "To do")
	inActive := createItem(t, client, base, token, todo, "active item")
	inShipped := createItem(t, client, base, token, todo, "shipped item")
	inPlanned := createItem(t, client, base, token, todo, "planned item")
	createItem(t, client, base, token, todo, "loose item")
	postJSON(t, client, base+"/general/items/"+inActive+"/release", token, map[string]any{"release_id": active}).Body.Close()
	postJSON(t, client, base+"/general/items/"+inShipped+"/release", token, map[string]any{"release_id": shipped}).Body.Close()
	postJSON(t, client, base+"/general/items/"+inPlanned+"/release", token, map[string]any{"release_id": planned}).Body.Close()
	postForm(t, client, base+"/general/releases/"+shipped+"/status", url.Values{"status": {"shipped"}, "csrf_token": {token}}).Body.Close()

	// The "Current release" token (release=active) shows only items in an *active*
	// release — not planned, not shipped. So only the active one is visible; the
	// planned, shipped-release and loose items are hidden.
	board := getBody(t, client, base+"/general?release=active", http.StatusOK)
	if !strings.Contains(board, `value="active"`) {
		t.Error("release facet missing the Current release token")
	}
	for _, m := range cardRe.FindAllStringSubmatch(board, -1) {
		classes, cid := m[1], m[2]
		hidden := strings.Contains(classes, "is-filtered")
		if cid == inActive && hidden {
			t.Error("item in an active release should be visible under Current release")
		}
		if cid != inActive && !hidden {
			t.Error("items not in an active release should be hidden under Current release")
		}
	}
}

func TestItemOverflowAndConvert(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	todo := statusID(t, client, base, "To do")

	// A normal task: the overflow menu is present but empty.
	normal := createItem(t, client, base, token, todo, "normal task")
	m := getBody(t, client, base+"/general/items/"+normal+"/modal", http.StatusOK)
	if !strings.Contains(m, "data-kebab") {
		t.Error("item modal missing the overflow menu")
	}
	if !strings.Contains(m, "No actions yet") || strings.Contains(m, "Convert to Release") {
		t.Error("a normal task's overflow menu should be empty")
	}

	// A milestone with a sub-task: the menu offers Convert to Release.
	ms := createItem(t, client, base, token, todo, "v1.0")
	postJSON(t, client, base+"/general/items/"+ms+"/milestone", token, map[string]any{"is_milestone": true}).Body.Close()
	postJSON(t, client, base+"/general/items/"+ms+"/subtasks", token, map[string]any{"title": "sub a"}).Body.Close()
	if mm := getBody(t, client, base+"/general/items/"+ms+"/modal", http.StatusOK); !strings.Contains(mm, "Convert to Release") {
		t.Error("a milestone's overflow menu should offer Convert to Release")
	}

	// Convert: returns the new release URL; the release lists the promoted sub-task.
	r := postJSON(t, client, base+"/general/items/"+ms+"/convert-release", token, map[string]any{})
	var out struct {
		URL string `json:"url"`
	}
	json.NewDecoder(r.Body).Decode(&out)
	r.Body.Close()
	if !strings.HasPrefix(out.URL, "/general/releases?r=") {
		t.Fatalf("convert url = %q, want a release link", out.URL)
	}
	if page := getBody(t, client, base+out.URL, http.StatusOK); !strings.Contains(page, "v1.0") || !strings.Contains(page, "sub a") {
		t.Error("converted release should be named for the milestone and list its sub-task")
	}
}

func TestReleaseBoardMode(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	// With no releases, the Releases view tab is hidden.
	if body := getBody(t, client, base+"/general", http.StatusOK); strings.Contains(body, "?mode=release") {
		t.Error("Releases tab should be hidden when there are no releases")
	}

	rel := createRelease(t, client, base, token, "v0.28.0", "")
	todo := statusID(t, client, base, "To do")
	inRel := createItem(t, client, base, token, todo, "released work")
	loose := createItem(t, client, base, token, todo, "loose work")
	postJSON(t, client, base+"/general/items/"+inRel+"/release", token, map[string]any{"release_id": rel}).Body.Close()

	// The Releases tab now appears.
	if body := getBody(t, client, base+"/general", http.StatusOK); !strings.Contains(body, "?mode=release") {
		t.Error("Releases tab should appear once a release exists")
	}

	// Release mode: a "No release" column plus the release's own column, with each
	// card bucketed into the right one.
	board := getBody(t, client, base+"/general?mode=release", http.StatusOK)
	if !strings.Contains(board, `data-mode="release"`) {
		t.Fatal("release-mode board missing data-mode=release")
	}
	if !strings.Contains(board, `data-release-col=""`) {
		t.Error("release mode missing the No release column")
	}
	relSeg := columnSegment(board, `data-release-col="`+rel+`"`)
	if !strings.Contains(relSeg, `data-item-id="`+inRel+`"`) {
		t.Error("released item not in its release column")
	}
	if strings.Contains(relSeg, `data-item-id="`+loose+`"`) {
		t.Error("loose item should not be in the release column")
	}
	noneSeg := columnSegment(board, `data-release-col=""`)
	if !strings.Contains(noneSeg, `data-item-id="`+loose+`"`) {
		t.Error("loose item not in the No release column")
	}
}

// columnSegment returns the markup of the board column whose <section> carries
// the given marker attribute, up to its closing </section>.
func columnSegment(board, marker string) string {
	i := strings.Index(board, marker)
	if i < 0 {
		return ""
	}
	seg := board[i:]
	if end := strings.Index(seg, "</section>"); end >= 0 {
		seg = seg[:end]
	}
	return seg
}

func TestBoardCardShowsReleaseChip(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	relID := createRelease(t, client, base, token, "v0.27.0", "")
	todo := statusID(t, client, base, "To do")
	id := createItem(t, client, base, token, todo, "chip me")
	postJSON(t, client, base+"/general/items/"+id+"/release", token, map[string]any{"release_id": relID}).Body.Close()

	board := getBody(t, client, base+"/general", http.StatusOK)
	if !strings.Contains(board, `<span class="item-release-name">v0.27.0</span>`) {
		t.Error("board card missing its release chip")
	}
	if !strings.Contains(board, `data-display="release"`) {
		t.Error("display popover missing the Release toggle")
	}
}
