package postgres

import (
	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/migration"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"slices"
	"time"
)

func (s *Store) migrationTx(ctx context.Context, caller string, digest []byte, now time.Time) (pgx.Tx, accounts.Account, string, error) {
	tx, a, e := s.managementTx(ctx, caller, digest, "workspace", now)
	if e != nil {
		return nil, a, "", e
	}
	session, e := (securityTx{tx, caller}).Session(ctx, digest, now)
	if e == nil && (session.Kind != "mcp" || !slices.Contains(session.Tools, auth.MCPMigration) || !auth.CanDelegateMigration(a)) {
		e = auth.ErrForbidden
	}
	if e != nil {
		tx.Rollback(ctx)
		return nil, a, "", e
	}
	return tx, a, session.ID, nil
}
func (s *Store) Migrate(ctx context.Context, caller string, digest []byte, now time.Time, op string, in migration.Request) (out migration.Record, err error) {
	tx, a, session, err := s.migrationTx(ctx, caller, digest, now)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	t := workspaceTx{tx, a}
	if op == "get" {
		out, err = t.migrationGet(ctx, in.Kind, in.ID)
		return out, err
	}
	if op != "create" && op != "edit" {
		return out, auth.ErrForbidden
	}
	if in.ActingAs == "" {
		return out, migration.Invalid("acting_as", "Choose an existing user or agent UUID.")
	}
	actor, e := t.Account(ctx, in.ActingAs)
	if e != nil {
		return out, e
	}
	t.actor = actor
	var before migration.Record
	if op == "edit" {
		before, err = t.migrationGet(ctx, in.Kind, in.ID)
		if err != nil {
			return out, err
		}
		if in.Revision == "" || in.Revision != before.Revision {
			return out, migration.ErrConflict
		}
		in.ID = before.ID
	} else if in.ID != "" || in.Revision != "" {
		return out, migration.Invalid("id", "Creation generates UUIDs; omit id and revision.")
	}
	// Transaction-local scope prevents normal operations from inheriting migration behavior.
	ctx = context.WithValue(ctx, migrationContextKey{}, true)
	id, err := t.migrationWrite(ctx, op, in, now)
	if err != nil {
		return out, err
	}
	out, err = t.migrationGet(ctx, in.Kind, id)
	if err != nil {
		return out, err
	}
	var old any
	if before.ID != "" {
		old = before.Data
	}
	_, err = tx.Exec(ctx, `INSERT INTO migration_operations(id,operator_id,session_id,acting_as,operation,kind,target_id,before_data,after_data) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, uuid.NewString(), caller, session, actor.ID, op, in.Kind, id, old, out.Data)
	if err != nil {
		return out, err
	}
	err = tx.Commit(ctx)
	return out, err
}

type migrationContextKey struct{}

func isMigration(ctx context.Context) bool { v, _ := ctx.Value(migrationContextKey{}).(bool); return v }

func record(kind, id string, raw []byte) migration.Record {
	hash := sha256.Sum256(raw)
	return migration.Record{Kind: kind, ID: id, Revision: hex.EncodeToString(hash[:]), Data: json.RawMessage(raw)}
}
func (t workspaceTx) migrationGet(ctx context.Context, kind, id string) (migration.Record, error) {
	var raw []byte
	var e error
	switch kind {
	case "task":
		v, err := t.TaskGet(ctx, id)
		if err != nil {
			return migration.Record{}, err
		}
		id = v.ID
		e = t.tx.QueryRow(ctx, `SELECT to_jsonb(t)||jsonb_build_object('reference',c.prefix||'-'||t.number,'assignees',ARRAY(SELECT account_id FROM task_assignees WHERE task_id=t.id ORDER BY account_id)) FROM tasks t JOIN task_settings c ON c.workspace_id=t.workspace_id WHERE t.id=$1`, id).Scan(&raw)
	case "workspace":
		e = t.tx.QueryRow(ctx, `SELECT to_jsonb(w)||jsonb_build_object('task_settings',to_jsonb(c),'boards',(SELECT jsonb_agg(to_jsonb(b) ORDER BY position) FROM task_boards b WHERE b.workspace_id=w.id),'statuses',(SELECT jsonb_agg(to_jsonb(s) ORDER BY position) FROM task_statuses s WHERE s.workspace_id=w.id)) FROM workspaces w JOIN task_settings c ON c.workspace_id=w.id WHERE w.id::text=$1`, id).Scan(&raw)
	case "comment":
		e = t.tx.QueryRow(ctx, `SELECT to_jsonb(c)-'request_hash'-'request_id'||jsonb_build_object('created_at',f.started_at,'updated_at',f.updated_at) FROM task_comments c JOIN activity_entries f ON f.id=c.id WHERE c.id::text=$1`, id).Scan(&raw)
	case "memory":
		e = t.tx.QueryRow(ctx, `SELECT to_jsonb(m) FROM memories m WHERE id::text=$1`, id).Scan(&raw)
	case "document":
		e = t.tx.QueryRow(ctx, `SELECT to_jsonb(v)||jsonb_build_object('id',d.id,'task_id',d.task_id) FROM documents d JOIN document_versions v ON v.document_id=d.id AND v.revision=d.revision WHERE d.id::text=$1`, id).Scan(&raw)
	case "activity":
		e = t.tx.QueryRow(ctx, `SELECT to_jsonb(f) FROM activity_entries f WHERE id::text=$1 AND NOT EXISTS(SELECT 1 FROM task_comments c WHERE c.id=f.id)`, id).Scan(&raw)
	default:
		return migration.Record{}, migration.Invalid("kind", "Choose workspace, task, comment, memory, document or activity.")
	}
	if errors.Is(e, pgx.ErrNoRows) {
		e = auth.ErrNotFound
	}
	return record(kind, id, raw), e
}
