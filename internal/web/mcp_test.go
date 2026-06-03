package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Test-local mirrors of the (unexported) MCP output shapes.
type mcpItemT struct {
	ID          string        `json:"id"`
	Ref         string        `json:"ref"`
	Title       string        `json:"title"`
	Status      string        `json:"status"`
	Assignee    string        `json:"assignee"`
	Milestone   bool          `json:"milestone"`
	Archived    bool          `json:"archived"`
	CreatedBy   string        `json:"created_by"`
	CreatedAt   string        `json:"created_at"`
	ParentID    string        `json:"parent_id"`
	URL         string        `json:"url"`
	Description string        `json:"description"`
	Subtasks    []mcpItemT    `json:"subtasks"`
	Comments    []mcpCommentT `json:"comments"`
}

type mcpCommentT struct {
	ID     string `json:"id"`
	Author string `json:"author"`
	Body   string `json:"body"`
	At     string `json:"at"`
}

type mcpWatchT struct {
	Comments []mcpCommentT `json:"comments"`
	Cursor   string        `json:"cursor"`
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

type mcpStatusesT struct {
	Statuses []struct {
		Name     string `json:"name"`
		Position int    `json:"position"`
	} `json:"statuses"`
}

type mcpEventT struct {
	Actor     string            `json:"actor"`
	Verb      string            `json:"verb"`
	Summary   string            `json:"summary"`
	ItemID    string            `json:"item_id"`
	ItemTitle string            `json:"item_title"`
	Data      map[string]string `json:"data"`
	At        string            `json:"at"`
}

type mcpActivityT struct {
	Events []mcpEventT `json:"events"`
}

type mcpNotificationT struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Unread    bool   `json:"unread"`
	Actor     string `json:"actor"`
	Workspace string `json:"workspace"`
	ItemID    string `json:"item_id"`
	ItemTitle string `json:"item_title"`
	Excerpt   string `json:"excerpt"`
	URL       string `json:"url"`
	At        string `json:"at"`
}

type mcpNotificationsT struct {
	Notifications []mcpNotificationT `json:"notifications"`
	Unread        int                `json:"unread"`
}

type mcpMarkReadT struct {
	Unread int `json:"unread"`
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
		"whoami", "list_workspaces", "list_statuses", "list_items", "get_item", "create_item",
		"set_item_status", "set_item_assignee", "add_comment", "watch_comments", "archive_item", "unarchive_item",
		"list_notifications", "mark_notification_read",
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
	if created.CreatedAt == "" {
		t.Errorf("create_item: created_at is empty")
	}
	if !strings.Contains(created.URL, "/general?item="+created.ID) {
		t.Errorf("create_item url = %q, want a board permalink", created.URL)
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

func TestMCPListStatuses(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)
	sess := mcpConnect(t, base, token)

	// The seeded 'general' board carries the default lanes, in board order.
	got := callTool[mcpStatusesT](t, sess, "list_statuses", map[string]any{"workspace": "general"})
	var names []string
	for i, s := range got.Statuses {
		if s.Position != i {
			t.Errorf("status %q position = %d, want %d", s.Name, s.Position, i)
		}
		names = append(names, s.Name)
	}
	want := []string{"To do", "Doing", "Done"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("list_statuses = %v, want %v", names, want)
	}

	// An unknown workspace is a tool error, not an empty list.
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "list_statuses", Arguments: map[string]any{"workspace": "nope"},
	})
	if err != nil {
		t.Fatalf("call list_statuses: %v", err)
	}
	if !res.IsError {
		t.Fatalf("list_statuses on unknown workspace: want tool error, got %s", toolText(res))
	}
}

