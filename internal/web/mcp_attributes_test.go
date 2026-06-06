package web_test

import (
	"context"
	"testing"
)

// TestMCPItemAttributes covers the attribute surface over MCP: the four setters,
// create_item's attribute params, the list_items filters, and the output fields.
func TestMCPItemAttributes(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)
	sess := mcpConnect(t, base, token)

	// The new tools are advertised.
	tools, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := map[string]bool{}
	for _, tl := range tools.Tools {
		got[tl.Name] = true
	}
	for _, want := range []string{"set_item_priority", "set_item_type", "set_item_size", "set_item_due"} {
		if !got[want] {
			t.Errorf("tool %q not advertised", want)
		}
	}

	// create_item carries attributes through.
	created := callTool[mcpItemT](t, sess, "create_item", map[string]any{
		"workspace": "general", "title": "shipped bug",
		"priority": "urgent", "type": "bug", "size": "m", "due": "2030-02-01",
	})
	if created.Priority != "urgent" || created.Type != "bug" || created.Size != "m" || created.Due != "2030-02-01" {
		t.Fatalf("create_item attrs = %+v", created)
	}

	// The four setters update and clear.
	up := callTool[mcpItemT](t, sess, "set_item_priority", map[string]any{"id": created.ID, "priority": "low"})
	if up.Priority != "low" {
		t.Errorf("set_item_priority = %q, want low", up.Priority)
	}
	if cleared := callTool[mcpItemT](t, sess, "set_item_type", map[string]any{"id": created.ID}); cleared.Type != "" {
		t.Errorf("clear type = %q, want empty", cleared.Type)
	}
	if z := callTool[mcpItemT](t, sess, "set_item_size", map[string]any{"id": created.ID, "size": "xl"}); z.Size != "xl" {
		t.Errorf("set_item_size = %q, want xl", z.Size)
	}
	if d := callTool[mcpItemT](t, sess, "set_item_due", map[string]any{"id": created.ID}); d.Due != "" {
		t.Errorf("clear due = %q, want empty", d.Due)
	}

	// A bad slug / bad date is a tool error.
	if msg := toolErr(t, sess, "set_item_priority", map[string]any{"id": created.ID, "priority": "p0"}); msg == "" {
		t.Error("set_item_priority with a bad slug should error")
	}
	if msg := toolErr(t, sess, "set_item_due", map[string]any{"id": created.ID, "due": "Feb 1"}); msg == "" {
		t.Error("set_item_due with a bad date should error")
	}

	// list_items filters by attribute. Seed a second, calm item.
	callTool[mcpItemT](t, sess, "create_item", map[string]any{"workspace": "general", "title": "calm task"})
	// created is now priority=low, size=xl, type cleared.
	low := callTool[mcpItemsT](t, sess, "list_items", map[string]any{"workspace": "general", "priority": "low"})
	if !hasItem(low, created.ID) || len(low.Items) != 1 {
		t.Errorf("list_items priority=low = %d items, want just the seeded one", len(low.Items))
	}
	none := callTool[mcpItemsT](t, sess, "list_items", map[string]any{"workspace": "general", "priority": "none"})
	if hasItem(none, created.ID) {
		t.Error("priority=none should exclude the low-priority item")
	}
}
