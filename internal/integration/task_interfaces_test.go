package integration

import (
	"acta/internal/accounts"
	"acta/internal/activity"
	"acta/internal/auth"
	"acta/internal/cli"
	apiclient "acta/internal/client"
	"acta/internal/config"
	"acta/internal/httpapi"
	"acta/internal/tasks"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskCLIAndMCPShareAuthority(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	w, e := manager(f).CreateWorkspace(ctx, f.token, "Interfaces", "interfaces", "")
	must(t, e)
	agent := agentAccount(t, f, "worker", nil)
	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer srv.Close()
	cfg, e := config.Parse(srv.URL, "")
	must(t, e)
	handler = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	cliToken := agentCLI(t, f, agent.ID)
	c := apiclient.New(srv.URL, cliToken)
	var v tasks.Task
	must(t, c.Call(ctx, "POST", "workspaces/"+w.ID+"/tasks", tasks.Create{Title: "CLI task"}, &v))
	if v.Title != "CLI task" {
		t.Fatal(v)
	}
	// Exercise actual Cobra parsing, profile selection, stdin and JSON rendering.
	var cliCreated tasks.Task
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, cliToken, "# From stdin\n", "task", "create", "-w", "interfaces", "--title", "Terminal task", "--priority", "high", "--type", "bug", "--size", "s", "--description-file", "-"), &cliCreated))
	if cliCreated.Priority != "high" || cliCreated.Type != "bug" || cliCreated.Size != "s" {
		t.Fatal("CLI metadata lost", cliCreated)
	}
	if cliCreated.Description != "# From stdin\n" || cliCreated.WorkspaceID != w.ID {
		t.Fatal(cliCreated)
	}
	var cliCleared tasks.Task
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, cliToken, "", "task", "edit", cliCreated.Reference, "--field", "description", "--version", "1", "--clear"), &cliCleared))
	if cliCleared.Description != "" || cliCleared.Versions["description"] != 2 {
		t.Fatal(cliCleared)
	}
	var cliHistory activity.Page
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, cliToken, "", "task", "activity", cliCreated.Reference), &cliHistory))
	if len(cliHistory.Entries) != 2 {
		t.Fatal("CLI activity missing", cliHistory)
	}
	var activityReads int
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM activity_reads`).Scan(&activityReads))
	if activityReads != 0 {
		t.Fatal("CLI activity marked read")
	}

	o := oauthFromAccount(t, f, srv.URL+"/mcp")
	redirect, e := f.security.ApproveOAuthTools(ctx, f.token, o.request, f.binding, true, srv.URL, agent.ID, []string{auth.MCPIdentityGrant, auth.MCPTasksRead, auth.MCPTasksWrite})
	must(t, e)
	u, e := url.Parse(redirect)
	must(t, e)
	o.code = u.Query().Get("code")
	tokens := o.exchange(t)
	session, e := mcp.NewClient(&mcp.Implementation{Name: "Task test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{tokens.AccessToken}}}, nil)
	must(t, e)
	defer session.Close()
	catalogue, e := session.ListTools(ctx, nil)
	must(t, e)
	if len(catalogue.Tools) != 25 {
		t.Fatalf("expected guide, identity, seventeen task tools and six document tools: %d", len(catalogue.Tools))
	}
	for _, tool := range catalogue.Tools {
		// The guide deliberately returns Markdown text, not a JSON output envelope.
		if tool.Description == "" || tool.InputSchema == nil || (tool.Name != "acta_guide" && tool.OutputSchema == nil) {
			t.Fatalf("missing contract for %s", tool.Name)
		}
	}
	searchResult, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "task_search", Arguments: map[string]any{"query": "Terminal", "workspace": w.ID}})
	must(t, e)
	if searchResult.IsError {
		t.Fatal(searchResult)
	}
	var searchEnvelope struct {
		Data tasks.SearchPage `json:"data"`
	}
	decodeTool(t, searchResult, &searchEnvelope)
	if len(searchEnvelope.Data.Tasks) != 1 || searchEnvelope.Data.Tasks[0].ID != cliCreated.ID {
		t.Fatal(searchEnvelope)
	}

	var archivedTask tasks.Task
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, cliToken, "", "task", "archive", cliCreated.Reference, "--version", "1"), &archivedTask))
	if !archivedTask.Archived {
		t.Fatal(archivedTask)
	}
	var archivedList tasks.SummaryPage
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, cliToken, "", "task", "list", "-w", w.ID, "--archived"), &archivedList))
	if len(archivedList.Tasks) != 1 || archivedList.Tasks[0].ID != cliCreated.ID {
		t.Fatal(archivedList)
	}
	archiveSearch, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "task_search", Arguments: map[string]any{"query": "Terminal", "include_archived": true}})
	must(t, e)
	decodeTool(t, archiveSearch, &searchEnvelope)
	if len(searchEnvelope.Data.Tasks) != 1 || !searchEnvelope.Data.Tasks[0].Archived {
		t.Fatal(searchEnvelope)
	}
	restored, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "task_restore", Arguments: map[string]any{"task": cliCreated.ID, "version": 2}})
	must(t, e)
	if restored.IsError {
		t.Fatal(restored)
	}
	archived, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "task_archive", Arguments: map[string]any{"task": cliCreated.ID, "version": 3}})
	must(t, e)
	if archived.IsError {
		t.Fatal(archived)
	}
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, cliToken, "", "task", "restore", cliCreated.Reference, "--version", "4"), &archivedTask))
	if archivedTask.Archived {
		t.Fatal(archivedTask)
	}
	// Comments use the same actual CLI parser, HTTP routes and MCP authority.
	var cliComment activity.Entry
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, cliToken, "Comment from stdin", "task", "comment", "add", cliCreated.Reference, "--body-file", "-"), &cliComment))
	var editedComment activity.Entry
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, cliToken, "", "task", "comment", "edit", cliCreated.Reference, cliComment.ID, "--body", "Edited from CLI", "--version", "1"), &editedComment))
	if editedComment.Comment.Version != 2 || editedComment.Actor.ID != agent.ID {
		t.Fatal(editedComment)
	}
	callComment := func(name string, args map[string]any) activity.Entry {
		t.Helper()
		result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		must(t, e)
		if result.IsError {
			t.Fatal(result)
		}
		var out struct {
			Data activity.Entry `json:"data"`
		}
		decodeTool(t, result, &out)
		return out.Data
	}
	postArgs := map[string]any{"task": cliCreated.ID, "reply_to": cliComment.ID, "body": "MCP reply", "request_id": uuid.NewString()}
	mcpReply := callComment("comment_create", postArgs)
	if callComment("comment_create", postArgs).ID != mcpReply.ID {
		t.Fatal("MCP duplicate retry")
	}
	callComment("comment_update", map[string]any{"task": cliCreated.ID, "comment": mcpReply.ID, "body": "MCP edited reply", "version": 1})
	if callComment("comment_get", map[string]any{"task": cliCreated.ID, "comment": mcpReply.ID}).Comment.Version != 2 {
		t.Fatal("MCP edit missing")
	}
	threadResult, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "comment_replies", Arguments: map[string]any{"task": cliCreated.ID, "comment": cliComment.ID}})
	must(t, e)
	var threadEnvelope struct {
		Data activity.Page `json:"data"`
	}
	decodeTool(t, threadResult, &threadEnvelope)
	if len(threadEnvelope.Data.Entries) != 1 || threadEnvelope.Data.Entries[0].ID != mcpReply.ID {
		t.Fatal(threadEnvelope)
	}
	if !callComment("comment_delete", map[string]any{"task": cliCreated.ID, "comment": mcpReply.ID, "version": 2}).Comment.Deleted {
		t.Fatal("MCP deletion missing")
	}
	var cliDeleted activity.Entry
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, cliToken, "", "task", "comment", "delete", cliCreated.Reference, cliComment.ID, "--version", "2"), &cliDeleted))
	if !cliDeleted.Comment.Deleted || cliDeleted.ReplyCount != 1 {
		t.Fatal(cliDeleted)
	}
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM activity_reads`).Scan(&activityReads))
	if activityReads != 0 {
		t.Fatal("CLI/MCP comment retrieval marked read")
	}

	result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "task_create", Arguments: map[string]any{"workspace": w.ID, "title": "MCP child", "priority": "urgent", "type": "feature", "size": "xl", "parent": v.Reference}})
	must(t, e)
	if result.IsError {
		t.Fatal(result)
	}
	raw, e := json.Marshal(result.StructuredContent)
	must(t, e)
	var envelope struct {
		Data tasks.Task `json:"data"`
	}
	must(t, json.Unmarshal(raw, &envelope))
	child := envelope.Data
	if child.Priority != "urgent" || child.Type != "feature" || child.Size != "xl" {
		t.Fatal("MCP metadata lost", child)
	}
	metadataResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "task_update", Arguments: map[string]any{"task": child.ID, "field": "size", "version": 1, "text": "m"}})
	must(t, err)
	if metadataResult.IsError {
		t.Fatal(metadataResult)
	}
	var propertyGroups []tasks.Group
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, cliToken, "", "task", "groups", "-w", w.ID, "--group", "priority"), &propertyGroups))
	if len(propertyGroups) != 5 {
		t.Fatal(propertyGroups)
	}
	var metadataPage tasks.SummaryPage
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, cliToken, "", "task", "list", "-w", w.ID, "--priority", "high", "--type", "bug", "--size", "s"), &metadataPage))
	if metadataPage.Total != 1 || metadataPage.Tasks[0].ID != cliCreated.ID {
		t.Fatal("CLI filters", metadataPage)
	}
	metadataResult, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "tasks_list", Arguments: map[string]any{"workspace": w.ID, "parent": v.ID, "priorities": []string{"urgent"}, "types": []string{"feature"}, "sizes": []string{"m"}}})
	must(t, err)
	var metadataEnvelope struct {
		Data tasks.SummaryPage `json:"data"`
	}
	decodeTool(t, metadataResult, &metadataEnvelope)
	if metadataResult.IsError || metadataEnvelope.Data.Total != 1 || metadataEnvelope.Data.Tasks[0].Size != "m" {
		t.Fatal(metadataResult)
	}
	if child.ParentID != v.ID {
		t.Fatal(child)
	}
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "task_update", Arguments: map[string]any{"task": child.ID, "field": "title", "version": 1, "text": "Updated through MCP"}})
	must(t, e)
	if result.IsError {
		t.Fatal(result)
	}
	// A stale edit is structured, carries the current field and can be reconciled.
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "task_update", Arguments: map[string]any{"task": child.Reference, "field": "title", "version": 1, "text": "Stale title"}})
	must(t, e)
	var problem struct {
		Error httpapi.Problem `json:"error"`
	}
	decodeTool(t, result, &problem)
	if !result.IsError || problem.Error.Code != "conflict" || problem.Error.Current == nil || problem.Error.Current.Version != 2 || problem.Error.Current.Value != "Updated through MCP" {
		t.Fatalf("missing structured conflict: %+v", problem)
	}
	// Omission and null never mean clear. A wrong value kind is rejected too.
	for _, args := range []map[string]any{
		{"task": child.ID, "field": "title", "version": 2},
		{"task": child.ID, "field": "description", "version": 1, "text": nil},
		{"task": child.ID, "field": "assignees", "version": 1},
		{"task": child.ID, "field": "description", "version": 1, "assignees": []string{}},
	} {
		r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "task_update", Arguments: args})
		must(t, err)
		if !r.IsError {
			t.Fatal("accepted implicit or wrong edit", args)
		}
	}
	current, e := c.Task(ctx, child.Reference, "")
	must(t, e)
	if current.Title != "Updated through MCP" || current.Versions["title"] != 2 {
		t.Fatal("rejected edit mutated task", current)
	}
	for _, value := range []json.RawMessage{nil, json.RawMessage(`null`)} {
		_, e = c.UpdateTask(ctx, child.ID, tasks.Patch{Field: "description", Version: 1, Value: value})
		var ce *apiclient.Error
		if !errors.As(e, &ce) || ce.Status != 422 || ce.Code != "validation" {
			t.Fatal("HTTP accepted implicit clear", e)
		}
	}
	_, e = c.UpdateTask(ctx, child.ID, tasks.Patch{Field: "title", Version: 1, Value: json.RawMessage(`"Stale CLI title"`)})
	var ce *apiclient.Error
	if !errors.As(e, &ce) || ce.Status != 409 || ce.Code != "conflict" || !strings.Contains(string(ce.Current), `"Updated through MCP"`) {
		t.Fatal("CLI lost conflict details", e)
	}
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "task_update", Arguments: map[string]any{"task": child.ID, "field": "title", "version": 2, "text": "Reconciled"}})
	must(t, e)
	if result.IsError {
		t.Fatal(result)
	}
	// Inspect children, even completed ones, while root search never promotes them.
	config, e := c.TaskConfig(ctx, "interfaces")
	must(t, e)
	_, e = c.UpdateTask(ctx, child.ID, tasks.Patch{Field: "status_id", Version: 1, Value: json.RawMessage(fmt.Sprintf("%q", config.Completed))})
	must(t, e)
	detail, e := c.Task(ctx, v.Reference, "")
	must(t, e)
	if detail.Subtasks.Total != 1 || detail.Subtasks.Tasks[0].Status.ID != config.Completed {
		t.Fatal("inspection omitted completed child", detail)
	}
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "task_get", Arguments: map[string]any{"task": v.Reference}})
	must(t, e)
	var inspected struct {
		Data tasks.Detail `json:"data"`
	}
	decodeTool(t, result, &inspected)
	if inspected.Data.Subtasks.Total != 1 {
		t.Fatal(result)
	}
	page, e := c.Tasks(ctx, "interfaces", tasks.Filter{State: "all", Query: "Reconciled"})
	must(t, e)
	if page.Total != 0 {
		t.Fatal("child promoted into root search", page)
	}
	page, e = c.Tasks(ctx, "interfaces", tasks.Filter{State: "all", Parent: v.Reference, Query: "Reconciled"})
	must(t, e)
	if page.Total != 1 {
		t.Fatal("reference parent lookup failed", page)
	}
	// Lists have predictable bounded pages and exclude the large description body.
	for i := 0; i < 52; i++ {
		_, e = c.CreateTask(ctx, "interfaces", tasks.Create{Title: fmt.Sprintf("Page %02d", i), Description: "private long description"})
		must(t, e)
	}
	filter := map[string]any{"workspace": "interfaces", "query": "Page ", "sort": "title", "direction": "asc"}
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "tasks_list", Arguments: filter})
	must(t, e)
	var listed struct {
		Data tasks.SummaryPage `json:"data"`
	}
	decodeTool(t, result, &listed)
	data, _ := json.Marshal(result.StructuredContent)
	if result.IsError || len(listed.Data.Tasks) != 50 || listed.Data.Total != 52 || !listed.Data.More || listed.Data.Cursor == "" || strings.Contains(string(data), "description") {
		t.Fatal("invalid summary", string(data))
	}
	filter["cursor"] = listed.Data.Cursor
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "tasks_list", Arguments: filter})
	must(t, e)
	decodeTool(t, result, &listed)
	if result.IsError || len(listed.Data.Tasks) != 2 || listed.Data.More || listed.Data.Tasks[0].Title != "Page 50" {
		t.Fatal(listed)
	}
	for name, args := range map[string]map[string]any{
		"task_activity":   {"task": child.ID},
		"workspaces_list": {"query": "Interfaces"},
		"task_statuses":   {"workspace": "interfaces"},
		"task_people":     {"workspace": "interfaces", "query": "worker"},
		"task_groups":     {"workspace": "interfaces", "group": "agents"},
	} {
		r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		must(t, err)
		if r.IsError {
			t.Fatalf("%s: %+v", name, r)
		}
	}
	// Board selection survives the real MCP schema and CLI parser, not just the domain API.
	boardResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "task_create", Arguments: map[string]any{"workspace": w.ID, "title": "Board interface review", "board": "backlog"}})
	must(t, err)
	var boardEnvelope struct {
		Data tasks.Task `json:"data"`
	}
	decodeTool(t, boardResult, &boardEnvelope)
	if boardResult.IsError || boardEnvelope.Data.Board != "backlog" {
		t.Fatal(boardResult)
	}
	var backlogPage tasks.SummaryPage
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, cliToken, "", "task", "list", "-w", w.ID, "--board", "backlog"), &backlogPage))
	if backlogPage.Total != 1 || backlogPage.Tasks[0].ID != boardEnvelope.Data.ID {
		t.Fatal(backlogPage)
	}
	var promoted tasks.Task
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, cliToken, "", "task", "edit", boardEnvelope.Data.ID, "--field", "board", "--version", "1", "--value", "tasks"), &promoted))
	if promoted.Board != "tasks" || promoted.ID != boardEnvelope.Data.ID {
		t.Fatal(promoted)
	}
	boardResult, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "task_update", Arguments: map[string]any{"task": promoted.ID, "field": "board", "text": "backlog", "version": 2}})
	must(t, err)
	decodeTool(t, boardResult, &boardEnvelope)
	if boardResult.IsError || boardEnvelope.Data.Board != "backlog" {
		t.Fatal(boardResult)
	}
	// Explicit assignment replacement and clearing work through the same tool.
	for i, ids := range [][]string{{agent.ID}, {}} {
		r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "task_update", Arguments: map[string]any{"task": v.ID, "field": "assignees", "version": i + 1, "assignees": ids}})
		must(t, err)
		if r.IsError {
			t.Fatal(r)
		}
	}
	// Slug aliases resolve to the same UUID and cannot change the target workspace.
	_, e = manager(f).EditWorkspace(ctx, f.token, w.ID, w.Version, "Interfaces renamed", "interfaces-renamed", "")
	must(t, e)
	for _, slug := range []string{"interfaces", "interfaces-renamed", "INTERFACES-RENAMED"} {
		p, err := c.Tasks(ctx, slug, tasks.Filter{State: "all", Query: "CLI task"})
		must(t, err)
		if p.Total != 1 || p.Tasks[0].WorkspaceID != w.ID {
			t.Fatal("slug resolution", slug, p)
		}
	}
	policy, e := manager(f).AgentWorkspaces(ctx, f.token, agent.ID)
	must(t, e)
	must(t, manager(f).SetAgentWorkspacePolicy(ctx, f.token, agent.ID, w.ID, policy.Version, false, nil))
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "task_create", Arguments: map[string]any{"workspace": w.ID, "title": "Denied"}})
	if e == nil && !result.IsError {
		t.Fatal("MCP bypassed permission change")
	}
	if e = c.Call(ctx, "POST", "workspaces/"+w.ID+"/tasks", tasks.Create{Title: "Denied"}, &v); e == nil {
		t.Fatal("CLI bypassed permission change")
	}
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "task_update", Arguments: map[string]any{"task": child.ID, "field": "title", "version": 1, "text": "Denied stale edit"}})
	must(t, e)
	var denied struct {
		Error httpapi.Problem `json:"error"`
	}
	decodeTool(t, result, &denied)
	if !result.IsError || denied.Error.Code != "forbidden" || denied.Error.Current != nil {
		t.Fatal("permission denial leaked current field", denied)
	}
	record, e := f.store.ReadOAuthToken(ctx, auth.Digest(tokens.AccessToken))
	must(t, e)
	_, e = f.conn.Exec(ctx, `UPDATE browser_sessions SET tool_grants=ARRAY['identity.read'] WHERE id=$1`, record.SessionID)
	must(t, e)
	list, e := session.ListTools(ctx, nil)
	must(t, e)
	if len(list.Tools) != 2 {
		t.Fatal("revoked tool grants survive", list.Tools)
	}
}

func decodeTool(t *testing.T, result *mcp.CallToolResult, target any) {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	must(t, err)
	must(t, json.Unmarshal(raw, target))
}

func runTaskCLI(t *testing.T, server, token, input string, args ...string) []byte {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ACTA_CONFIG_DIR", dir)
	t.Setenv("ACTA_TOKEN", token)
	config, err := json.Marshal(map[string]any{"active": "default", "profiles": map[string]any{"default": map[string]string{"url": server}}})
	must(t, err)
	must(t, os.WriteFile(filepath.Join(dir, "config.json"), config, 0600))
	in, err := os.CreateTemp(dir, "input")
	must(t, err)
	defer in.Close()
	_, err = in.WriteString(input)
	must(t, err)
	_, err = in.Seek(0, 0)
	must(t, err)
	out, err := os.CreateTemp(dir, "output")
	must(t, err)
	defer out.Close()
	errs, err := os.CreateTemp(dir, "errors")
	must(t, err)
	defer errs.Close()
	command, err := cli.NewCommand(in, out, errs)
	must(t, err)
	command.SetArgs(append([]string{"--json"}, args...))
	must(t, command.ExecuteContext(t.Context()))
	raw, err := os.ReadFile(out.Name())
	must(t, err)
	return raw
}
