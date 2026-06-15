package web_test

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPResourcesAndPrompts(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)
	sess := mcpConnect(t, base, token)
	ctx := context.Background()

	// The resource list exposes the conventions guide and a snapshot per board.
	rl, err := sess.ListResources(ctx, &mcp.ListResourcesParams{})
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	uris := map[string]bool{}
	for _, r := range rl.Resources {
		uris[r.URI] = true
	}
	if !uris["acta://guide"] || !uris["acta://workspace/general"] {
		t.Fatalf("missing expected resources, got %v", uris)
	}

	// The guide reads as the hardcoded, shipped document.
	guide := readResource(t, sess, "acta://guide")
	if !strings.Contains(guide, "Using Acta") || !strings.Contains(guide, "list_workspaces") {
		t.Fatalf("guide resource missing expected content:\n%s", guide)
	}
	// It leans hard on memory and renders the live workspace list inline.
	if !strings.Contains(guide, "memory_recall") {
		t.Fatalf("guide resource doesn't mention memory:\n%s", guide)
	}
	if !strings.Contains(guide, "slug `general`") {
		t.Fatalf("guide resource missing the live workspace list:\n%s", guide)
	}

	// Creating an item shows up in the live workspace snapshot, grouped by status.
	callTool[mcpItemT](t, sess, "create_item", map[string]any{"workspace": "general", "title": "Wire MCP resources"})
	snap := readResource(t, sess, "acta://workspace/general")
	if !strings.Contains(snap, "# General") || !strings.Contains(snap, "Wire MCP resources") {
		t.Fatalf("workspace snapshot missing content:\n%s", snap)
	}

	// The seeded prompts are advertised.
	pl, err := sess.ListPrompts(ctx, &mcp.ListPromptsParams{})
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	names := map[string]bool{}
	for _, p := range pl.Prompts {
		names[p.Name] = true
	}
	if !names["standup"] || !names["triage"] {
		t.Fatalf("missing seeded prompts, got %v", names)
	}

	// Getting a prompt substitutes its {{workspace}} argument into the message.
	gp, err := sess.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      "standup",
		Arguments: map[string]string{"workspace": "general"},
	})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	msg := promptText(t, gp)
	if !strings.Contains(msg, "general") || !strings.Contains(msg, "standup") {
		t.Fatalf("prompt message missing substituted arg:\n%s", msg)
	}
	if strings.Contains(msg, "{{workspace}}") {
		t.Fatalf("placeholder left unsubstituted:\n%s", msg)
	}
}

func TestMCPListActivity(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)
	sess := mcpConnect(t, base, token)

	// A create then a status move should both land in the log, newest first.
	it := callTool[mcpItemT](t, sess, "create_item",
		map[string]any{"workspace": "general", "title": "Ship activity log"})
	callTool[mcpItemT](t, sess, "set_item_status",
		map[string]any{"id": it.ID, "status": "Doing"})

	act := callTool[mcpActivityT](t, sess, "list_activity", map[string]any{"workspace": "general"})
	if len(act.Events) < 2 {
		t.Fatalf("want >=2 events, got %d: %+v", len(act.Events), act.Events)
	}
	newest := act.Events[0]
	if newest.Verb != "item.status_changed" || newest.Data["to"] != "Doing" {
		t.Fatalf("newest event = %+v, want a status change to Doing", newest)
	}
	if newest.Summary == "" || newest.ItemID != it.ID {
		t.Fatalf("event missing summary/item id: %+v", newest)
	}

	// Scoped to the item, the same two events appear and nothing else.
	scoped := callTool[mcpActivityT](t, sess, "list_activity",
		map[string]any{"workspace": "general", "item": it.ID})
	if len(scoped.Events) != 2 {
		t.Fatalf("item-scoped activity: want 2, got %d", len(scoped.Events))
	}
}

func readResource(t *testing.T, s *mcp.ClientSession, uri string) string {
	t.Helper()
	res, err := s.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("read resource %s: %v", uri, err)
	}
	if len(res.Contents) == 0 {
		t.Fatalf("read resource %s: no contents", uri)
	}
	return res.Contents[0].Text
}

func promptText(t *testing.T, res *mcp.GetPromptResult) string {
	t.Helper()
	if len(res.Messages) == 0 {
		t.Fatal("prompt returned no messages")
	}
	tc, ok := res.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("prompt message content is not text: %T", res.Messages[0].Content)
	}
	return tc.Text
}