func TestMCPWatchComments(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)
	sess := mcpConnect(t, base, token)

	item := callTool[mcpItemT](t, sess, "create_item", map[string]any{"workspace": "general", "title": "Thread"})
	q := callTool[mcpCommentT](t, sess, "add_comment", map[string]any{"id": item.ID, "body": "question?"})
	if q.ID == "" {
		t.Fatal("add_comment returned no comment id")
	}

	// Already-present: a comment after the cursor comes back at once, cursor advanced.
	a1 := callTool[mcpCommentT](t, sess, "add_comment", map[string]any{"id": item.ID, "body": "answer 1"})
	got := callTool[mcpWatchT](t, sess, "watch_comments", map[string]any{"item": item.ID, "after": q.ID})
	if len(got.Comments) != 1 || got.Comments[0].ID != a1.ID || got.Comments[0].Body != "answer 1" {
		t.Fatalf("watch after the question: want [answer 1], got %+v", got.Comments)
	}
	if got.Cursor != a1.ID {
		t.Fatalf("cursor = %q, want %q", got.Cursor, a1.ID)
	}

	// Nothing newer than the cursor: blocks the window, then returns empty with the
	// cursor unchanged so the caller can loop.
	empty := callTool[mcpWatchT](t, sess, "watch_comments", map[string]any{"item": item.ID, "after": a1.ID, "timeout_seconds": 1})
	if len(empty.Comments) != 0 || empty.Cursor != a1.ID {
		t.Fatalf("watch with nothing new: want empty list + cursor %s, got %+v", a1.ID, empty)
	}

	// Unknown cursor is an error, not a silent full replay.
	if msg := toolErr(t, sess, "watch_comments", map[string]any{"item": item.ID, "after": "nope"}); msg == "" {
		t.Fatal("watch with an unknown cursor: want an error")
	}

	// Blocking: a parked watch wakes the moment a comment is posted elsewhere.
	sess2 := mcpConnect(t, base, token)
	type result struct {
		w   mcpWatchT
		err string
	}
	done := make(chan result, 1)
	go func() {
		r, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "watch_comments",
			Arguments: map[string]any{"item": item.ID, "after": a1.ID, "timeout_seconds": 10},
		})
		switch {
		case err != nil:
			done <- result{err: err.Error()}
		case r.IsError:
			done <- result{err: toolText(r)}
		default:
			var w mcpWatchT
			_ = json.Unmarshal([]byte(toolText(r)), &w)
			done <- result{w: w}
		}
	}()

	// Let the watch subscribe and park, then post the reply from the other session.
	time.Sleep(250 * time.Millisecond)
	reply := callTool[mcpCommentT](t, sess2, "add_comment", map[string]any{"id": item.ID, "body": "answer 2"})

	select {
	case r := <-done:
		if r.err != "" {
			t.Fatalf("blocking watch errored: %s", r.err)
		}
		if len(r.w.Comments) != 1 || r.w.Comments[0].ID != reply.ID || r.w.Comments[0].Body != "answer 2" {
			t.Fatalf("blocking watch: want [answer 2 = %s], got %+v", reply.ID, r.w.Comments)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("blocking watch did not return after a comment was posted")
	}
}

func TestMCPItemEdits(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)
	sess := mcpConnect(t, base, token)

	mk := func(title string) mcpItemT {
		return callTool[mcpItemT](t, sess, "create_item", map[string]any{"workspace": "general", "title": title})
	}
	a, b := mk("parent item"), mk("mover")

	// Description: set it, then get_item surfaces it.
	callTool[mcpItemT](t, sess, "set_item_description", map[string]any{"id": b.ID, "description": "the long body"})
	if got := callTool[mcpItemT](t, sess, "get_item", map[string]any{"id": b.ID}); got.Description != "the long body" {
		t.Fatalf("description = %q, want 'the long body'", got.Description)
	}

	// Milestone: flag it.
	if got := callTool[mcpItemT](t, sess, "set_item_milestone", map[string]any{"id": a.ID, "milestone": true}); !got.Milestone {
		t.Fatalf("set_item_milestone: milestone not set")
	}

	// Reparent: nest b under a, then promote it back to the board.
	if got := callTool[mcpItemT](t, sess, "set_item_parent", map[string]any{"id": b.ID, "parent": a.ID}); got.ParentID != a.ID {
		t.Fatalf("reparent: parent_id = %q, want %q", got.ParentID, a.ID)
	}
	if got := callTool[mcpItemT](t, sess, "set_item_parent", map[string]any{"id": b.ID}); got.ParentID != "" {
		t.Fatalf("promote: parent_id = %q, want empty", got.ParentID)
	}

	// A cycle is refused: an item can't become its own descendant's child.
	callTool[mcpItemT](t, sess, "set_item_parent", map[string]any{"id": b.ID, "parent": a.ID}) // b under a
	if msg := toolErr(t, sess, "set_item_parent", map[string]any{"id": a.ID, "parent": b.ID}); !strings.Contains(msg, "cycle") {
		t.Fatalf("cycle reparent error = %q, want it to mention a cycle", msg)
	}
}

