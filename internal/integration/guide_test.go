package integration

import (
	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/config"
	"acta/internal/guide"
	"acta/internal/httpapi"
	"acta/learn"
	"encoding/json"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestGuidePreferencesOwnershipAndRevisions(t *testing.T) {
	root := securityDatabase(t)
	ctx := t.Context()
	owner, f := permissionMember(t, root, "guide-owner")
	_, other := permissionMember(t, root, "guide-other")
	m := manager(f)
	initial, e := m.GuidePreferences(ctx, f.token)
	must(t, e)
	if initial.Site.CanWrite || !initial.User.CanWrite || initial.User.Revision != 0 {
		t.Fatal(initial)
	}
	site, e := manager(root).SaveGuidePreferences(ctx, root.token, guide.Save{Scope: "site", Content: "Global policy"})
	must(t, e)
	if _, e = m.SaveGuidePreferences(ctx, f.token, guide.Save{Scope: "site", Content: "Bad"}); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal(e)
	}
	// Both first creation and subsequent updates reject stale writers.
	revision := int64(0)
	for range 2 {
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, e := m.SaveGuidePreferences(ctx, f.token, guide.Save{Scope: "user", Content: "Owner policy", Revision: revision})
				results <- e
			}()
		}
		wg.Wait()
		close(results)
		successes, conflicts := 0, 0
		for e := range results {
			if e == nil {
				successes++
			} else if errors.Is(e, guide.ErrConflict) {
				conflicts++
			} else {
				t.Fatal(e)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatal(successes, conflicts)
		}
		revision++
	}
	a := agentAccount(t, f, "guide-agent", nil)
	token := agentCLI(t, f, a.ID)
	got, e := m.GuidePreferences(ctx, token)
	must(t, e)
	if got.Site.Content != site.Content || got.User.Content != "Owner policy" || got.Site.CanWrite || got.User.CanWrite {
		t.Fatal(got)
	}
	for _, scope := range []string{"site", "user"} {
		if _, e = m.SaveGuidePreferences(ctx, token, guide.Save{Scope: scope, Content: "Bad", Revision: revision}); !errors.Is(e, auth.ErrForbidden) {
			t.Fatal(e)
		}
	}
	got, e = manager(other).GuidePreferences(ctx, other.token)
	must(t, e)
	if got.User.Content != "" || got.Site.Content != site.Content {
		t.Fatal("cross-user policy", got)
	}
	grantPermissions(t, root, owner.ID, []string{accounts.WriteSiteGuide}, false)
	if _, e = m.AgentPermissions(ctx, f.token, a.ID, a.PermissionsVersion, []string{accounts.WriteSiteGuide}); e == nil {
		t.Fatal("site policy editing delegated to agent")
	}

	_, e = m.SaveGuidePreferences(ctx, f.token, guide.Save{Scope: "site", Content: "Updated global policy", Revision: site.Revision})
	must(t, e)
	grantPermissions(t, root, owner.ID, nil, false)
	if _, e = m.SaveGuidePreferences(ctx, f.token, guide.Save{Scope: "site", Content: "Revoked", Revision: 2}); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal(e)
	}
	cleared, e := m.SaveGuidePreferences(ctx, f.token, guide.Save{Scope: "user", Revision: revision})
	must(t, e)
	if cleared.Content != "" || cleared.Revision != revision+1 {
		t.Fatal(cleared)
	}
	if _, e = m.SaveGuidePreferences(ctx, f.token, guide.Save{Scope: "user", Content: "Resurrection"}); !errors.Is(e, guide.ErrConflict) {
		t.Fatal(e)
	}
}

func TestGuideHTTPAndMCPShareOwnerPolicy(t *testing.T) {
	root := securityDatabase(t)
	ctx := t.Context()
	_, f := permissionMember(t, root, "guide-owner")
	a := agentAccount(t, f, "guide-reader", nil)
	m := manager(f)
	_, e := manager(root).SaveGuidePreferences(ctx, root.token, guide.Save{Scope: "site", Content: "Site essential policy"})
	must(t, e)
	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer srv.Close()
	cfg, e := config.Parse(srv.URL, "")
	must(t, e)
	handler = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), m, cfg)
	request := func(method, path, body, token, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if token != "" {
			req.AddCookie(&http.Cookie{Name: "acta_session", Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	for _, origin := range []string{"", "https://elsewhere.invalid"} {
		w := request("POST", "/api/guide/preferences", `{"scope":"user","content":"Bad","revision":0}`, f.token, origin)
		if w.Code != 403 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request("GET", "/api/guide", "", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := request("POST", "/api/guide/preferences", `{"scope":"user","content":"Owner essential policy","revision":0}`, f.token, srv.URL)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("POST", "/api/guide/preferences", `{"scope":"user","content":"Stale","revision":0}`, f.token, srv.URL)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("POST", "/api/guide/preferences", `{"scope":"site","content":"Bad","revision":1}`, f.token, srv.URL)
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	o := oauthFromAccount(t, f, srv.URL+"/mcp")
	redirect, e := f.security.ApproveOAuthTools(ctx, f.token, o.request, f.binding, true, srv.URL, a.ID, []string{})
	must(t, e)
	u, e := url.Parse(redirect)
	must(t, e)
	o.code = u.Query().Get("code")
	tokens := o.exchange(t)
	session, e := mcp.NewClient(&mcp.Implementation{Name: "Guide test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{tokens.AccessToken}}}, nil)
	must(t, e)
	defer session.Close()
	for i := range 2 {
		if i == 1 {
			_, e = m.SaveGuidePreferences(ctx, f.token, guide.Save{Scope: "user", Content: "New owner policy", Revision: 1})
			must(t, e)
		}
		w = request("GET", "/api/guide", "", f.token, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var doc guide.Document
		must(t, json.Unmarshal(w.Body.Bytes(), &doc))
		result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "acta_guide", Arguments: map[string]any{}})
		must(t, e)
		if result.IsError || len(result.Content) != 1 {
			t.Fatal(result)
		}
		text, ok := result.Content[0].(*mcp.TextContent)
		if !ok || text.Text != doc.Markdown || doc.BuiltIn != learn.AgentGuide || !strings.Contains(text.Text, "Site essential policy") {
			t.Fatal("preview differs from MCP")
		}
		if i == 1 && !strings.Contains(text.Text, "New owner policy") {
			t.Fatal("cached stale policy")
		}
	}
	record, e := f.store.ReadOAuthToken(ctx, auth.Digest(tokens.AccessToken))
	must(t, e)
	must(t, m.RevokeAgent(ctx, f.token, a.ID, record.SessionID))
	if result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "acta_guide", Arguments: map[string]any{}}); e == nil && !result.IsError {
		t.Fatal("revoked guide remained accessible")
	}
}
