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
	CreatedBy string `json:"created_by"`
}

// mintToken logs-in flow already done; mints a PAT for the current session and
// returns its plaintext.
func mintToken(t *testing.T, client *http.Client, base, csrf string) string {
	t.Helper()
	resp := postForm(t, client, base+"/settings/tokens", url.Values{"name": {"api"}, "csrf_token": {csrf}})
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

func TestAPIAgentCreatesAttributed(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)

	// Create an agent and mint its token.
	resp := postForm(t, client, base+"/settings/agents", url.Values{
		"name": {"deploybot"}, "csrf_token": {csrf},
	})
	resp.Body.Close()
	loc := resp.Header.Get("Location") // /settings/agents/{id}
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
