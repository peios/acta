package integration

import (
	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/config"
	"acta/internal/httpapi"
	"acta/internal/migration"
	"acta/internal/tasks"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func migrationAccess(t *testing.T, f securityFixture, agent string) (context.Context, auth.OAuthTokens, string) {
	t.Helper()
	o := oauthFromAccount(t, f, mcpResource)
	redirect, e := f.security.ApproveOAuthTools(t.Context(), f.token, o.request, f.binding, true, "http://localhost:8081", agent, []string{auth.MCPIdentityGrant, auth.MCPMigration})
	must(t, e)
	u, e := url.Parse(redirect)
	must(t, e)
	o.code = u.Query().Get("code")
	tokens := o.exchange(t)
	ctx, _, grants, e := f.security.MCPContext(t.Context(), tokens.AccessToken, mcpResource)
	must(t, e)
	if !slices.Contains(grants, auth.MCPMigration) {
		t.Fatal("missing migration grant")
	}
	tok, e := f.store.ReadOAuthToken(ctx, auth.Digest(tokens.AccessToken))
	must(t, e)
	return ctx, tokens, tok.SessionID
}
func migrationFields(values map[string]any) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, v := range values {
		out[k], _ = json.Marshal(v)
	}
	return out
}
func migrationData(t *testing.T, r migration.Record) map[string]any {
	t.Helper()
	var out map[string]any
	must(t, json.Unmarshal(r.Data, &out))
	return out
}
func TestMigrationContentAndIntegrity(t *testing.T) {
	f := securityDatabase(t)
	m := manager(f)
	ctx0 := t.Context()
	historical, _ := permissionMember(t, f, "historical")
	agent := agentAccount(t, f, "migration", nil)
	ctx, _, session := migrationAccess(t, f, agent.ID)
	create := func(kind string, fields map[string]any) migration.Record {
		t.Helper()
		r, e := m.Migrate(ctx, "create", migration.Request{Kind: kind, ActingAs: historical.ID, Fields: migrationFields(fields)})
		must(t, e)
		return r
	}
	edit := func(r migration.Record, fields map[string]any) migration.Record {
		t.Helper()
		v, e := m.Migrate(ctx, "edit", migration.Request{Kind: r.Kind, ID: r.ID, Revision: r.Revision, ActingAs: agent.ID, Fields: migrationFields(fields)})
		must(t, e)
		return v
	}
	get := func(kind, id string) migration.Record {
		t.Helper()
		r, e := m.Migrate(ctx, "get", migration.Request{Kind: kind, ID: id})
		must(t, e)
		return r
	}
	fail := func(op string, r migration.Record, fields map[string]any) {
		t.Helper()
		_, e := m.Migrate(ctx, op, migration.Request{Kind: r.Kind, ID: r.ID, Revision: r.Revision, ActingAs: historical.ID, Fields: migrationFields(fields)})
		if e == nil {
			t.Fatal("invalid mutation accepted", fields)
		}
	}
	w := create("workspace", map[string]any{"name": "Migration", "slug": "migration", "prefix": "PEI", "statuses": []string{"Open", "Closed"}, "creation_status": "Open", "completed_status": "Closed"})
	w = edit(get("workspace", w.ID), map[string]any{"status_board": "backlog", "statuses": []string{"Ideas", "Ready"}, "creation_status": "Ideas", "completed_status": ""})
	config, e := m.TaskConfig(ctx0, f.token, w.ID)
	must(t, e)
	if len(config.Statuses) != 4 || config.Boards[1].Completed != "" {
		t.Fatal("migration backlog config", config)
	}
	oldTime := "2020-01-02T03:04:05Z"
	task := create("task", map[string]any{"workspace_id": w.ID, "reference": "PEI-123", "title": "Historical task", "created_at": oldTime, "updated_at": oldTime, "assignees": []string{historical.ID, agent.ID}})
	data := migrationData(t, task)
	if data["reference"] != "PEI-123" || data["created_by"] != historical.ID {
		t.Fatal(data)
	}
	next := create("task", map[string]any{"workspace_id": w.ID, "title": "Next"})
	if migrationData(t, next)["reference"] != "PEI-124" {
		t.Fatal("counter not advanced", next)
	}
	fail("create", migration.Record{Kind: "task"}, map[string]any{"workspace_id": w.ID, "reference": "PEI-123", "title": "Collision"})
	fail("edit", task, map[string]any{"parent_id": task.ID})
	fail("edit", task, map[string]any{"title": nil})
	fail("edit", task, map[string]any{"id": "invented"})
	fail("edit", get("workspace", w.ID), map[string]any{"statuses": []string{"New", "Done"}, "creation_status": "New", "completed_status": "Done"})
	// A normal edit invalidates the opaque migration revision.
	live, e := m.Task(ctx0, f.token, task.ID)
	must(t, e)
	_, e = patchTask(t, f, live, "title", "Concurrent normal edit")
	must(t, e)
	_, e = m.Migrate(ctx, "edit", migration.Request{Kind: "task", ID: task.ID, Revision: task.Revision, ActingAs: agent.ID, Fields: migrationFields(map[string]any{"title": "Lost"})})
	if !errors.Is(e, migration.ErrConflict) {
		t.Fatal("lost-update check", e)
	}
	task = edit(get("task", task.ID), map[string]any{"description": "Preserve title", "archived_at": oldTime})
	if migrationData(t, task)["title"] != "Concurrent normal edit" {
		t.Fatal("omission clobbered title")
	}
	batch := migrationData(t, task)["archive_batch"]
	task = edit(task, map[string]any{"description": "Still archived"})
	if migrationData(t, task)["archive_batch"] != batch {
		t.Fatal("archive batch changed")
	}
	task = edit(task, map[string]any{"archived_at": nil})
	comment := create("comment", map[string]any{"task_id": task.ID, "body": "Original", "created_at": oldTime, "updated_at": oldTime})
	reply := create("comment", map[string]any{"task_id": task.ID, "body": "Reply", "reply_to": comment.ID})
	if migrationData(t, reply)["reply_to"] != comment.ID {
		t.Fatal("reply relationship")
	}
	comment = edit(comment, map[string]any{"author_id": agent.ID, "body": "Corrected"})
	if migrationData(t, comment)["author_id"] != agent.ID {
		t.Fatal("comment impersonation")
	}
	for _, scope := range []string{"site", "workspace", "user", "agent"} {
		fields := map[string]any{"scope": scope, "key": "migration-example", "summary": "A summary", "content": "Some content", "created_at": oldTime}
		if scope != "site" {
			fields["scope_id"] = map[string]string{"workspace": w.ID, "user": historical.ID, "agent": agent.ID}[scope]
		}
		mem := create("memory", fields)
		mem = edit(mem, map[string]any{"content": "Updated"})
		if migrationData(t, mem)["updated_by"] != agent.ID || migrationData(t, mem)["created_by"] != historical.ID {
			t.Fatal("memory actors")
		}
	}
	doc := create("document", map[string]any{"task_id": task.ID, "filename": "notes.txt", "content_base64": base64.StdEncoding.EncodeToString([]byte("original")), "created_at": oldTime})
	doc = edit(doc, map[string]any{"title": "Renamed"})
	if migrationData(t, doc)["revision"] != float64(2) {
		t.Fatal("document version")
	}
	var contents string
	must(t, f.conn.QueryRow(ctx0, `SELECT convert_from(content,'UTF8') FROM document_files WHERE file_id=$1`, migrationData(t, doc)["file_id"]).Scan(&contents))
	if contents != "original" {
		t.Fatal("omitted bytes lost")
	}
	act := create("activity", map[string]any{"task_id": task.ID, "change": map[string]any{"kind": "task.changed", "field": "title", "before": map[string]any{"text": "Earlier"}, "after": map[string]any{"text": "Later"}}, "created_at": oldTime, "updated_at": oldTime})
	act = edit(act, map[string]any{"created_at": "2019-01-01T00:00:00Z"})
	var count int
	must(t, f.conn.QueryRow(ctx0, `SELECT count(*) FROM activity_events WHERE entry_id=$1`, act.ID).Scan(&count))
	if count != 2 {
		t.Fatal("correction overwrote event")
	}
	_, e = f.conn.Exec(ctx0, `UPDATE migration_operations SET acting_as=$1`, agent.ID)
	if e == nil {
		t.Fatal("audit mutable")
	}
	must(t, f.conn.QueryRow(ctx0, `SELECT count(*) FROM migration_operations WHERE operator_id<>$1 OR session_id<>$2`, agent.ID, session).Scan(&count))
	if count != 0 {
		t.Fatal("operator misattributed")
	}
	// Only the normal edit above can have generated notifications.
	must(t, f.conn.QueryRow(ctx0, `SELECT count(*) FROM migration_operations WHERE target_id=$1 AND operation='create' AND acting_as=$2`, task.ID, historical.ID).Scan(&count))
	if count != 1 {
		t.Fatal("audit missing")
	}
	page, e := m.MigrationFind(ctx, migration.Find{Kind: "task", Parent: w.ID})
	must(t, e)
	if len(page.Records) != 2 {
		t.Fatal(page)
	}
	page, e = m.MigrationFind(ctx, migration.Find{Kind: "account", Query: "historical"})
	must(t, e)
	if len(page.Records) != 1 {
		t.Fatal(page)
	}
	// Explicit references also advance the ordinary allocator.
	normal, e := m.CreateTask(ctx0, f.token, w.ID, tasks.Create{Title: "Normal"})
	must(t, e)
	if normal.Number != 125 {
		t.Fatal(normal.Number)
	}
}

