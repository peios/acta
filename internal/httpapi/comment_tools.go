package httpapi

import (
	"acta2/internal/activity"
	"acta2/internal/auth"
	"acta2/internal/comments"
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"slices"
)

func (h *Handler) commentTools(s *mcp.Server, grants []string) {
	no, yes := false, true
	if slices.Contains(grants, auth.MCPTasksRead) {
		addTaskTool(s, &mcp.Tool{Name: "comment_get", Description: "Read a comment's Markdown, version, author, reply target and thread UUID. Does not mark read.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no}}, func(ctx context.Context, in struct {
			Task    string `json:"task"`
			Comment string `json:"comment"`
		}) (activity.Entry, error) {
			return h.management.Comment(ctx, "", in.Task, in.Comment)
		})
		addTaskTool(s, &mcp.Tool{Name: "comment_replies", Description: "Read replies to a root comment, newest first, without marking read. Follow cursor for older replies. UI presents replies oldest first.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no}}, func(ctx context.Context, in struct {
			Task    string `json:"task"`
			Comment string `json:"comment" jsonschema:"Root comment UUID."`
			Cursor  string `json:"cursor,omitempty"`
		}) (activity.Page, error) {
			return h.management.CommentReplies(ctx, "", in.Task, in.Comment, in.Cursor)
		})
	}
	if slices.Contains(grants, auth.MCPTasksWrite) {
		addTaskTool(s, &mcp.Tool{Name: "comment_create", Description: "Post Markdown to a task. Supply a fresh request_id UUID; reuse exactly the same UUID and inputs on an uncertain retry to prevent duplicates. Optional reply_to is any comment in this task; replies stay in its root thread. Requires commenting permission.", Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no}}, func(ctx context.Context, in struct {
			Task      string `json:"task"`
			Body      string `json:"body"`
			ReplyTo   string `json:"reply_to,omitempty"`
			RequestID string `json:"request_id"`
		}) (activity.Entry, error) {
			return h.management.CreateComment(ctx, "", in.Task, comments.Create{Body: in.Body, ReplyTo: in.ReplyTo, RequestID: in.RequestID})
		})
		addTaskTool(s, &mcp.Tool{Name: "comment_update", Description: "Edit a comment using its expected version. Requires commenting permission for your own comments or manage others’ comments for another author. On conflict inspect error.current and reconcile before retrying.", Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no}}, func(ctx context.Context, in struct {
			Task    string `json:"task"`
			Comment string `json:"comment"`
			Body    string `json:"body"`
			Version int64  `json:"version"`
		}) (activity.Entry, error) {
			return h.management.UpdateComment(ctx, "", in.Task, in.Comment, comments.Update{Body: in.Body, Version: in.Version})
		})
		addTaskTool(s, &mcp.Tool{Name: "comment_delete", Description: "Remove comment text, leaving a placeholder and replies intact. Supply the current version; deleted text is not recoverable through Acta.", Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: &yes, OpenWorldHint: &no}}, func(ctx context.Context, in struct {
			Task    string `json:"task"`
			Comment string `json:"comment"`
			Version int64  `json:"version"`
		}) (activity.Entry, error) {
			return h.management.UpdateComment(ctx, "", in.Task, in.Comment, comments.Update{Delete: true, Version: in.Version})
		})
	}
}
