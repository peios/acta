package integration

import (
	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/config"
	"acta/internal/httpapi"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestAmendMCPSessionGrants(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	agent := agentAccount(t, f, "editable-access", nil)
	o, tokens := agentOAuth(t, f, agent.ID)
	sessions, e := manager(f).AgentSessions(ctx, f.token, agent.ID)
	must(t, e)
	session := sessions[0]
	old := []string{auth.MCPIdentityGrant}
	next := []string{auth.MCPIdentityGrant, auth.MCPMemoriesRead}
	must(t, f.security.AmendMCPGrants(ctx, f.token, agent.ID, session.ID, old, next))
	_, grants, e := f.security.MCPAccount(ctx, tokens.AccessToken, mcpResource)
	must(t, e)
	if !slices.Contains(grants, auth.MCPMemoriesRead) {
		t.Fatal("old access token did not gain access", grants)
	}
	refreshed, e := o.refresh(tokens.RefreshToken)
	must(t, e)
	_, grants, e = f.security.MCPAccount(ctx, refreshed.AccessToken, mcpResource)
	must(t, e)
	if !slices.Contains(grants, auth.MCPMemoriesRead) {
		t.Fatal("refresh lost amended access")
	}
	after, e := manager(f).AgentSessions(ctx, f.token, agent.ID)
	must(t, e)
	if after[0].ID != session.ID || !after[0].ExpiresAt.Equal(session.ExpiresAt) || !after[0].CreatedAt.Equal(session.CreatedAt) {
		t.Fatal("grant edit replaced or extended session")
	}
	if e = f.security.AmendMCPGrants(ctx, f.token, agent.ID, session.ID, old, old); !errors.Is(e, auth.ErrSessionGrantsChanged) {
		t.Fatal("stale write", e)
	}
	if e = f.security.AmendMCPGrants(ctx, f.token, agent.ID, session.ID, next, []string{"admin"}); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("unknown grant accepted", e)
	}
	_, other := permissionMember(t, f, "access-outsider")
	if e = f.security.AmendMCPGrants(ctx, other.token, agent.ID, session.ID, next, old); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("other human amended agent", e)
	}
	if e = f.security.AmendMCPGrants(ctx, other.token, "", session.ID, next, old); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("other human amended foreign session", e)
	}
	cli := agentCLI(t, f, agent.ID)
	if e = f.security.AmendMCPGrants(ctx, cli, agent.ID, session.ID, next, old); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("agent amended own access", e)
	}
	view, e := f.security.View(ctx, f.token)
	must(t, e)
	for _, s := range view.Sessions {
		if s.Current {
			if e = f.security.AmendMCPGrants(ctx, f.token, "", s.ID, nil, next); !errors.Is(e, auth.ErrNotFound) {
				t.Fatal("browser converted to MCP", e)
			}
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, value := range [][]string{old, {auth.MCPMemoriesRead}} {
		wg.Add(1)
		go func(value []string) {
			defer wg.Done()
			results <- f.security.AmendMCPGrants(ctx, f.token, agent.ID, session.ID, next, value)
		}(value)
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for e := range results {
		if e == nil {
			wins++
		} else if errors.Is(e, auth.ErrSessionGrantsChanged) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal(wins, conflicts)
	}
	sessions, e = manager(f).AgentSessions(ctx, f.token, agent.ID)
	must(t, e)
	for _, s := range sessions {
		if s.ID == session.ID {
			must(t, f.security.AmendMCPGrants(ctx, f.token, agent.ID, session.ID, s.Tools, nil))
		}
	}
	_, grants, e = f.security.MCPAccount(ctx, refreshed.AccessToken, mcpResource)
	must(t, e)
	if len(grants) != 0 {
		t.Fatal("empty access not enforced", grants)
	}
	must(t, manager(f).RevokeAgent(ctx, f.token, agent.ID, session.ID))
	if e = f.security.AmendMCPGrants(ctx, f.token, agent.ID, session.ID, nil, next); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("revoked session recreated", e)
	}
}

func TestAmendedGrantsLiveMCPAndHTTP(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer srv.Close()
	cfg, e := config.Parse(srv.URL, "")
	must(t, e)
	handler = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	o := oauthFromAccount(t, f, srv.URL+"/mcp")
	o.approve(t)
	tokens := o.exchange(t)
	client, e := mcp.NewClient(&mcp.Implementation{Name: "Mutable grants test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{tokens.AccessToken}}}, nil)
	must(t, e)
	defer client.Close()
	view, e := f.security.View(ctx, f.token)
	must(t, e)
	id := ""
	for _, s := range view.Sessions {
		if s.Kind == "mcp" {
			id = s.ID
		}
	}
	// Exercise the browser API with its normal origin and cookie, not a token swap.
	amend := func(previous, grants string) int {
		r := httptest.NewRequest("POST", srv.URL+"/api/security/sessions/"+id+"/grants", strings.NewReader(`{"previous_grants":`+previous+`,"tool_grants":`+grants+`}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", srv.URL)
		r.AddCookie(&http.Cookie{Name: "acta_session", Value: f.token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Log(w.Body.String())
		}
		return w.Code
	}
	list, e := client.ListTools(ctx, nil)
	must(t, e)
	if len(list.Tools) != 2 {
		t.Fatal(list)
	}
	if status := amend(`["identity.read"]`, `["identity.read","memories.read"]`); status != 200 {
		t.Fatal(status)
	}
	list, e = client.ListTools(ctx, nil)
	must(t, e)
	if len(list.Tools) != 4 {
		t.Fatal("same connection did not discover new tools", list)
	}
	result, e := client.CallTool(ctx, &mcp.CallToolParams{Name: "memory_recall", Arguments: map[string]any{"workspace": nil}})
	must(t, e)
	if result.IsError {
		t.Fatal(result)
	}
	if status := amend(`["identity.read","memories.read"]`, `["identity.read"]`); status != 200 {
		t.Fatal(status)
	}
	result, e = client.CallTool(ctx, &mcp.CallToolParams{Name: "memory_recall", Arguments: map[string]any{"workspace": nil}})
	if e == nil && !result.IsError {
		t.Fatal("removed cached tool remained callable")
	}
}
