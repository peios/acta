package integration

import (
	"acta2/internal/accounts"
	"acta2/internal/auth"
	"acta2/internal/comments"
	"acta2/internal/config"
	"acta2/internal/httpapi"
	"acta2/internal/tasks"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func searchTasks(t *testing.T, f securityFixture, q tasks.SearchQuery) tasks.SearchPage {
	t.Helper()
	p, e := manager(f).SearchTasks(t.Context(), f.token, q)
	must(t, e)
	return p
}

func TestTaskSearchContentRankingPagingAndAccess(t *testing.T) {
	root := securityDatabase(t)
	ctx := t.Context()
	owner, f, w := workspaceOwner(t, root, "search")
	create := func(title, desc, parent string) tasks.Task {
		v, e := manager(root).CreateTask(ctx, root.token, w.ID, tasks.Create{Title: title, Description: desc, ParentID: parent})
		must(t, e)
		return v
	}
	create("IT", "", "")
	if p := searchTasks(t, f, tasks.SearchQuery{Query: "IT"}); len(p.Tasks) != 1 {
		t.Fatal("short title term", p)
	}
	parent := create("Parent work", "", "")
	child := create("Nested searchable citadel", "", parent.ID)
	desc := create("Background work", "Investigating citadels and their design", "")
	commentTask := create("Discussion", "", "")
	c := postComment(t, root, commentTask.ID, "Citadel answer", "")
	postComment(t, root, commentTask.ID, "Another citadel answer", c.ID)
	p := searchTasks(t, f, tasks.SearchQuery{Query: "citadel"})
	if len(p.Tasks) != 3 || p.Tasks[0].ID != child.ID || p.Tasks[1].ID != desc.ID || p.Tasks[2].ID != commentTask.ID {
		t.Fatal(p)
	}
	if len(p.Tasks[0].Ancestors) != 1 || p.Tasks[0].Ancestors[0].ID != parent.ID || p.Tasks[2].CommentID == "" {
		t.Fatal(p)
	}
	marked := false
	for _, part := range p.Tasks[1].Excerpt {
		marked = marked || part.Match
	}
	if !marked {
		t.Fatal("missing excerpt highlight", p)
	}
	if p = searchTasks(t, f, tasks.SearchQuery{Query: "searchab"}); len(p.Tasks) != 1 || p.Tasks[0].ID != child.ID {
		t.Fatal("title prefix", p)
	}
	if p = searchTasks(t, f, tasks.SearchQuery{Query: child.Reference}); len(p.Tasks) == 0 || p.Tasks[0].ID != child.ID {
		t.Fatal("reference", p)
	}
	cfg, e := manager(root).TaskConfig(ctx, root.token, w.ID)
	must(t, e)
	must(t, manager(root).TaskPrefix(ctx, root.token, w.ID, "FIND", cfg.Version))
	if p = searchTasks(t, f, tasks.SearchQuery{Query: child.Reference}); len(p.Tasks) == 0 || p.Tasks[0].Reference != fmt.Sprintf("FIND-%d", child.Number) {
		t.Fatal("old reference", p)
	}
	desc, e = patchTask(t, root, desc, "description", "Changed body")
	must(t, e)
	if p = searchTasks(t, f, tasks.SearchQuery{Query: "citadel"}); len(p.Tasks) != 2 {
		t.Fatal("stale index", p)
	}
	_, e = manager(root).UpdateComment(ctx, root.token, commentTask.ID, c.ID, comments.Update{Version: 1, Delete: true})
	must(t, e)
	if p = searchTasks(t, f, tasks.SearchQuery{Query: "Citadel answer"}); len(p.Tasks) != 1 || p.Tasks[0].CommentID == c.ID {
		t.Fatal("deleted comment matched", p)
	}
	for i := 0; i < 31; i++ {
		create(fmt.Sprintf("Pagination quarry %02d", i), "", parent.ID)
	}
	q := tasks.SearchQuery{Query: "quarry", Workspace: w.Slug}
	p = searchTasks(t, f, q)
	if len(p.Tasks) != 25 || !p.More {
		t.Fatal(p)
	}
	seen := map[string]bool{}
	for _, r := range p.Tasks {
		seen[r.ID] = true
	}
	q.Cursor = p.Cursor
	p = searchTasks(t, f, q)
	if len(p.Tasks) != 6 || p.More {
		t.Fatal(p)
	}
	for _, r := range p.Tasks {
		if seen[r.ID] {
			t.Fatal("duplicate page", r)
		}
	}
	q.Query = "other"
	_, e = manager(f).SearchTasks(ctx, f.token, q)
	var field *accounts.FieldError
	if !errors.As(e, &field) {
		t.Fatal("cursor reuse accepted", e)
	}
	_, outsider := permissionMember(t, root, "search-outsider")
	if p = searchTasks(t, outsider, tasks.SearchQuery{Query: "quarry"}); len(p.Tasks) != 0 {
		t.Fatal("workspace leaked", p)
	}
	_, e = manager(outsider).SearchTasks(ctx, outsider.token, tasks.SearchQuery{Query: "quarry", Workspace: w.ID})
	if !errors.Is(e, auth.ErrNotFound) {
		t.Fatal(e)
	}
	agent := agentAccount(t, root, "searcher", nil)
	token := agentCLI(t, root, agent.ID)
	af := root
	af.token = token
	settings, e := manager(root).AgentWorkspaces(ctx, root.token, agent.ID)
	must(t, e)
	must(t, manager(root).SetAgentWorkspaces(ctx, root.token, agent.ID, settings.Version, false, []string{}))
	if p = searchTasks(t, af, tasks.SearchQuery{Query: "quarry"}); len(p.Tasks) != 0 {
		t.Fatal("agent selection leaked", p)
	}
	w = readWorkspace(t, root, w.ID)
	must(t, manager(root).WorkspaceMembership(ctx, root.token, w.ID, owner.ID, w.Version, false))
	if p = searchTasks(t, f, tasks.SearchQuery{Query: "quarry"}); len(p.Tasks) != 0 {
		t.Fatal("revoked access leaked", p)
	}
}

func TestTaskSearchHTTPAndCLI(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	w, e := manager(f).CreateWorkspace(ctx, f.token, "Search API", "search-api", "")
	must(t, e)
	v, e := manager(f).CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Needle searchable"})
	must(t, e)
	var h http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	defer srv.Close()
	cfg, e := config.Parse(srv.URL, "")
	must(t, e)
	h = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	token := agentCLI(t, f, agentAccount(t, f, "search-cli", nil).ID)
	var p tasks.SearchPage
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, token, "", "task", "search", "Needle", "-w", w.Slug), &p))
	if len(p.Tasks) != 1 || p.Tasks[0].ID != v.ID {
		t.Fatal(p)
	}
	localCfg, e := config.Parse("http://localhost:8081", "")
	must(t, e)
	c := client{t, httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), localCfg), map[string]*http.Cookie{"acta_session": {Name: "acta_session", Value: f.token}}}
	c.request("GET", "tasks/search?q=a", "", 422)
	c.request("GET", "tasks/search?q=Needle&cursor=bad", "", 422)
	if !strings.Contains(c.request("GET", "tasks/search?q=Needle", "", 200).Body.String(), v.ID) {
		t.Fatal("missing HTTP result")
	}
}
