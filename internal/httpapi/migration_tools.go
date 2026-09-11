package httpapi

import (
	"acta/internal/auth"
	"acta/internal/migration"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"slices"
)

// RawMessage is a byte slice to the SDK schema generator. Decode only at this
// transport boundary so MCP advertises an object instead of an array of bytes.
type migrationToolRecord struct {
	Kind     string         `json:"kind"`
	ID       string         `json:"id"`
	Revision string         `json:"revision"`
	Data     map[string]any `json:"data"`
}

func migrationOutput(r migration.Record, err error) (migrationToolRecord, error) {
	out := migrationToolRecord{Kind: r.Kind, ID: r.ID, Revision: r.Revision}
	if err == nil {
		err = json.Unmarshal(r.Data, &out.Data)
	}
	return out, err
}

type migrationToolPage struct {
	Records    []migrationToolRecord `json:"records"`
	More       bool                  `json:"more"`
	NextOffset int                   `json:"next_offset"`
}

func (h *Handler) migrationTools(s *mcp.Server, grants []string) {
	if !slices.Contains(grants, auth.MCPMigration) {
		return
	}
	no, yes := false, true
	read := func(name, description string) *mcp.Tool {
		return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no}}
	}
	addTaskTool(s, read("migration_schema", "Read the accepted fields, attribution rules and workflow before using Migration Assistant. This connection has explicit superuser-delegated content authority."), func(ctx context.Context, in struct{}) (map[string]any, error) {
		return map[string]any{"fields": migration.Fields, "instructions": migration.Instructions}, nil
	})
	addTaskTool(s, read("migration_find", "Find existing content or historical account identities before migrating. kind: account/workspace/task/comment/memory/document/activity. parent is the workspace UUID for tasks, task UUID for comments/documents/activity, optional scope UUID for memories. query is a substring. Follow next_offset while more is true; records include opaque revisions for editing."), func(ctx context.Context, in migration.Find) (migrationToolPage, error) {
		page, err := h.management.MigrationFind(ctx, in)
		out := migrationToolPage{Records: []migrationToolRecord{}, More: page.More, NextOffset: page.NextOffset}
		if err != nil {
			return out, err
		}
		for _, r := range page.Records {
			v, e := migrationOutput(r, nil)
			if e != nil {
				return out, e
			}
			out.Records = append(out.Records, v)
		}
		return out, nil
	})
	addTaskTool(s, read("migration_get", "Inspect a content record and its opaque revision, including protected metadata. kind: workspace/task/comment/memory/document/activity. id is a UUID or task reference."), func(ctx context.Context, in struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}) (migrationToolRecord, error) {
		return migrationOutput(h.management.Migrate(ctx, "get", migration.Request{Kind: in.Kind, ID: in.ID}))
	})
	type createInput struct {
		Kind     string         `json:"kind"`
		ActingAs string         `json:"acting_as"`
		Fields   map[string]any `json:"fields"`
	}
	type editInput struct {
		Kind     string         `json:"kind"`
		ID       string         `json:"id"`
		Revision string         `json:"revision"`
		ActingAs string         `json:"acting_as"`
		Fields   map[string]any `json:"fields"`
	}
	fields := func(in map[string]any) map[string]json.RawMessage {
		out := map[string]json.RawMessage{}
		for k, v := range in {
			out[k], _ = json.Marshal(v)
		}
		return out
	}
	addTaskTool(s, &mcp.Tool{Name: "migration_create", Description: "Create content with historical attribution, dates and optional explicit task reference. Read migration_schema first. acting_as is an existing user/agent UUID. UUIDs are generated; inspect before retrying an uncertain result. Notifications are suppressed and the actual operator is audited. This cannot create accounts or grant permissions.", Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, OpenWorldHint: &no}}, func(ctx context.Context, in createInput) (migrationToolRecord, error) {
		return migrationOutput(h.management.Migrate(ctx, "create", migration.Request{Kind: in.Kind, ActingAs: in.ActingAs, Fields: fields(in.Fields)}))
	})
	addTaskTool(s, &mcp.Tool{Name: "migration_edit", Description: "Edit content, including protected historical metadata, using the opaque revision from migration_get/find. Read migration_schema first. Omitted fields are preserved; only documented nullable fields accept null. acting_as controls attribution, not authentication. Notifications are suppressed and the actual operator and before/after values are audited.", Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, OpenWorldHint: &no}}, func(ctx context.Context, in editInput) (migrationToolRecord, error) {
		return migrationOutput(h.management.Migrate(ctx, "edit", migration.Request{Kind: in.Kind, ID: in.ID, Revision: in.Revision, ActingAs: in.ActingAs, Fields: fields(in.Fields)}))
	})
}
