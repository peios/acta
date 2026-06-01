package web_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type agentResp struct {
	ID     string `json:"id"`
	Handle string `json:"handle"`
	Name   string `json:"name"`
}

type tokenResp struct {
	ID     string `json:"id"`
	Token  string `json:"token"`
	Prefix string `json:"prefix"`
	Name   string `json:"name"`
}

func decodeJSON[T any](t *testing.T, resp *http.Response, wantStatus int) T {
	t.Helper()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("want %d, got %d: %s", wantStatus, resp.StatusCode, body)
	}
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	return out
}

func TestAPIAgentAndTokenProvisioning(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	human := mintToken(t, client, base, csrf)

	// Create an agent over the API.
	ag := decodeJSON[agentResp](t, bearerJSON(t, base, "POST", "/api/v1/agents", human,
		map[string]string{"name": "claude"}), http.StatusCreated)
	if ag.Handle != "jack/claude" || ag.Name != "claude" || ag.ID == "" {
		t.Fatalf("create agent = %+v", ag)
	}

	// It shows up in the owner's agent list.
	list := decodeJSON[[]agentResp](t, bearerJSON(t, base, "GET", "/api/v1/agents", human, nil), http.StatusOK)
	found := false
	for _, a := range list {
		if a.ID == ag.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("agent list missing %s: %+v", ag.ID, list)
	}

	// Mint a token for the agent; it comes back as one-time plaintext.
	tok := decodeJSON[tokenResp](t, bearerJSON(t, base, "POST", "/api/v1/agents/"+ag.ID+"/tokens", human,
		map[string]string{"name": "claude@thinkpad"}), http.StatusCreated)
	if !strings.HasPrefix(tok.Token, "acta_pat_") || tok.Prefix == "" {
		t.Fatalf("agent token = %+v", tok)
	}

	// The minted token authenticates as the agent: an item it creates is
	// attributed to the agent's handle.
	created := decodeItem(t, bearerJSON(t, base, "POST", "/api/v1/w/general/items", tok.Token,
		map[string]string{"title": "Provisioned"}), http.StatusCreated)
	if created.CreatedBy != "jack/claude" {
		t.Fatalf("agent-created item created_by = %q, want jack/claude", created.CreatedBy)
	}

	// A self token authenticates as the human.
	self := decodeJSON[tokenResp](t, bearerJSON(t, base, "POST", "/api/v1/tokens", human,
		map[string]string{"name": "self@thinkpad"}), http.StatusCreated)
	me := readBody(t, bearerJSON(t, base, "GET", "/api/v1/me", self.Token, nil))
	if !strings.Contains(me, `"jack"`) {
		t.Fatalf("/me with self token = %s", me)
	}
}

func TestAPIAgentValidation(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	human := mintToken(t, client, base, csrf)

	mk := func(name string) *http.Response {
		return bearerJSON(t, base, "POST", "/api/v1/agents", human, map[string]string{"name": name})
	}

	// First create succeeds; the duplicate is a 409.
	mk("dup").Body.Close()
	if r := mk("dup"); r.StatusCode != http.StatusConflict {
		r.Body.Close()
		t.Fatalf("duplicate name: want 409, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}

	// An invalid local part is a 400.
	if r := mk("Bad Name"); r.StatusCode != http.StatusBadRequest {
		r.Body.Close()
		t.Fatalf("invalid name: want 400, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}

	// Minting for an agent you don't own (here: a bogus id) is a 404.
	if r := bearerJSON(t, base, "POST", "/api/v1/agents/zzzzzzzz/tokens", human, map[string]string{"name": "x"}); r.StatusCode != http.StatusNotFound {
		r.Body.Close()
		t.Fatalf("mint for unowned agent: want 404, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}

	// Agents can't own agents: an agent token creating an agent is a 403.
	ag := decodeJSON[agentResp](t, mk("nested-owner"), http.StatusCreated)
	tok := decodeJSON[tokenResp](t, bearerJSON(t, base, "POST", "/api/v1/agents/"+ag.ID+"/tokens", human,
		map[string]string{"name": "t"}), http.StatusCreated)
	if r := bearerJSON(t, base, "POST", "/api/v1/agents", tok.Token, map[string]string{"name": "grandchild"}); r.StatusCode != http.StatusForbidden {
		r.Body.Close()
		t.Fatalf("agent owning agent: want 403, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
}
