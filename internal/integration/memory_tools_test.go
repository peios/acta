package integration

import (
	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/config"
	"acta/internal/httpapi"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMemoryMCPScopesAndGrants(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	agent := agentAccount(t, f, "memory-mcp", nil)
	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer srv.Close()
	cfg, e := config.Parse(srv.URL, "")
	must(t, e)
	handler = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	connect := func(grants []string) *mcp.ClientSession {
		t.Helper()
		o := oauthFromAccount(t, f, srv.URL+"/mcp")
		redirect, e := f.security.ApproveOAuthTools(ctx, f.token, o.request, f.binding, true, srv.URL, agent.ID, grants)
		must(t, e)
		u, e := url.Parse(redirect)
		must(t, e)
		o.code = u.Query().Get("code")
		tokens := o.exchange(t)
		session, e := mcp.NewClient(&mcp.Implementation{Name: "Memory test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{tokens.AccessToken}}}, nil)
		must(t, e)
		t.Cleanup(func() { session.Close() })
		return session
	}
	old := connect([]string{auth.MCPIdentityGrant})
	list, e := old.ListTools(ctx, nil)
	must(t, e)
	if len(list.Tools) != 2 {
		t.Fatal("existing grants gained memory tools")
	}
	session := connect([]string{auth.MCPMemoriesRead, auth.MCPMemoriesWrite})
	list, e = session.ListTools(ctx, nil)
	must(t, e)
	if len(list.Tools) != 5 {
		t.Fatal(list)
	}
	for _, tool := range list.Tools {
		if tool.Name == "memory_save" && (!strings.Contains(tool.Description, "learning something yourself") || !strings.Contains(tool.Description, "own.memories.write")) {
			t.Fatal("scope guidance absent")
		}
	}
	args := map[string]any{"scope": "agent", "key": "workflow", "summary": "Workflow convention", "content": "# Test knowledge", "revision": 0}
	result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "memory_save", Arguments: args})
	must(t, e)
	if result.IsError {
		t.Fatal(result)
	}
	var envelope struct {
		Data struct {
			ID       string `json:"id"`
			ScopeID  string `json:"scope_id"`
			Revision int64  `json:"revision"`
		}
	}
	raw, _ := json.Marshal(result.StructuredContent)
	must(t, json.Unmarshal(raw, &envelope))
	if envelope.Data.ID == "" || envelope.Data.ScopeID != agent.ID {
		t.Fatal(string(raw))
	}
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "memory_recall", Arguments: map[string]any{"workspace": nil}})
	must(t, e)
	if result.IsError {
		t.Fatal(result)
	}
	raw, _ = json.Marshal(result.StructuredContent)
	if strings.Contains(string(raw), "# Test knowledge") || !strings.Contains(string(raw), "workflow") {
		t.Fatal(string(raw))
	}
	var index struct {
		Data struct {
			Memories []map[string]any `json:"memories"`
		} `json:"data"`
	}
	must(t, json.Unmarshal(raw, &index))
	if len(index.Data.Memories) != 1 {
		t.Fatal(string(raw))
	}
	row := index.Data.Memories[0]
	if len(row) != 5 || row["id"] != envelope.Data.ID || row["scope"] != "agent" || row["key"] != "workflow" || row["revision"] != float64(1) || row["summary"] != "Workflow convention" {
		t.Fatal(row)
	}
	// Invalid selector combinations are validation failures, not permission failures.
	args["workspace"] = "some-workspace"
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "memory_save", Arguments: args})
	must(t, e)
	assertMemoryError(t, result, "validation")
	delete(args, "workspace")
	args["scope"] = "site"
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "memory_save", Arguments: args})
	must(t, e)
	assertMemoryError(t, result, "forbidden")
	args["scope"] = "user"
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "memory_save", Arguments: args})
	must(t, e)
	assertMemoryError(t, result, "forbidden")
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "memory_delete", Arguments: map[string]any{"id": envelope.Data.ID, "revision": envelope.Data.Revision}})
	must(t, e)
	if result.IsError {
		t.Fatal(result)
	}
	read := connect([]string{auth.MCPMemoriesRead})
	list, e = read.ListTools(ctx, nil)
	must(t, e)
	if len(list.Tools) != 3 {
		t.Fatal("read only got writes", list)
	}
	// Recall pagination across mixed scopes, including duplicate keys at the boundary.
	m := manager(f)
	w, e := m.CreateWorkspace(ctx, f.token, "Memory pages", "memory-pages", "")
	must(t, e)
	for i := range 51 {
		_, e = m.SaveMemory(ctx, f.token, memoryInput("user", fmt.Sprintf("recall-page-%02d", i)))
		must(t, e)
	}
	in := memoryInput("workspace", "recall-page-49")
	in.Workspace = &w.ID
	_, e = m.SaveMemory(ctx, f.token, in)
	must(t, e)
	recall := func(workspace *string, query, cursor string) *mcp.CallToolResult {
		t.Helper()
		r, e := read.CallTool(ctx, &mcp.CallToolParams{Name: "memory_recall", Arguments: map[string]any{"workspace": workspace, "query": query, "cursor": cursor}})
		must(t, e)
		return r
	}
	type compactPage struct {
		Data struct {
			Memories []struct{ ID, Key, Scope string }
			Cursor   string
		}
	}
	decodePage := func(r *mcp.CallToolResult) compactPage {
		t.Helper()
		if r.IsError {
			t.Fatal(r)
		}
		raw, e := json.Marshal(r.StructuredContent)
		must(t, e)
		var p compactPage
		must(t, json.Unmarshal(raw, &p))
		return p
	}
	first := decodePage(recall(&w.ID, "recall-page-", ""))
	if len(first.Data.Memories) != 50 || first.Data.Cursor == "" {
		t.Fatal(first)
	}
	second := decodePage(recall(&w.ID, "recall-page-", first.Data.Cursor))
	if len(second.Data.Memories) != 2 || second.Data.Cursor != "" {
		t.Fatal(second)
	}
	seen := map[string]bool{}
	matching := 0
	for _, entry := range append(first.Data.Memories, second.Data.Memories...) {
		if seen[entry.ID] {
			t.Fatal("duplicate page entry", entry)
		}
		seen[entry.ID] = true
		if entry.Key == "recall-page-49" {
			matching++
		}
	}
	if len(seen) != 52 || matching != 2 {
		t.Fatal(len(seen), matching)
	}
	assertMemoryError(t, recall(nil, "recall-page-", first.Data.Cursor), "validation")
	assertMemoryError(t, recall(&w.ID, "other-query", first.Data.Cursor), "validation")
	assertMemoryError(t, recall(&w.ID, "recall-page-", "invalid-cursor"), "validation")
}

func assertMemoryError(t *testing.T, result *mcp.CallToolResult, code string) {
	t.Helper()
	raw, e := json.Marshal(result.StructuredContent)
	must(t, e)
	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	must(t, json.Unmarshal(raw, &out))
	if !result.IsError || out.Error.Code != code {
		t.Fatalf("want %s: %s", code, raw)
	}
}
