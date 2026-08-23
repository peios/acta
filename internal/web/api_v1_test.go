package web_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type apiItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Assignee  string `json:"assignee"`
	Project   string `json:"project"`
	Release   string `json:"release"`
	CreatedBy string `json:"created_by"`
}

// mintToken logs-in flow already done; mints a PAT for the current session and
// returns its plaintext.
func mintToken(t *testing.T, client *http.Client, base, csrf string) string {
	t.Helper()
	resp := postForm(t, client, base+"/account/tokens", url.Values{"name": {"api"}, "csrf_token": {csrf}})
	body := readBody(t, resp)
	m := tokenValueRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("token not minted:\n%s", body)
	}
	return m[1]
}

// bearerJSON calls the API with a fresh client (no cookies), proving the API is
// authenticated solely by the token and needs no CSRF.
func bearerJSON(t *testing.T, base, method, path, token string, body any) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, base+path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeItem(t *testing.T, resp *http.Response, wantStatus int) apiItem {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("want %d, got %d: %s", wantStatus, resp.StatusCode, b)
	}
	var it apiItem
	if err := json.NewDecoder(resp.Body).Decode(&it); err != nil {
		t.Fatal(err)
	}
	return it
}

func TestAPIRequiresToken(t *testing.T) {
	base, _ := newTestServer(t)
	if r := bearerJSON(t, base, "GET", "/api/v1/workspaces", "", nil); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: want 401, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
}

func TestAPIListCreateTransition(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)

	// Create with no status -> lands in the first lane, attributed to jack.
	// No cookie on this client: a successful POST proves the API skips CSRF.
	created := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items", token,
		map[string]string{"title": "Ship it"}), http.StatusCreated)
	if created.Status != "To do" {
		t.Fatalf("default status = %q, want To do", created.Status)
	}
	if created.CreatedBy != "jack" {
		t.Fatalf("created_by = %q, want jack", created.CreatedBy)
	}

	// It shows up on the board listing.
	listResp := bearerJSON(t, base, "GET", "/api/v1/w/general/items", token, nil)
	listBody := readBody(t, listResp)
	if !strings.Contains(listBody, created.ID) || !strings.Contains(listBody, "Ship it") {
		t.Fatalf("board listing missing the new item:\n%s", listBody)
	}

	// Transition it by status name.
	moved := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items/"+created.ID+"/transition", token,
		map[string]string{"status": "Doing"}), http.StatusOK)
	if moved.Status != "Doing" {
		t.Fatalf("after transition status = %q, want Doing", moved.Status)
	}

	// An unknown status is a 400.
	bad := bearerJSON(t, base, "POST", "/api/v1/w/general/items/"+created.ID+"/transition", token,
		map[string]string{"status": "Nowhere"})
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown status: want 400, got %d", bad.StatusCode)
	}

	// Creating with an explicit status name works too.
	done := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items", token,
		map[string]string{"title": "Already done", "status": "Done"}), http.StatusCreated)
	if done.Status != "Done" {
		t.Fatalf("explicit status = %q, want Done", done.Status)
	}
}

