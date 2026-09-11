package postgres

import (
	"acta/internal/migration"
	"context"
	"encoding/json"
	"time"
)

func (s *Store) MigrationFind(ctx context.Context, caller string, digest []byte, now time.Time, in migration.Find) (out migration.Page, err error) {
	out.Records = []migration.Record{}
	if in.Offset < 0 || in.Offset > 1000000 || len(in.Query) > 300 {
		return out, migration.Invalid("query", "Invalid query or offset.")
	}
	tx, a, _, err := s.migrationTx(ctx, caller, digest, now)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if in.Kind == "account" {
		rows, e := tx.Query(ctx, `SELECT a.id::text,CASE WHEN p.id IS NULL THEN a.username ELSE p.username||'/'||a.username END,a.display_name,a.parent_id IS NOT NULL,a.pending,a.disabled_at IS NOT NULL FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE $1='' OR position(lower($1) in lower(COALESCE(p.username||'/','')||a.username||' '||COALESCE(a.display_name,'')))>0 ORDER BY a.id LIMIT 51 OFFSET $2`, in.Query, in.Offset)
		if e != nil {
			return out, e
		}
		defer rows.Close()
		for rows.Next() {
			var id, handle string
			var display *string
			var agent, pending, disabled bool
			if e = rows.Scan(&id, &handle, &display, &agent, &pending, &disabled); e != nil {
				return out, e
			}
			raw, _ := json.Marshal(map[string]any{"id": id, "username": handle, "display_name": display, "agent": agent, "pending": pending, "disabled": disabled})
			out.Records = append(out.Records, record("account", id, raw))
		}
		err = rows.Err()
	} else {
		table, filter, label := "", "", ""
		switch in.Kind {
		case "workspace":
			table, filter, label = "workspaces", "true", "name||' '||slug"
		case "task":
			table, filter, label = "tasks", "workspace_id::text=$1", "title"
		case "comment":
			table, filter, label = "task_comments", "task_id::text=$1", "body"
		case "document":
			table, filter, label = "documents", "task_id::text=$1", "id::text"
		case "activity":
			table, filter, label = "activity_entries", "subject_type='task' AND subject_id::text=$1 AND NOT EXISTS(SELECT 1 FROM task_comments c WHERE c.id=activity_entries.id)", "change::text"
		case "memory":
			table, filter, label = "memories", "($1='' OR COALESCE(workspace_id,account_id)::text=$1)", "key||' '||summary"
		default:
			return out, migration.Invalid("kind", "Choose a supported content kind or account.")
		}
		if in.Kind != "workspace" && in.Kind != "memory" && in.Parent == "" {
			return out, migration.Invalid("parent", "Supply the parent UUID.")
		}
		rows, e := tx.Query(ctx, `SELECT id::text FROM `+table+` WHERE (`+filter+`) AND ($1::text IS NOT NULL) AND ($2='' OR position(lower($2) in lower(`+label+`))>0) ORDER BY id LIMIT 51 OFFSET $3`, in.Parent, in.Query, in.Offset)
		if e != nil {
			return out, e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return out, e
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		t := workspaceTx{tx, a}
		for _, id := range ids {
			r, e := t.migrationGet(ctx, in.Kind, id)
			if e != nil {
				return out, e
			}
			out.Records = append(out.Records, r)
		}
	}
	if len(out.Records) > 50 {
		out.More = true
		out.Records = out.Records[:50]
	}
	out.NextOffset = in.Offset + len(out.Records)
	return out, err
}
