package httpapi

import (
	"context"

	"acta2/internal/guide"
	"acta2/learn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const agentInstructions = `Acta is shared working context for people and agents: tasks track work, comments preserve progress and decisions, and memories hold durable standalone knowledge. Read acta_guide before substantial work with Acta for workflow, memory scope, collaboration and safe-edit guidance. Follow the user's instructions and project conventions. This connection may expose only some capabilities; the guide does not grant access to other tools.`

// The guide and applicable policy are available on every authenticated MCP
// connection. Policy never expands the connection's tool grants.
func (h *Handler) guideTool(server *mcp.Server) {
	no := false
	mcp.AddTool(server, &mcp.Tool{
		Name:        "acta_guide",
		Description: "Read Acta's working guide before substantial work: discover existing tasks, record useful progress and decisions, choose memory scopes, and handle concurrent edits and uncertain writes. Returns maintained Markdown guidance plus site and human-owner policy preferences. No arguments.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		prefs, err := h.management.GuidePreferences(ctx, "")
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: guide.Compose(learn.AgentGuide, prefs).Markdown}}}, nil, nil
	})
}