func TestAPIProjects(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)

	// Create a project; lead "me" resolves to the caller, status defaults active.
	resp := bearerJSON(t, base, "POST", "/api/v1/w/general/projects", token,
		map[string]string{"name": "Peinit", "brief": "boot", "lead": "me"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create project: want 201, got %d: %s", resp.StatusCode, b)
	}
	var pr struct{ Slug, Status, Lead string }
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		t.Fatal(err)
	}
	if pr.Slug != "peinit" || pr.Status != "active" || pr.Lead != "jack" {
		t.Fatalf("project = %+v, want peinit/active/jack", pr)
	}

	// It lists.
	if lb := readBody(t, bearerJSON(t, base, "GET", "/api/v1/w/general/projects", token, nil)); !strings.Contains(lb, `"slug":"peinit"`) {
		t.Fatalf("project list missing peinit:\n%s", lb)
	}

	// Create an item filed under the project at creation.
	filed := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items", token,
		map[string]string{"title": "write spec", "project": "peinit"}), http.StatusCreated)
	if filed.Project != "peinit" {
		t.Fatalf("created item project = %q, want peinit", filed.Project)
	}
	loose := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items", token,
		map[string]string{"title": "unfiled"}), http.StatusCreated)

	// The ?project filter narrows the listing.
	fb := readBody(t, bearerJSON(t, base, "GET", "/api/v1/w/general/items?project=peinit", token, nil))
	if !strings.Contains(fb, filed.ID) || strings.Contains(fb, loose.ID) {
		t.Fatalf("?project filter wrong:\n%s", fb)
	}

	// The set-project endpoint clears it.
	cleared := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items/"+filed.ID+"/project", token,
		map[string]string{"project": ""}), http.StatusOK)
	if cleared.Project != "" {
		t.Fatalf("cleared project = %q, want empty", cleared.Project)
	}

	// An unknown project slug is a 400.
	bad := bearerJSON(t, base, "POST", "/api/v1/w/general/items/"+filed.ID+"/project", token,
		map[string]string{"project": "nope"})
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown project: want 400, got %d", bad.StatusCode)
	}
}

func TestAPIReleases(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)

	// Create a release as planned.
	resp := bearerJSON(t, base, "POST", "/api/v1/w/general/releases", token,
		map[string]string{"name": "v0.27.0", "description": "the cut", "status": "planned"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create release: want 201, got %d: %s", resp.StatusCode, b)
	}
	var rel struct{ Name, Status string }
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		t.Fatal(err)
	}
	if rel.Name != "v0.27.0" || rel.Status != "planned" {
		t.Fatalf("release = %+v, want v0.27.0/planned", rel)
	}

	// It lists.
	if lb := readBody(t, bearerJSON(t, base, "GET", "/api/v1/w/general/releases", token, nil)); !strings.Contains(lb, `"name":"v0.27.0"`) {
		t.Fatalf("release list missing v0.27.0:\n%s", lb)
	}

	// Create an item added to the release at creation; the item reflects it.
	in := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items", token,
		map[string]string{"title": "ship it", "release": "v0.27.0"}), http.StatusCreated)
	if in.Release != "v0.27.0" {
		t.Fatalf("created item release = %q, want v0.27.0", in.Release)
	}
	loose := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items", token,
		map[string]string{"title": "unfiled"}), http.StatusCreated)

	// The ?release filter narrows the listing.
	fb := readBody(t, bearerJSON(t, base, "GET", "/api/v1/w/general/items?release=v0.27.0", token, nil))
	if !strings.Contains(fb, in.ID) || strings.Contains(fb, loose.ID) {
		t.Fatalf("?release filter wrong:\n%s", fb)
	}

	// The target date sets on create, moves through its own endpoint, and comes
	// back with the size-weighted progress an agent plans against.
	tgt := readBody(t, bearerJSON(t, base, "POST", "/api/v1/w/general/releases/v0.27.0/target", token,
		map[string]string{"target_date": "2026-10-14"}))
	if !strings.Contains(tgt, `"target_date":"2026-10-14"`) {
		t.Fatalf("set target response wrong:\n%s", tgt)
	}
	listed := readBody(t, bearerJSON(t, base, "GET", "/api/v1/w/general/releases", token, nil))
	if !strings.Contains(listed, `"target_date":"2026-10-14"`) || !strings.Contains(listed, `"total_points":3`) {
		t.Fatalf("release list missing target/points:\n%s", listed)
	}
	if bad := bearerJSON(t, base, "POST", "/api/v1/w/general/releases/v0.27.0/target", token,
		map[string]string{"target_date": "whenever"}); bad.StatusCode != http.StatusBadRequest {
		bad.Body.Close()
		t.Fatalf("bad target date: want 400, got %d", bad.StatusCode)
	}

	// Lifecycle: activate then ship.
	st := bearerJSON(t, base, "POST", "/api/v1/w/general/releases/v0.27.0/status", token, map[string]string{"status": "active"})
	st.Body.Close()
	shipped := readBody(t, bearerJSON(t, base, "POST", "/api/v1/w/general/releases/v0.27.0/status", token, map[string]string{"status": "shipped"}))
	if !strings.Contains(shipped, `"status":"shipped"`) || !strings.Contains(shipped, `"shipped_at"`) {
		t.Fatalf("ship response wrong:\n%s", shipped)
	}

	// Clearing the item's release.
	cleared := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items/"+in.ID+"/release", token,
		map[string]string{"release": ""}), http.StatusOK)
	if cleared.Release != "" {
		t.Fatalf("cleared release = %q, want empty", cleared.Release)
	}

	// Unknown release names are rejected.
	if bad := bearerJSON(t, base, "POST", "/api/v1/w/general/items/"+in.ID+"/release", token,
		map[string]string{"release": "nope"}); bad.StatusCode != http.StatusNotFound {
		bad.Body.Close()
		t.Fatalf("unknown release: want 404, got %d", bad.StatusCode)
	}
}

