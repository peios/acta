package web_test

import (
	"context"
	"strings"
	"testing"
)

// Test-local mirrors of the (unexported) memory MCP output shapes.
type mcpMemoryT struct {
	ID        string `json:"id"`
	Scope     string `json:"scope"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Body      string `json:"body"`
	UpdatedBy string `json:"updated_by"`
	UpdatedAt string `json:"updated_at"`
}

type mcpRecallT struct {
	Memories []mcpMemoryT `json:"memories"`
}

type mcpMemDeleteT struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
	Name    string `json:"name"`
}

func findMem(ms []mcpMemoryT, scope, name string) (mcpMemoryT, bool) {
	for _, m := range ms {
		if m.Scope == scope && m.Name == name {
			return m, true
		}
	}
	return mcpMemoryT{}, false
}

func TestMCPMemory(t *testing.T) {
	base, client := newTestServer(t)
	csrf := signIn(t, client, base)
	token := mintToken(t, client, base, csrf)
	sess := mcpConnect(t, base, token)

	// The five memory tools are advertised.
	tools, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	advertised := map[string]bool{}
	for _, tl := range tools.Tools {
		advertised[tl.Name] = true
	}
	for _, want := range []string{"memory_recall", "memory_get", "memory_save", "memory_edit", "memory_delete"} {
		if !advertised[want] {
			t.Errorf("tool %q not advertised", want)
		}
	}

	// --- save (agent scope) + provenance ---
	saved := callTool[mcpMemoryT](t, sess, "memory_save", map[string]any{
		"scope": "agent", "name": "prefs.md", "summary": "my preferences", "body": "line1",
	})
	if saved.Scope != "agent" || saved.Name != "prefs.md" || saved.Summary != "my preferences" {
		t.Fatalf("save returned %+v", saved)
	}
	if saved.Body != "line1" {
		t.Fatalf("save body = %q, want line1", saved.Body)
	}
	if saved.UpdatedBy != "Jack" {
		t.Errorf("provenance updated_by = %q, want Jack", saved.UpdatedBy)
	}

	// --- recall index: summary present, body omitted ---
	idx := callTool[mcpRecallT](t, sess, "memory_recall", map[string]any{})
	m, ok := findMem(idx.Memories, "agent", "prefs.md")
	if !ok {
		t.Fatalf("recall did not include the saved memory: %+v", idx.Memories)
	}
	if m.Summary != "my preferences" {
		t.Errorf("recall summary = %q", m.Summary)
	}
	if m.Body != "" {
		t.Errorf("recall index leaked body: %q", m.Body)
	}

	// --- recall with bodies ---
	full := callTool[mcpRecallT](t, sess, "memory_recall", map[string]any{"include_bodies": true})
	if m, ok := findMem(full.Memories, "agent", "prefs.md"); !ok || m.Body != "line1" {
		t.Errorf("recall include_bodies body = %q (ok=%v)", m.Body, ok)
	}

	// --- get by scope+name ---
	got := callTool[mcpMemoryT](t, sess, "memory_get", map[string]any{"scope": "agent", "name": "prefs.md"})
	if got.Body != "line1" {
		t.Errorf("get body = %q", got.Body)
	}

	// --- append ---
	app := callTool[mcpMemoryT](t, sess, "memory_save", map[string]any{
		"scope": "agent", "name": "prefs.md", "body": "line2", "mode": "append",
	})
	if app.Body != "line1\nline2" {
		t.Errorf("append body = %q, want line1\\nline2", app.Body)
	}
	if app.Summary != "my preferences" {
		t.Errorf("append dropped summary: %q", app.Summary)
	}

	// --- edit (surgical) ---
	ed := callTool[mcpMemoryT](t, sess, "memory_edit", map[string]any{
		"scope": "agent", "name": "prefs.md", "old_string": "line1", "new_string": "LINE1",
	})
	if ed.Body != "LINE1\nline2" {
		t.Errorf("edit body = %q", ed.Body)
	}

	// --- edit: not unique without replace_all → error; with replace_all → ok ---
	callTool[mcpMemoryT](t, sess, "memory_save", map[string]any{"scope": "agent", "name": "dup.md", "body": "dup dup"})
	if msg := toolErr(t, sess, "memory_edit", map[string]any{
		"scope": "agent", "name": "dup.md", "old_string": "dup", "new_string": "x",
	}); !strings.Contains(msg, "more than once") {
		t.Errorf("non-unique edit error = %q", msg)
	}
	all := callTool[mcpMemoryT](t, sess, "memory_edit", map[string]any{
		"scope": "agent", "name": "dup.md", "old_string": "dup", "new_string": "x", "replace_all": true,
	})
	if all.Body != "x x" {
		t.Errorf("replace_all body = %q", all.Body)
	}

	// --- workspace scope (shared) ---
	wsMem := callTool[mcpMemoryT](t, sess, "memory_save", map[string]any{
		"scope": "workspace", "workspace": "general", "name": "conventions.md", "body": "use tabs",
	})
	if wsMem.Scope != "workspace" {
		t.Errorf("workspace save scope = %q", wsMem.Scope)
	}
	wsIdx := callTool[mcpRecallT](t, sess, "memory_recall", map[string]any{"workspace": "general"})
	if _, ok := findMem(wsIdx.Memories, "workspace", "conventions.md"); !ok {
		t.Errorf("recall with workspace missed the workspace memory: %+v", wsIdx.Memories)
	}
	// ...and it's absent from the default (no-workspace) recall.
	if _, ok := findMem(idx.Memories, "workspace", "conventions.md"); ok {
		t.Errorf("default recall should not include workspace memories")
	}

	// --- delete ---
	del := callTool[mcpMemDeleteT](t, sess, "memory_delete", map[string]any{"scope": "agent", "name": "prefs.md"})
	if !del.Deleted {
		t.Errorf("delete returned %+v", del)
	}
	after := callTool[mcpRecallT](t, sess, "memory_recall", map[string]any{})
	if _, ok := findMem(after.Memories, "agent", "prefs.md"); ok {
		t.Errorf("memory still present after delete")
	}

	// --- error paths ---
	if msg := toolErr(t, sess, "memory_get", map[string]any{"scope": "agent", "name": "nope.md"}); !strings.Contains(msg, "no memory named") {
		t.Errorf("missing get error = %q", msg)
	}
	if msg := toolErr(t, sess, "memory_save", map[string]any{"scope": "bogus", "name": "x", "body": "y"}); !strings.Contains(msg, "unknown scope") {
		t.Errorf("bad scope error = %q", msg)
	}
}
