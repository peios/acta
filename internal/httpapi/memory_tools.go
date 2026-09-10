package httpapi

import (
	"acta2/internal/auth"
	"acta2/internal/memories"
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"slices"
)

// MCP recall is a scannable index. Keep browser metadata and full bodies on
// the detail surface; IDs and scope remain essential for retrieval and identity.
type memoryIndexEntry struct {
	ID       string `json:"id"`
	Scope    string `json:"scope"`
	Key      string `json:"key"`
	Summary  string `json:"summary"`
	Revision int64  `json:"revision"`
}
type memoryIndexPage struct {
	Memories []memoryIndexEntry `json:"memories"`
	Cursor   string             `json:"cursor,omitempty"`
}

func memoryIndex(page memories.Page) memoryIndexPage {
	out := memoryIndexPage{Memories: make([]memoryIndexEntry, 0, len(page.Memories)), Cursor: page.Cursor}
	for _, row := range page.Memories {
		out.Memories = append(out.Memories, memoryIndexEntry{ID: row.ID, Scope: row.Scope, Key: row.Key, Summary: row.Summary, Revision: row.Revision})
	}
	return out
}

func (h *Handler) memoryTools(s *mcp.Server, grants []string) {
	no, yes := false, true
	read := func(name, description string) *mcp.Tool {
		return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no}}
	}
	if slices.Contains(grants, auth.MCPMemoriesRead) {
		addTaskTool(s, read("memory_recall", "Recall a small index of accessible site, user and own-agent memories. For an agent, user means its human owner. A nullable workspace adds that workspace's memories. Returns only id, scope, key, summary and revision per entry; use memory_get for relevant entries. Optional query searches key, summary and content; follow cursor with unchanged options."), func(ctx context.Context, in struct {
			Workspace *string `json:"workspace"`
			Query     string  `json:"query,omitempty"`
			Cursor    string  `json:"cursor,omitempty"`
		}) (memoryIndexPage, error) {
			page, err := h.management.RecallMemories(ctx, "", memories.Recall{Workspace: in.Workspace, Query: in.Query, Cursor: in.Cursor})
			return memoryIndex(page), err
		})
		addTaskTool(s, read("memory_get", "Read one memory's full Markdown content and current revision, after checking access."), func(ctx context.Context, in struct {
			ID string `json:"id"`
		}) (memories.Memory, error) {
			return h.management.GetMemory(ctx, "", in.ID)
		})
	}
	if slices.Contains(grants, auth.MCPMemoriesWrite) {
		addTaskTool(s, &mcp.Tool{Name: "memory_save", Description: memories.SaveDescription, Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, OpenWorldHint: &no}}, func(ctx context.Context, in struct {
			ID        string  `json:"id,omitempty"`
			Scope     string  `json:"scope"`
			Workspace *string `json:"workspace,omitempty"`
			Key       string  `json:"key"`
			Summary   string  `json:"summary"`
			Content   string  `json:"content"`
			Revision  int64   `json:"revision"`
		}) (memories.Memory, error) {
			return h.management.SaveMemory(ctx, "", memories.Save{ID: in.ID, Scope: in.Scope, Workspace: in.Workspace, Key: in.Key, Summary: in.Summary, Content: in.Content, Revision: in.Revision})
		})
		addTaskTool(s, &mcp.Tool{Name: "memory_delete", Description: "Delete a memory by UUID and the revision read. Check that the knowledge is obsolete or incorrect before deleting. Requires write access to its scope.", Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, OpenWorldHint: &no}}, func(ctx context.Context, in struct {
			ID       string `json:"id"`
			Revision int64  `json:"revision"`
		}) (map[string]bool, error) {
			e := h.management.DeleteMemory(ctx, "", in.ID, in.Revision)
			return map[string]bool{"deleted": e == nil}, e
		})
	}
}