func TestAPIAgentCreatesAttributed(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)

	// Create an agent and mint its token.
	resp := postForm(t, client, base+"/account/agents", url.Values{
		"name": {"deploybot"}, "csrf_token": {csrf},
	})
	resp.Body.Close()
	loc := resp.Header.Get("Location") // /account/agents/{id}
	mint := postForm(t, client, base+loc+"/tokens", url.Values{"name": {"ci"}, "csrf_token": {csrf}})
	m := tokenValueRe.FindStringSubmatch(readBody(t, mint))
	if m == nil {
		t.Fatal("agent token not minted")
	}
	agentToken := m[1]

	// The agent creates an item; it's attributed to the agent's handle.
	created := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items", agentToken,
		map[string]string{"title": "Built by a bot"}), http.StatusCreated)
	if created.CreatedBy != "jack/deploybot" {
		t.Fatalf("created_by = %q, want jack/deploybot", created.CreatedBy)
	}
}

func TestAPISubscriptions(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)

	// Create a project to follow.
	cp := bearerJSON(t, base, "POST", "/api/v1/w/general/projects", token, map[string]string{"name": "Peinit"})
	var pr struct{ Slug string }
	if err := json.NewDecoder(cp.Body).Decode(&pr); err != nil {
		t.Fatal(err)
	}
	cp.Body.Close()

	// Subscribe to it (no events -> the project default filter).
	subResp := bearerJSON(t, base, "POST", "/api/v1/subscriptions", token,
		map[string]any{"type": "project", "ref": pr.Slug, "workspace": "general"})
	if subResp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe: want 200, got %d", subResp.StatusCode)
	}
	subResp.Body.Close()

	// List shows it, addressed by slug.
	listBody := readBody(t, bearerJSON(t, base, "GET", "/api/v1/subscriptions", token, nil))
	if !strings.Contains(listBody, pr.Slug) || !strings.Contains(listBody, "items_added") {
		t.Fatalf("subscription listing missing the project:\n%s", listBody)
	}

	// A project ref without a workspace is a 400.
	bad := bearerJSON(t, base, "POST", "/api/v1/subscriptions", token,
		map[string]any{"type": "project", "ref": pr.Slug})
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing workspace: want 400, got %d", bad.StatusCode)
	}

	// Unsubscribe (DELETE with a body) -> 204, and the list no longer has it.
	del := bearerJSON(t, base, "DELETE", "/api/v1/subscriptions", token,
		map[string]any{"type": "project", "ref": pr.Slug, "workspace": "general"})
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("unsubscribe: want 204, got %d", del.StatusCode)
	}
	if after := readBody(t, bearerJSON(t, base, "GET", "/api/v1/subscriptions", token, nil)); strings.Contains(after, pr.Slug) {
		t.Fatalf("project still listed after unsubscribe:\n%s", after)
	}
}