func TestMigrationAuthorityRevocation(t *testing.T) {
	f := securityDatabase(t)
	m := manager(f)
	ctx0 := t.Context()
	owner, limited := permissionMember(t, f, "delegator")
	a := agentAccount(t, limited, "migration", nil)
	o := oauthFromAccount(t, limited, mcpResource)
	_, e := limited.security.ApproveOAuthTools(ctx0, limited.token, o.request, limited.binding, true, "http://localhost:8081", a.ID, []string{auth.MCPMigration})
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("non-superuser consent", e)
	}
	grantPermissions(t, f, owner.ID, []string{accounts.Superuser}, false)
	ctx, tokens, session := migrationAccess(t, limited, a.ID)
	_, e = m.Migrate(ctx0, "get", migration.Request{Kind: "workspace", ID: "invalid"})
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("browser authority accepted", e)
	}
	req := migration.Request{Kind: "workspace", ActingAs: owner.ID, Fields: migrationFields(map[string]any{"name": "Forbidden", "slug": "forbidden"})}
	must(t, limited.security.AmendMCPGrants(ctx0, limited.token, a.ID, session, []string{auth.MCPIdentityGrant, auth.MCPMigration}, []string{auth.MCPIdentityGrant}))
	_, e = m.Migrate(ctx, "create", req)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("captured context survived grant revocation", e)
	}
	must(t, limited.security.AmendMCPGrants(ctx0, limited.token, a.ID, session, []string{auth.MCPIdentityGrant}, []string{auth.MCPIdentityGrant, auth.MCPMigration}))
	grantPermissions(t, f, owner.ID, nil, false)
	_, e = m.Migrate(ctx, "create", req)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("captured context survived demotion", e)
	}
	_, _, grants, e := f.security.MCPContext(ctx0, tokens.AccessToken, mcpResource)
	must(t, e)
	if slices.Contains(grants, auth.MCPMigration) {
		t.Fatal("demoted tools still listed")
	}
	e = limited.security.AmendMCPGrants(ctx0, limited.token, a.ID, session, []string{auth.MCPIdentityGrant, auth.MCPMigration}, []string{auth.MCPMigration})
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("demoted regrant", e)
	}
	var count int
	must(t, f.conn.QueryRow(ctx0, `SELECT count(*) FROM migration_operations`).Scan(&count))
	if count != 0 {
		t.Fatal("denied writes left audit/state")
	}
}

