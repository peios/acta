package httpapi

import (
	"acta/internal/auth"
	"acta/internal/documents"
	"context"
	"encoding/base64"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"slices"
	"unicode/utf8"
)

type documentRead struct {
	documents.Version
	Encoding   string `json:"encoding"`
	Content    string `json:"content"`
	Offset     int64  `json:"offset"`
	NextOffset *int64 `json:"next_offset"`
}

func (h *Handler) documentTools(s *mcp.Server, grants []string) {
	no := false
	tool := func(name, description string, write bool) *mcp.Tool {
		return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: !write, DestructiveHint: &write, OpenWorldHint: &no}}
	}
	if slices.Contains(grants, auth.MCPTasksRead) {
		addTaskTool(s, tool("documents_list", "List a task's documents and latest version metadata, without file contents. task accepts a task UUID or reference. Follow cursor for more.", false), func(ctx context.Context, in struct {
			Task   string `json:"task"`
			Cursor string `json:"cursor,omitempty"`
		}) (documents.Page, error) {
			return h.management.Documents(ctx, "", in.Task, in.Cursor)
		})
		addTaskTool(s, tool("document_get", "Get a document's task, current revision, filename, type and size. Read this revision before uploading a replacement or deleting.", false), func(ctx context.Context, in struct {
			ID string `json:"id"`
		}) (documents.Document, error) {
			return h.management.Document(ctx, "", in.ID)
		})
		addTaskTool(s, tool("document_versions", "List immutable versions newest first without contents. Follow before for older versions.", false), func(ctx context.Context, in struct {
			ID     string `json:"id"`
			Before int64  `json:"before,omitempty"`
		}) (documents.History, error) {
			return h.management.DocumentVersions(ctx, "", in.ID, in.Before)
		})
		addTaskTool(s, tool("document_read", "Read up to 32 KiB of a document version. revision=0 selects latest; use the returned revision for subsequent chunks to avoid mixing versions. Text is UTF-8; binary is base64. next_offset=null means finished. For binary files prefer acta document download to a local file, then the provider's native file tools.", false), func(ctx context.Context, in struct {
			ID       string `json:"id"`
			Revision int64  `json:"revision"`
			Offset   int64  `json:"offset,omitempty"`
		}) (documentRead, error) {
			var out documentRead
			v, b, e := h.management.DocumentFile(ctx, "", in.ID, in.Revision)
			if e != nil {
				return out, e
			}
			if in.Offset < 0 || in.Offset > int64(len(b)) {
				return out, documents.Invalid("offset", "Use the returned next_offset.")
			}
			start := int(in.Offset)
			end := min(start+32768, len(b))
			encoding := "base64"
			if utf8.Valid(b) {
				encoding = "utf8"
				if start < len(b) && !utf8.RuneStart(b[start]) {
					return out, documents.Invalid("offset", "Use a UTF-8 character boundary from next_offset.")
				}
				for end < len(b) && !utf8.RuneStart(b[end]) {
					end--
				}
			}
			out = documentRead{Version: v, Encoding: encoding, Offset: in.Offset}
			if encoding == "utf8" {
				out.Content = string(b[start:end])
			} else {
				out.Content = base64.StdEncoding.EncodeToString(b[start:end])
			}
			if end < len(b) {
				next := int64(end)
				out.NextOffset = &next
			}
			return out, nil
		})
	}
	if slices.Contains(grants, auth.MCPTasksWrite) {
		addTaskTool(s, tool("document_save", "Create a task document (revision=0) or upload a new immutable version (id plus latest revision from document_get). Supply task, title, filename and exactly one of content (UTF-8) or base64. Inline payload is limited to 128 KiB; for larger/local files use acta document upload, up to 20 MiB, without encoding bytes into model context. Never silently retry a revision conflict or overwrite another agent's update. Documents are task-owned outputs; do not copy them into memories.", true), func(ctx context.Context, in struct {
			ID       string  `json:"id,omitempty"`
			Task     string  `json:"task"`
			Title    string  `json:"title"`
			Filename string  `json:"filename"`
			Revision int64   `json:"revision"`
			Content  *string `json:"content,omitempty"`
			Base64   *string `json:"base64,omitempty"`
		}) (documents.Document, error) {
			var b []byte
			var e error
			if (in.Content == nil) == (in.Base64 == nil) {
				return documents.Document{}, documents.Invalid("content", "Supply exactly one of content or base64.")
			}
			if in.Content != nil {
				if len(*in.Content) > 128*1024 {
					return documents.Document{}, documents.Invalid("content", "Use CLI upload for payloads over 128 KiB.")
				}
				b = []byte(*in.Content)
			} else {
				if len(*in.Base64) > 128*1024 {
					return documents.Document{}, documents.Invalid("base64", "Use CLI upload for payloads over 128 KiB.")
				}
				b, e = base64.StdEncoding.Strict().DecodeString(*in.Base64)
				if e != nil {
					return documents.Document{}, documents.Invalid("base64", "Supply valid base64.")
				}
			}
			return h.management.SaveDocument(ctx, "", documents.Save{ID: in.ID, Task: in.Task, Title: in.Title, Filename: in.Filename, Revision: in.Revision, Content: b})
		})
		addTaskTool(s, tool("document_delete", "Permanently delete a task document and all its stored versions, using its latest revision. Requires task edit permission. Task activity retains a record of the deletion.", true), func(ctx context.Context, in struct {
			ID       string `json:"id"`
			Revision int64  `json:"revision"`
		}) (map[string]bool, error) {
			e := h.management.DeleteDocument(ctx, "", in.ID, in.Revision)
			return map[string]bool{"deleted": e == nil}, e
		})
	}
}
