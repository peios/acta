package web

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/peios/acta/internal/memory"
	"github.com/peios/acta/internal/store"
)

// --- MCP memory tools ---
//
// Five tools over the shared memory service: recall (the cross-scope index),
// get, save (upsert by name, replace/append), edit (surgical), delete. Scopes
// resolve from the calling principal — agent = me, user = my owner, site =
// global — or from a workspace/project slug. There are no permissions in Acta
// yet, so any principal can read and write any scope.

// mcpMemory is one memory as the MCP tools return it.
type mcpMemory struct {
	ID        string `json:"id"`
	Scope     string `json:"scope"`
	Name      string `json:"name"`
	Summary   string `json:"summary,omitempty"`
	Body      string `json:"body,omitempty"` // omitted from the recall index unless include_bodies
	UpdatedBy string `json:"updated_by,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func (h *handlers) mcpMemoryView(ctx context.Context, m store.Memory, includeBody bool) mcpMemory {
	v := mcpMemory{
		ID:        m.ID,
		Scope:     m.Scope,
		Name:      m.Name,
		Summary:   m.Summary,
		UpdatedBy: h.authorName(ctx, m.UpdatedBy),
		UpdatedAt: m.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if includeBody {
		v.Body = m.Body
	}
	return v
}

// mcpMemoryScope resolves a scope name + optional workspace/project slug into the
// (scope, scopeID) pair the store keys on. agent/user/site derive from the
// caller's identity; workspace/project need a slug.
func (h *handlers) mcpMemoryScope(ctx context.Context, scopeIn, workspace, project string) (string, string, error) {
	p := principalFrom(ctx)
	if p == nil {
		return "", "", errors.New("not authenticated")
	}
	switch strings.ToLower(strings.TrimSpace(scopeIn)) {
	case store.ScopeAgent:
		return store.ScopeAgent, p.ID, nil
	case store.ScopeUser:
		owner := p.ID
		if u, err := h.board.User(ctx, p.ID); err == nil && u.AgentOfID != "" {
			owner = u.AgentOfID // an agent's user scope is its human owner's
		}
		return store.ScopeUser, owner, nil
	case store.ScopeSite:
		return store.ScopeSite, "", nil
	case store.ScopeWorkspace:
		ws, err := h.mcpWorkspace(ctx, workspace)
		if err != nil {
			return "", "", err
		}
		return store.ScopeWorkspace, ws.ID, nil
	case store.ScopeProject:
		ws, err := h.mcpWorkspace(ctx, workspace)
		if err != nil {
			return "", "", err
		}
		pid, perr := h.projectIDBySlug(ctx, ws.ID, project)
		if perr != nil {
			return "", "", fmt.Errorf("project not found: %s", project)
		}
		return store.ScopeProject, pid, nil
	default:
		return "", "", fmt.Errorf("unknown scope %q (want agent, user, site, workspace, or project)", scopeIn)
	}
}

type memoryRecallInput struct {
	Scopes        []string `json:"scopes,omitempty" jsonschema:"limit to these scopes (agent, user, site, workspace, project); default: all visible"`
	Workspace     string   `json:"workspace,omitempty" jsonschema:"workspace slug — include this workspace's memories (and enables project scope)"`
	Project       string   `json:"project,omitempty" jsonschema:"project slug within the workspace — include this project's memories"`
	Query         string   `json:"query,omitempty" jsonschema:"case-insensitive substring filter over name, summary, and body"`
	IncludeBodies bool     `json:"include_bodies,omitempty" jsonschema:"include full markdown bodies (default false: names + summaries only)"`
}

type memoryRecallOutput struct {
	Memories []mcpMemory `json:"memories"`
}

func (h *handlers) mcpMemoryRecall(ctx context.Context, _ *mcp.CallToolRequest, in memoryRecallInput) (*mcp.CallToolResult, memoryRecallOutput, error) {
	p := principalFrom(ctx)
	if p == nil {
		return nil, memoryRecallOutput{}, errors.New("not authenticated")
	}
	explicit := len(in.Scopes) > 0
	want := func(s string) bool { return !explicit || containsFold(in.Scopes, s) }

	type target struct{ scope, id string }
	var targets []target
	if want(store.ScopeAgent) {
		targets = append(targets, target{store.ScopeAgent, p.ID})
	}
	if want(store.ScopeUser) {
		owner := p.ID
		if u, err := h.board.User(ctx, p.ID); err == nil && u.AgentOfID != "" {
			owner = u.AgentOfID
		}
		targets = append(targets, target{store.ScopeUser, owner})
	}
	if want(store.ScopeSite) {
		targets = append(targets, target{store.ScopeSite, ""})
	}
	if want(store.ScopeWorkspace) && strings.TrimSpace(in.Workspace) != "" {
		ws, err := h.mcpWorkspace(ctx, in.Workspace)
		if err != nil {
			return nil, memoryRecallOutput{}, err
		}
		targets = append(targets, target{store.ScopeWorkspace, ws.ID})
		if want(store.ScopeProject) && strings.TrimSpace(in.Project) != "" {
			pid, perr := h.projectIDBySlug(ctx, ws.ID, in.Project)
			if perr != nil {
				return nil, memoryRecallOutput{}, fmt.Errorf("project not found: %s", in.Project)
			}
			targets = append(targets, target{store.ScopeProject, pid})
		}
	}

	q := strings.ToLower(strings.TrimSpace(in.Query))
	out := memoryRecallOutput{Memories: []mcpMemory{}}
	for _, t := range targets {
		mems, err := h.memories.List(ctx, t.scope, t.id)
		if err != nil {
			return nil, memoryRecallOutput{}, mcpErr(err)
		}
		for _, m := range mems {
			if q != "" && !memoryMatches(m, q) {
				continue
			}
			out.Memories = append(out.Memories, h.mcpMemoryView(ctx, m, in.IncludeBodies))
		}
	}
	return &mcp.CallToolResult{}, out, nil
}

type memoryGetInput struct {
	Scope     string `json:"scope,omitempty" jsonschema:"agent, user, site, workspace, or project (omit if using id)"`
	Name      string `json:"name,omitempty" jsonschema:"the memory's name within the scope"`
	ID        string `json:"id,omitempty" jsonschema:"fetch by id instead of scope+name"`
	Workspace string `json:"workspace,omitempty" jsonschema:"workspace slug (required for workspace/project scope)"`
	Project   string `json:"project,omitempty" jsonschema:"project slug (required for project scope)"`
}

func (h *handlers) mcpMemoryGet(ctx context.Context, _ *mcp.CallToolRequest, in memoryGetInput) (*mcp.CallToolResult, mcpMemory, error) {
	if strings.TrimSpace(in.ID) != "" {
		m, err := h.memories.Get(ctx, in.ID)
		if err != nil {
			return nil, mcpMemory{}, mcpErr(err)
		}
		return &mcp.CallToolResult{}, h.mcpMemoryView(ctx, m, true), nil
	}
	scope, scopeID, err := h.mcpMemoryScope(ctx, in.Scope, in.Workspace, in.Project)
	if err != nil {
		return nil, mcpMemory{}, err
	}
	m, found, err := h.memories.ByName(ctx, scope, scopeID, in.Name)
	if err != nil {
		return nil, mcpMemory{}, mcpErr(err)
	}
	if !found {
		return nil, mcpMemory{}, fmt.Errorf("no memory named %q in %s scope", in.Name, scope)
	}
	return &mcp.CallToolResult{}, h.mcpMemoryView(ctx, m, true), nil
}

type memorySaveInput struct {
	Scope     string `json:"scope" jsonschema:"agent, user, site, workspace, or project"`
	Name      string `json:"name" jsonschema:"short label / filename, the key within the scope (upsert)"`
	Body      string `json:"body" jsonschema:"the markdown content"`
	Summary   string `json:"summary,omitempty" jsonschema:"one-line description shown in the recall index"`
	Mode      string `json:"mode,omitempty" jsonschema:"replace (default: overwrite the body) or append (add to the end)"`
	Workspace string `json:"workspace,omitempty" jsonschema:"workspace slug (required for workspace/project scope)"`
	Project   string `json:"project,omitempty" jsonschema:"project slug (required for project scope)"`
}

func (h *handlers) mcpMemorySave(ctx context.Context, _ *mcp.CallToolRequest, in memorySaveInput) (*mcp.CallToolResult, mcpMemory, error) {
	scope, scopeID, err := h.mcpMemoryScope(ctx, in.Scope, in.Workspace, in.Project)
	if err != nil {
		return nil, mcpMemory{}, err
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = memory.SaveReplace
	}
	if mode != memory.SaveReplace && mode != memory.SaveAppend {
		return nil, mcpMemory{}, fmt.Errorf("invalid mode %q (want replace or append)", in.Mode)
	}
	m, err := h.memories.Save(ctx, scope, scopeID, in.Name, in.Summary, in.Body, mode, principalFrom(ctx).ID)
	if err != nil {
		if errors.Is(err, memory.ErrInvalid) {
			return nil, mcpMemory{}, errors.New("invalid name, summary, or body (check lengths)")
		}
		return nil, mcpMemory{}, mcpErr(err)
	}
	return &mcp.CallToolResult{}, h.mcpMemoryView(ctx, m, true), nil
}

type memoryEditInput struct {
	Scope      string `json:"scope" jsonschema:"agent, user, site, workspace, or project"`
	Name       string `json:"name" jsonschema:"the memory to edit"`
	OldString  string `json:"old_string" jsonschema:"exact substring to replace; must occur once unless replace_all"`
	NewString  string `json:"new_string" jsonschema:"replacement text"`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"replace every occurrence instead of requiring a unique match"`
	Workspace  string `json:"workspace,omitempty" jsonschema:"workspace slug (required for workspace/project scope)"`
	Project    string `json:"project,omitempty" jsonschema:"project slug (required for project scope)"`
}

