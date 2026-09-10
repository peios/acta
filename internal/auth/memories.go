package auth

import (
	"acta2/internal/accounts"
	"acta2/internal/memories"
	ws "acta2/internal/workspaces"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"reflect"
	"strings"
	"unicode/utf8"
)

type MemoryTx interface {
	MemoryGet(context.Context, string) (memories.Memory, error)
	MemoryList(context.Context, []string, string, string, string) ([]memories.Memory, error)
	MemorySave(context.Context, memories.Save, string) (memories.Memory, error)
	MemoryDelete(context.Context, string, int64) error
}

func memoryScope(ctx context.Context, tx WorkspaceTx, scope, ref, agent string, write bool) (string, error) {
	a := tx.Actor()
	switch scope {
	case "site":
		if ref != "" {
			return "", &accounts.FieldError{Field: "workspace", Message: "Supply workspace only for workspace-scoped memories."}
		}
		if agent != "" {
			return "", &accounts.FieldError{Field: "agent_id", Message: "Supply agent_id only for agent-scoped memories."}
		}
		if !accounts.CheckPermission(a, accounts.ReadSiteMemories) {
			if write {
				return "", ErrForbidden
			}
			return "", ErrNotFound
		}
		if write && !accounts.CheckPermission(a, accounts.WriteSiteMemories) {
			return "", ErrForbidden
		}
		return "", nil
	case "workspace":
		if agent != "" {
			return "", &accounts.FieldError{Field: "agent_id", Message: "Supply agent_id only for agent-scoped memories."}
		}
		if strings.TrimSpace(ref) == "" {
			return "", &accounts.FieldError{Field: "workspace", Message: "Supply a workspace UUID or slug."}
		}
		_, err := uuid.Parse(ref)
		w, access, e := workspaceAccess(ctx, tx, ref, err != nil)
		if e != nil {
			return "", e
		}
		if write && !ws.Contains(access.Permissions, ws.WriteMemories) {
			return "", ErrForbidden
		}
		return w.ID, nil
	case "user":
		if ref != "" {
			return "", &accounts.FieldError{Field: "workspace", Message: "Supply workspace only for workspace-scoped memories."}
		}
		if agent != "" {
			return "", &accounts.FieldError{Field: "agent_id", Message: "Supply agent_id only for agent-scoped memories."}
		}
		if a.IsAgent() {
			if write && !accounts.CheckPermission(a, accounts.WriteUserMemories) {
				return "", ErrForbidden
			}
			return *a.ParentID, nil
		}
		return a.ID, nil
	case "agent":
		if ref != "" {
			return "", &accounts.FieldError{Field: "workspace", Message: "Supply workspace only for workspace-scoped memories."}
		}
		if a.IsAgent() {
			if agent != "" && agent != a.ID {
				return "", ErrNotFound
			}
			return a.ID, nil
		}
		target, e := tx.Account(ctx, agent)
		if e != nil {
			return "", e
		}
		if !target.IsAgent() || *target.ParentID != a.ID {
			return "", ErrNotFound
		}
		return target.ID, nil
	}
	return "", &accounts.FieldError{Field: "scope", Message: "Choose site, workspace, user or agent."}
}
func memoryAccess(ctx context.Context, tx WorkspaceTx, m memories.Memory, write bool) error {
	ref, agent := "", ""
	if m.Scope == "workspace" {
		ref = m.ScopeID
	}
	if m.Scope == "agent" {
		agent = m.ScopeID
	}
	target, e := memoryScope(ctx, tx, m.Scope, ref, agent, write)
	if e == nil && target != m.ScopeID {
		return ErrNotFound
	}
	return e
}
func decorateMemory(ctx context.Context, tx WorkspaceTx, m *memories.Memory) {
	m.CanWrite = memoryAccess(ctx, tx, *m, true) == nil
}
func (m *Management) GetMemory(ctx context.Context, token, id string) (memories.Memory, error) {
	var out memories.Memory
	e := m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		var e error
		out, e = tx.MemoryGet(ctx, id)
		if e != nil {
			return e
		}
		if e = memoryAccess(ctx, tx, out, false); e != nil {
			return e
		}
		decorateMemory(ctx, tx, &out)
		return nil
	})
	return out, e
}
func (m *Management) SaveMemory(ctx context.Context, token string, in memories.Save) (memories.Memory, error) {
	var out memories.Memory
	if e := memories.Validate(&in); e != nil {
		return out, e
	}
	e := m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		ref := ""
		if in.Workspace != nil {
			ref = *in.Workspace
		}
		target, e := memoryScope(ctx, tx, in.Scope, ref, in.AgentID, true)
		if e != nil {
			return e
		}
		if in.ID != "" {
			old, e := tx.MemoryGet(ctx, in.ID)
			if e != nil {
				return e
			}
			if e = memoryAccess(ctx, tx, old, true); e != nil {
				return e
			}
			if old.Scope != in.Scope || old.ScopeID != target {
				return &accounts.FieldError{Field: "scope", Message: "Updates cannot change a memory's scope."}
			}
		}
		out, e = tx.MemorySave(ctx, in, target)
		if e == nil {
			out.CanWrite = true
		}
		return e
	})
	return out, e
}
func (m *Management) DeleteMemory(ctx context.Context, token, id string, revision int64) error {
	return m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		old, e := tx.MemoryGet(ctx, id)
		if e != nil {
			return e
		}
		if e = memoryAccess(ctx, tx, old, true); e != nil {
			return e
		}
		return tx.MemoryDelete(ctx, id, revision)
	})
}

