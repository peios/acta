package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Test-local mirrors of the (unexported) MCP output shapes.
type mcpItemT struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Status    string        `json:"status"`
	Assignee  string        `json:"assignee"`
	Archived  bool          `json:"archived"`
	CreatedBy string        `json:"created_by"`
	ParentID  string        `json:"parent_id"`
	Subtasks  []mcpItemT    `json:"subtasks"`
	Comments  []mcpCommentT `json:"comments"`
}

type mcpCommentT struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	At     string `json:"at"`
}

type mcpPrincipalT struct {
	Username string `json:"username"`
	IsAgent  bool   `json:"is_agent"`
	Owner    string `json:"owner"`
}

type mcpWorkspacesT struct {
	Workspaces []struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"workspaces"`
}

type mcpItemsT struct {
	Items []mcpItemT `json:"items"`
}

type bearerRT struct{ token string }

func (b bearerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r2)
}

func mcpConnect(t *testing.T, base, token string) *mcp.ClientSession {
	t.Helper()
	hc := &http.Client{}
	if token != "" {
		hc.Transport = bearerRT{token: token}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "acta-test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             base + "/mcp",
		HTTPClient:           hc,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func toolText(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// callTool invokes a tool, fails on protocol or tool errors, and decodes the
// structured result into T.
func callTool[T any](t *testing.T, s *mcp.ClientSession, name string, args any) T {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("tool %s returned error: %s", name, toolText(res))
	}
	var out T
	if txt := toolText(res); txt != "" {
		if err := json.Unmarshal([]byte(txt), &out); err != nil {
			t.Fatalf("decode %s result: %v\n%s", name, err, txt)
		}
	}
	return out
}

// toolErr invokes a tool expecting a tool-level error, returning its message.
func toolErr(t *testing.T, s *mcp.ClientSession, name string, args any) string {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("tool %s: expected an error, got %s", name, toolText(res))
	}
	return toolText(res)
}

func TestMCPRequiresToken(t *testing.T) {
	base, _ := newTestServer(t)
	client := mcp.NewClient(&mcp.Implementation{Name: "acta-test", Version: "0"}, nil)
	_, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             base + "/mcp",
		HTTPClient:           &http.Client{},
		DisableStandaloneSSE: true,
	}, nil)
	if err == nil {
		t.Fatal("connect without a token: want failure, got success")
	}
}

func TestMCPToolsLifecycle(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)
	sess := mcpConnect(t, base, token)

	// The advertised tool set is the agreed surface.
	tools, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := map[string]bool{}
	for _, tl := range tools.Tools {
		got[tl.Name] = true
	}
	for _, want := range []string{
		"whoami", "list_workspaces", "list_items", "get_item", "create_item",
		"set_item_status", "set_item_assignee", "add_comment", "archive_item", "unarchive_item",
	} {
		if !got[want] {
			t.Errorf("tool %q not advertised", want)
		}
	}

	// whoami: a human principal.
	who := callTool[mcpPrincipalT](t, sess, "whoami", struct{}{})
	if who.Username != "jack" || who.IsAgent {
		t.Fatalf("whoami = %+v, want jack / not-agent", who)
	}

	// list_workspaces includes the seeded 'general' board.
	wss := callTool[mcpWorkspacesT](t, sess, "list_workspaces", struct{}{})
	if !hasWorkspace(wss, "general") {
		t.Fatalf("list_workspaces missing general: %+v", wss)
	}

	// create_item -> first lane, attributed to jack.
	created := callTool[mcpItemT](t, sess, "create_item", map[string]any{
		"workspace": "general", "title": "From MCP",
	})
	if created.ID == "" || created.Status != "To do" || created.CreatedBy != "jack" {
		t.Fatalf("create_item = %+v, want first-lane jack item", created)
	}

	// set_item_status by name.
	moved := callTool[mcpItemT](t, sess, "set_item_status", map[string]any{
		"id": created.ID, "status": "Doing",
	})
	if moved.Status != "Doing" {
		t.Fatalf("set_item_status = %q, want Doing", moved.Status)
	}

	// set_item_assignee "me" -> jack.
	assigned := callTool[mcpItemT](t, sess, "set_item_assignee", map[string]any{
		"id": created.ID, "assignee": "me",
	})
	if assigned.Assignee != "jack" {
		t.Fatalf("set_item_assignee = %q, want jack", assigned.Assignee)
	}

	// add_comment, then get_item should surface it alongside status/assignee.
	comment := callTool[mcpCommentT](t, sess, "add_comment", map[string]any{
		"id": created.ID, "body": "picked this up",
	})
	if comment.Author != "jack" || comment.Body != "picked this up" {
		t.Fatalf("add_comment = %+v", comment)
	}
	detail := callTool[mcpItemT](t, sess, "get_item", map[string]any{"id": created.ID})
	if detail.Status != "Doing" || detail.Assignee != "jack" {
		t.Fatalf("get_item core = %+v", detail)
	}
	if len(detail.Comments) != 1 || detail.Comments[0].Body != "picked this up" {
		t.Fatalf("get_item comments = %+v", detail.Comments)
	}

	// A subtask via create_item parent, then list_items by parent surfaces it.
	sub := callTool[mcpItemT](t, sess, "create_item", map[string]any{
		"workspace": "general", "title": "a subtask", "parent": created.ID,
	})
	if sub.ParentID != created.ID {
		t.Fatalf("subtask parent = %q, want %q", sub.ParentID, created.ID)
	}
	kids := callTool[mcpItemsT](t, sess, "list_items", map[string]any{
		"workspace": "general", "parent": created.ID,
	})
	if len(kids.Items) != 1 || kids.Items[0].ID != sub.ID {
		t.Fatalf("list_items parent = %+v, want [%s]", kids.Items, sub.ID)
	}

	// Board listing shows the root item (not the subtask) and its progress.
	board := callTool[mcpItemsT](t, sess, "list_items", map[string]any{"workspace": "general"})
	if !hasItem(board, created.ID) || hasItem(board, sub.ID) {
		t.Fatalf("board listing = %+v, want root only", board.Items)
	}

	// archive hides it from the board; unarchive restores it.
	arch := callTool[mcpItemT](t, sess, "archive_item", map[string]any{"id": created.ID})
	if !arch.Archived {
		t.Fatal("archive_item: archived not set")
	}
	if board := callTool[mcpItemsT](t, sess, "list_items", map[string]any{"workspace": "general"}); hasItem(board, created.ID) {
		t.Fatal("archived item still on the board")
	}
	un := callTool[mcpItemT](t, sess, "unarchive_item", map[string]any{"id": created.ID})
	if un.Archived {
		t.Fatal("unarchive_item: still archived")
	}

	// An unknown status name is a tool error that names the bad value.
	if msg := toolErr(t, sess, "set_item_status", map[string]any{
		"id": created.ID, "status": "Nowhere",
	}); !strings.Contains(msg, "Nowhere") {
		t.Fatalf("unknown-status error = %q, want it to name Nowhere", msg)
	}

	// A bogus id reads as not-found.
	if msg := toolErr(t, sess, "get_item", map[string]any{"id": "zzzzzzzz"}); !strings.Contains(msg, "not found") {
		t.Fatalf("missing-item error = %q", msg)
	}
}

func TestMCPAgentAttribution(t *testing.T) {
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
	sess := mcpConnect(t, base, m[1])

	// whoami reports the agent identity and its owner.
	who := callTool[mcpPrincipalT](t, sess, "whoami", struct{}{})
	if who.Username != "jack/deploybot" || !who.IsAgent || who.Owner != "jack" {
		t.Fatalf("agent whoami = %+v", who)
	}

	// Items the agent creates are attributed to its handle.
	created := callTool[mcpItemT](t, sess, "create_item", map[string]any{
		"workspace": "general", "title": "Built by a bot",
	})
	if created.CreatedBy != "jack/deploybot" {
		t.Fatalf("create_item created_by = %q, want jack/deploybot", created.CreatedBy)
	}
}

func hasWorkspace(w mcpWorkspacesT, slug string) bool {
	for _, ws := range w.Workspaces {
		if ws.Slug == slug {
			return true
		}
	}
	return false
}

func hasItem(l mcpItemsT, id string) bool {
	for _, it := range l.Items {
		if it.ID == id {
			return true
		}
	}
	return false
}