func (h *handlers) mcpMemoryEdit(ctx context.Context, _ *mcp.CallToolRequest, in memoryEditInput) (*mcp.CallToolResult, mcpMemory, error) {
	scope, scopeID, err := h.mcpMemoryScope(ctx, in.Scope, in.Workspace, in.Project)
	if err != nil {
		return nil, mcpMemory{}, err
	}
	m, err := h.memories.Edit(ctx, scope, scopeID, in.Name, in.OldString, in.NewString, in.ReplaceAll, principalFrom(ctx).ID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrMemoryNotFound):
			return nil, mcpMemory{}, fmt.Errorf("no memory named %q in %s scope", in.Name, scope)
		case errors.Is(err, memory.ErrEditNoMatch):
			return nil, mcpMemory{}, errors.New("old_string not found in the memory body")
		case errors.Is(err, memory.ErrEditNotUnique):
			return nil, mcpMemory{}, errors.New("old_string occurs more than once — pass replace_all or use a longer, unique string")
		case errors.Is(err, memory.ErrInvalid):
			return nil, mcpMemory{}, errors.New("the result exceeds the size limit")
		default:
			return nil, mcpMemory{}, mcpErr(err)
		}
	}
	return &mcp.CallToolResult{}, h.mcpMemoryView(ctx, m, true), nil
}

