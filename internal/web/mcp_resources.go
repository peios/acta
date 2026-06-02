package web

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/peios/acta/internal/store"
)

// registerMCPResources mounts the read-only context resources: the conventions
// guide (acta://guide) and one live board snapshot per workspace
// (acta://workspace/{slug}). Because the server is rebuilt per request, the
// per-workspace set is always current.
func (h *handlers) registerMCPResources(ctx context.Context, srv *mcp.Server) {
	srv.AddResource(&mcp.Resource{
		URI:         "acta://guide",
		Name:        "guide",
		Title:       "Using Acta",
		Description: "How this Acta instance is modeled and how an agent should work in it. Read before creating or changing items.",
		MIMEType:    "text/markdown",
	}, h.mcpGuideResource)

	list, err := h.workspaces.List(ctx)
	if err != nil {
		slog.Error("mcp: list workspaces for resources", "err", err)
		return
	}
	for _, ws := range list {
		uri := "acta://workspace/" + ws.Slug
		srv.AddResource(&mcp.Resource{
			URI:         uri,
			Name:        "workspace:" + ws.Slug,
			Title:       ws.Name + " board",
			Description: "Live snapshot of the " + ws.Name + " board: its status lanes and the open items in each.",
			MIMEType:    "text/markdown",
		}, h.mcpWorkspaceResource(ws.Slug, uri))
	}
}

func (h *handlers) mcpGuideResource(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	body, err := h.mcpcfg.EffectiveGuide(ctx)
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI:      "acta://guide",
		MIMEType: "text/markdown",
		Text:     body,
	}}}, nil
}

func (h *handlers) mcpWorkspaceResource(slug, uri string) mcp.ResourceHandler {
	return func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		md, err := h.mcpWorkspaceSnapshot(ctx, slug)
		if err != nil {
			return nil, mcpErr(err)
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: "text/markdown",
			Text:     md,
		}}}, nil
	}
}

// mcpWorkspaceSnapshot renders a workspace's board as markdown: every status
// lane in order (so the reader learns the real status vocabulary) with the open
// top-level items under it.
func (h *handlers) mcpWorkspaceSnapshot(ctx context.Context, slug string) (string, error) {
	ws, err := h.mcpWorkspace(ctx, slug)
	if err != nil {
		return "", err
	}
	statuses, err := h.board.Statuses(ctx, ws.ID)
	if err != nil {
		return "", err
	}
	items, err := h.board.Items(ctx, ws.ID)
	if err != nil {
		return "", err
	}
	_, userName, err := h.nameMaps(ctx, ws.ID)
	if err != nil {
		return "", err
	}

	byStatus := make(map[string][]store.Item, len(statuses))
	for _, it := range items {
		byStatus[it.StatusID] = append(byStatus[it.StatusID], it)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", ws.Name)
	fmt.Fprintf(&b, "%d open item(s) across %d statuses. Address items by id; address this board by slug %q.\n", len(items), len(statuses), ws.Slug)

	for _, s := range statuses {
		lane := byStatus[s.ID]
		fmt.Fprintf(&b, "\n## %s (%d)\n", s.Name, len(lane))
		if len(lane) == 0 {
			b.WriteString("_none_\n")
			continue
		}
		for _, it := range lane {
			who := "unassigned"
			if it.AssigneeID != "" {
				if n := userName[it.AssigneeID]; n != "" {
					who = n
				}
			}
			marker := ""
			if it.IsMilestone {
				marker = "◆ "
			}
			fmt.Fprintf(&b, "- %s%s — %s · `%s`\n", marker, it.Title, who, it.ID)
		}
	}
	return b.String(), nil
}

// registerMCPPrompts mounts every user-defined prompt as an MCP prompt (a slash
// command in clients). The body's {{arg}} placeholders are filled from the
// arguments the client passes at invocation.
func (h *handlers) registerMCPPrompts(ctx context.Context, srv *mcp.Server) {
	prompts, err := h.mcpcfg.Prompts(ctx)
	if err != nil {
		slog.Error("mcp: list prompts", "err", err)
		return
	}
	for _, p := range prompts {
		var args []*mcp.PromptArgument
		for _, a := range p.Arguments {
			args = append(args, &mcp.PromptArgument{
				Name:        a.Name,
				Description: a.Description,
				Required:    a.Required,
			})
		}
		body := p.Body
		desc := p.Description
		srv.AddPrompt(&mcp.Prompt{
			Name:        p.Name,
			Title:       p.Title,
			Description: p.Description,
			Arguments:   args,
		}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			var fill map[string]string
			if req != nil && req.Params != nil {
				fill = req.Params.Arguments
			}
			return &mcp.GetPromptResult{
				Description: desc,
				Messages: []*mcp.PromptMessage{{
					Role:    mcp.Role("user"),
					Content: &mcp.TextContent{Text: substituteArgs(body, fill)},
				}},
			}, nil
		})
	}
}

var argPlaceholderRe = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// substituteArgs replaces {{name}} placeholders in a prompt body with the values
// the client supplied. An unsupplied placeholder collapses to empty.
func substituteArgs(body string, args map[string]string) string {
	return argPlaceholderRe.ReplaceAllStringFunc(body, func(m string) string {
		name := argPlaceholderRe.FindStringSubmatch(m)[1]
		return args[name]
	})
}