func TestMCPAgentAttribution(t *testing.T) {
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

func TestMCPNotifications(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)
	sess := mcpConnect(t, base, token)

	// A fresh inbox is empty.
	if got := callTool[mcpNotificationsT](t, sess, "list_notifications", struct{}{}); got.Unread != 0 || len(got.Notifications) != 0 {
		t.Fatalf("fresh inbox = %+v, want empty", got)
	}

	// jack @-mentions himself in a comment. A self-mention is a valid bookmark,
	// and the cleanest way to seed the caller's own inbox within one session.
	item := callTool[mcpItemT](t, sess, "create_item", map[string]any{
		"workspace": "general", "title": "Watch this",
	})
	callTool[mcpCommentT](t, sess, "add_comment", map[string]any{
		"id": item.ID, "body": "@jack don't forget",
	})

	// The inbox now holds one unread, pointing at the item with an excerpt + url.
	got := callTool[mcpNotificationsT](t, sess, "list_notifications", struct{}{})
	if got.Unread != 1 || len(got.Notifications) != 1 {
		t.Fatalf("after mention = %+v, want 1 unread", got)
	}
	n := got.Notifications[0]
	if !n.Unread || n.Kind != "mention" || n.ItemID != item.ID || n.ItemTitle != "Watch this" {
		t.Fatalf("notification = %+v", n)
	}
	if n.Actor != "Jack" || n.Workspace != "general" || !strings.Contains(n.Excerpt, "don't forget") {
		t.Fatalf("notification meta = %+v", n)
	}
	if !strings.Contains(n.URL, "/general?item="+item.ID) {
		t.Errorf("notification url = %q, want a board permalink", n.URL)
	}

	// Marking it read drains the unread inbox and reports 0 remaining.
	if mk := callTool[mcpMarkReadT](t, sess, "mark_notification_read", map[string]any{"id": n.ID}); mk.Unread != 0 {
		t.Fatalf("mark_notification_read unread = %d, want 0", mk.Unread)
	}
	if got := callTool[mcpNotificationsT](t, sess, "list_notifications", struct{}{}); got.Unread != 0 || len(got.Notifications) != 0 {
		t.Fatalf("unread inbox after mark = %+v, want empty", got)
	}

	// include_read still surfaces the now-read row (it was marked read, not deleted).
	all := callTool[mcpNotificationsT](t, sess, "list_notifications", map[string]any{"include_read": true})
	if len(all.Notifications) != 1 || all.Notifications[0].Unread {
		t.Fatalf("include_read = %+v, want one read row", all.Notifications)
	}

	// Marking an unknown id is a no-op, not an error.
	if mk := callTool[mcpMarkReadT](t, sess, "mark_notification_read", map[string]any{"id": "zzzzzzzz"}); mk.Unread != 0 {
		t.Fatalf("mark unknown id unread = %d, want 0", mk.Unread)
	}
}

// TestMCPItemHumanID checks human ids over MCP: create_item returns the ref,
// and get_item resolves an item by "PREFIX-N" (case-insensitive) and by the
// opaque id. The seeded "general" workspace's prefix is GEN.
func TestMCPItemHumanID(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)
	sess := mcpConnect(t, base, token)

	created := callTool[mcpItemT](t, sess, "create_item", map[string]any{
		"workspace": "general", "title": "Ship it",
	})
	if created.Ref != "GEN-1" {
		t.Fatalf("create_item ref = %q, want GEN-1", created.Ref)
	}

	for _, ref := range []string{"GEN-1", "gen-1", created.ID} {
		got := callTool[mcpItemT](t, sess, "get_item", map[string]any{"id": ref})
		if got.ID != created.ID || got.Ref != "GEN-1" {
			t.Errorf("get_item(%q) = id %q ref %q, want the GEN-1 item", ref, got.ID, got.Ref)
		}
	}
}