type memoryCursor struct {
	Targets        []string
	Query, Key, ID string
}

func (m *Management) RecallMemories(ctx context.Context, token string, in memories.Recall) (memories.Page, error) {
	out := memories.Page{Memories: []memories.Memory{}}
	if len(in.Query) > 500 || len(in.Cursor) > 4096 || !utf8.ValidString(in.Query) || strings.ContainsRune(in.Query, 0) {
		return out, &accounts.FieldError{Field: "query", Message: "Query or cursor is too long."}
	}
	e := m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		targets := []string{}
		for _, scope := range []string{"site", "user", "agent"} {
			if scope == "agent" && !tx.Actor().IsAgent() && in.AgentID == "" {
				continue
			}
			agent := ""
			if scope == "agent" {
				agent = in.AgentID
			}
			target, e := memoryScope(ctx, tx, scope, "", agent, false)
			if e != nil {
				if scope == "site" && errors.Is(e, ErrNotFound) {
					continue
				}
				return e
			}
			targets = append(targets, scope+":"+target)
		}
		if in.Workspace != nil {
			target, e := memoryScope(ctx, tx, "workspace", *in.Workspace, "", false)
			if e != nil {
				return e
			}
			targets = append(targets, "workspace:"+target)
		}
		c := memoryCursor{Targets: targets, Query: strings.TrimSpace(in.Query)}
		if in.Cursor != "" {
			raw, e := base64.RawURLEncoding.DecodeString(in.Cursor)
			var prior memoryCursor
			if e != nil || json.Unmarshal(raw, &prior) != nil || !reflect.DeepEqual(prior.Targets, c.Targets) || prior.Query != c.Query {
				return &accounts.FieldError{Field: "cursor", Message: "Use the cursor with the same workspace, agent and query."}
			}
			c = prior
		}
		rows, e := tx.MemoryList(ctx, targets, c.Query, c.Key, c.ID)
		if e != nil {
			return e
		}
		if len(rows) > 50 {
			rows = rows[:50]
			last := rows[len(rows)-1]
			c.Key = last.Key
			c.ID = last.ID
			raw, _ := json.Marshal(c)
			out.Cursor = base64.RawURLEncoding.EncodeToString(raw)
		}
		for i := range rows {
			decorateMemory(ctx, tx, &rows[i])
		}
		out.Memories = rows
		return nil
	})
	return out, e
}
