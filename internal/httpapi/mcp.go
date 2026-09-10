package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"acta2/internal/accounts"
	"acta2/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpIdentity struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName *string `json:"display_name"`
}

// Each request exposes the guide and applicable policy plus tools allowed by connection grants.
// Account state and grants are reloaded before dispatch, including tools/call.
// Protocol sessions are stateless; durable authorization belongs to Acta.
func identityServer(account accounts.Account, grants []string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "Acta", Version: "0.0.0"}, &mcp.ServerOptions{Instructions: agentInstructions, Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}}})
	if slices.Contains(grants, auth.MCPIdentityGrant) {
		no := false
		mcp.AddTool(s, &mcp.Tool{Name: "whoami", Description: "Return the current Acta account's ID, username and display name.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no}},
			func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, mcpIdentity, error) {
				return nil, mcpIdentity{ID: account.ID, Username: account.Handle(), DisplayName: account.DisplayName}, nil
			})
	}
	return s
}
func (h *Handler) mcp(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && !h.config.Origins[origin] {
		writeError(w, 403, "invalid_origin", "This origin is not allowed.", nil)
		return
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	raw := ""
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		raw = parts[1]
	}
	ctx, a, grants, err := h.security.MCPContext(r.Context(), raw, h.mcpResource())
	if err != nil {
		if errors.Is(err, auth.ErrMFARequired) {
			writeError(w, 403, "mfa_required", "Set up the MFA required by your administrator in Acta, then reconnect.", nil)
			return
		}
		if !errors.Is(err, auth.ErrUnauthenticated) && !errors.Is(err, auth.ErrNotFound) && !errors.Is(err, auth.ErrAccountDisabled) {
			failure(w, err)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+h.config.PublicURL+`/.well-known/oauth-protected-resource/mcp", scope="`+auth.OAuthScope+`"`)
		writeError(w, 401, "unauthenticated", "Connect to Acta to authorize MCP access.", nil)
		return
	}
	limit := int64(256 * 1024)
	if slices.Contains(grants, auth.MCPMigration) {
		limit = 28 * 1024 * 1024
	} // A base64-encoded document (20 MiB) plus metadata.
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	server := identityServer(a, grants)
	h.guideTool(server)
	h.taskTools(server, grants)
	h.documentTools(server, grants)
	h.memoryTools(server, grants)
	h.migrationTools(server, grants)
	r = r.WithContext(ctx)
	mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}).ServeHTTP(w, r)
}