type memoryDeleteInput struct {
	Scope     string `json:"scope,omitempty" jsonschema:"agent, user, site, workspace, or project (omit if using id)"`
	Name      string `json:"name,omitempty" jsonschema:"the memory's name within the scope"`
	ID        string `json:"id,omitempty" jsonschema:"delete by id instead of scope+name"`
	Workspace string `json:"workspace,omitempty" jsonschema:"workspace slug (required for workspace/project scope)"`
	Project   string `json:"project,omitempty" jsonschema:"project slug (required for project scope)"`
}

type memoryDeleteOutput struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
}

func (h *handlers) mcpMemoryDelete(ctx context.Context, _ *mcp.CallToolRequest, in memoryDeleteInput) (*mcp.CallToolResult, memoryDeleteOutput, error) {
	var m store.Memory
	if strings.TrimSpace(in.ID) != "" {
		got, err := h.memories.Get(ctx, in.ID)
		if err != nil {
			return nil, memoryDeleteOutput{}, mcpErr(err)
		}
		m = got
	} else {
		scope, scopeID, err := h.mcpMemoryScope(ctx, in.Scope, in.Workspace, in.Project)
		if err != nil {
			return nil, memoryDeleteOutput{}, err
		}
		got, found, err := h.memories.ByName(ctx, scope, scopeID, in.Name)
		if err != nil {
			return nil, memoryDeleteOutput{}, mcpErr(err)
		}
		if !found {
			return nil, memoryDeleteOutput{}, fmt.Errorf("no memory named %q in %s scope", in.Name, scope)
		}
		m = got
	}
	if err := h.memories.Delete(ctx, m.ID); err != nil && !errors.Is(err, store.ErrMemoryNotFound) {
		return nil, memoryDeleteOutput{}, mcpErr(err)
	}
	return &mcp.CallToolResult{}, memoryDeleteOutput{Deleted: true, ID: m.ID, Name: m.Name}, nil
}

func memoryMatches(m store.Memory, lowerQuery string) bool {
	return strings.Contains(strings.ToLower(m.Name), lowerQuery) ||
		strings.Contains(strings.ToLower(m.Summary), lowerQuery) ||
		strings.Contains(strings.ToLower(m.Body), lowerQuery)
}

func containsFold(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.EqualFold(strings.TrimSpace(s), needle) {
			return true
		}
	}
	return false
}