func TestMigrationMCPToolsAndNotifications(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer srv.Close()
	cfg, e := config.Parse(srv.URL, "")
	must(t, e)
	handler = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	historical, _ := permissionMember(t, f, "author")
	a := agentAccount(t, f, "assistant", nil)
	policy, e := manager(f).AgentWorkspaces(ctx, f.token, a.ID)
	must(t, e)
	must(t, manager(f).SetAgentWorkspaces(ctx, f.token, a.ID, policy.Version, false, nil))
	o := oauthFromAccount(t, f, srv.URL+"/mcp")
	redirect, e := f.security.ApproveOAuthTools(ctx, f.token, o.request, f.binding, true, srv.URL, a.ID, []string{auth.MCPMigration, auth.MCPTasksWrite})
	must(t, e)
	u, e := url.Parse(redirect)
	must(t, e)
	o.code = u.Query().Get("code")
	tokens := o.exchange(t)
	client, e := mcp.NewClient(&mcp.Implementation{Name: "Migration test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{tokens.AccessToken}}}, nil)
	must(t, e)
	defer client.Close()
	list, e := client.ListTools(ctx, nil)
	must(t, e)
	names := []string{}
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	for _, name := range []string{"migration_schema", "migration_get", "migration_find", "migration_create", "migration_edit"} {
		if !slices.Contains(names, name) {
			t.Fatal("missing tool", name)
		}
	}
	call := func(name string, args map[string]any) json.RawMessage {
		t.Helper()
		r, e := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		must(t, e)
		if r.IsError {
			t.Fatalf("%s: %+v", name, r)
		}
		raw, e := json.Marshal(r.StructuredContent)
		must(t, e)
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		must(t, json.Unmarshal(raw, &envelope))
		return envelope.Data
	}
	call("migration_schema", map[string]any{})
	var w, task, comment migration.Record
	must(t, json.Unmarshal(call("migration_create", map[string]any{"kind": "workspace", "acting_as": historical.ID, "fields": map[string]any{"name": "Transport", "slug": "transport", "prefix": "MIG"}}), &w))
	must(t, json.Unmarshal(call("migration_create", map[string]any{"kind": "task", "acting_as": historical.ID, "fields": map[string]any{"workspace_id": w.ID, "title": "MCP task", "assignees": []string{a.ID}}}), &task))
	// Generated documents can be larger than the ordinary 256 KiB MCP body limit.
	payload := strings.Repeat("file content\n", 30000)
	call("migration_create", map[string]any{"kind": "document", "acting_as": historical.ID, "fields": map[string]any{"task_id": task.ID, "filename": "large.txt", "content_base64": base64.StdEncoding.EncodeToString([]byte(payload))}})
	must(t, json.Unmarshal(call("migration_create", map[string]any{"kind": "comment", "acting_as": a.ID, "fields": map[string]any{"task_id": task.ID, "body": "Historical comment"}}), &comment))
	call("migration_edit", map[string]any{"kind": "comment", "id": comment.ID, "revision": comment.Revision, "acting_as": historical.ID, "fields": map[string]any{"body": "Revised"}})
	call("migration_get", map[string]any{"kind": "task", "id": task.ID})
	call("migration_find", map[string]any{"kind": "workspace"})
	for _, kind := range []string{"comment", "document", "activity", "memory", "account"} {
		args := map[string]any{"kind": kind}
		if kind != "account" && kind != "memory" {
			args["parent"] = task.ID
		}
		call("migration_find", args)
	}
	var count int
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM notifications`).Scan(&count))
	if count != 0 {
		t.Fatal("migration sent notifications", count)
	}
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM task_followers WHERE task_id=$1 AND following`, task.ID).Scan(&count))
	if count != 2 {
		t.Fatal("auto-follow not maintained", count)
	}
	// The migration grant does not give ordinary task tools the historical actor's access.
	r, e := client.CallTool(ctx, &mcp.CallToolParams{Name: "task_create", Arguments: map[string]any{"workspace": w.ID, "title": "Should fail"}})
	if e == nil && !r.IsError {
		t.Fatal("ordinary tools acquired migration authority")
	}
	// Unknown fields cannot sneak account/permission writes through generic fields.
	r, e = client.CallTool(ctx, &mcp.CallToolParams{Name: "migration_create", Arguments: map[string]any{"kind": "workspace", "acting_as": a.ID, "fields": map[string]any{"name": "Bad", "slug": "bad", "permissions": []string{accounts.Superuser}}}})
	must(t, e)
	if !r.IsError {
		t.Fatal("unknown fields accepted")
	}
}

func TestMigrationConcurrencyPaginationAndHistoricalIdentity(t *testing.T) {
	f := securityDatabase(t)
	m := manager(f)
	ctx, _, _ := migrationAccess(t, f, "")
	author, _, e := m.Create(t.Context(), f.token, "pending-author", "Historical author")
	must(t, e)
	create := func(kind string, fields map[string]any) (migration.Record, error) {
		return m.Migrate(ctx, "create", migration.Request{Kind: kind, ActingAs: author.ID, Fields: migrationFields(fields)})
	}
	w, e := create("workspace", map[string]any{"name": "Pages", "slug": "pages", "prefix": "PAG"})
	must(t, e)
	ch := make(chan error, 2)
	for range 2 {
		go func() {
			_, e := create("task", map[string]any{"workspace_id": w.ID, "title": "Collision race", "reference": "PAG-80"})
			ch <- e
		}()
	}
	wins := 0
	for range 2 {
		if <-ch == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatal("duplicate reference race", wins)
	}
	first, e := m.MigrationFind(ctx, migration.Find{Kind: "task", Parent: w.ID})
	must(t, e)
	base := first.Records[0]
	for range 2 {
		go func() {
			_, e := m.Migrate(ctx, "edit", migration.Request{Kind: "task", ID: base.ID, Revision: base.Revision, ActingAs: author.ID, Fields: migrationFields(map[string]any{"description": "Racing change"})})
			ch <- e
		}()
	}
	wins = 0
	for range 2 {
		e := <-ch
		if e == nil {
			wins++
		} else if !errors.Is(e, migration.ErrConflict) {
			t.Fatal(e)
		}
	}
	if wins != 1 {
		t.Fatal("revision race", wins)
	}
	for i := 0; i < 50; i++ {
		_, e = create("task", map[string]any{"workspace_id": w.ID, "title": fmt.Sprintf("Page %d", i)})
		must(t, e)
	}
	first, e = m.MigrationFind(ctx, migration.Find{Kind: "task", Parent: w.ID})
	must(t, e)
	if len(first.Records) != 50 || !first.More || first.NextOffset != 50 {
		t.Fatal(first)
	}
	last, e := m.MigrationFind(ctx, migration.Find{Kind: "task", Parent: w.ID, Offset: first.NextOffset})
	must(t, e)
	if len(last.Records) != 1 || last.More {
		t.Fatal(last)
	}
	for _, a := range first.Records {
		if a.ID == last.Records[0].ID {
			t.Fatal("page repeated entry")
		}
	}
	var count int
	must(t, f.conn.QueryRow(t.Context(), `SELECT count(*) FROM migration_operations`).Scan(&count))
	if count != 53 {
		t.Fatal("partial or duplicate audit", count)
	}
	// The failed reference allocation did not consume a counter value.
	var next int64
	must(t, f.conn.QueryRow(t.Context(), `SELECT next_number FROM task_settings WHERE workspace_id=$1`, w.ID).Scan(&next))
	if next != 131 {
		t.Fatal(next)
	}
}
