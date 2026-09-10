package postgres

import (
	"acta2/internal/accounts"
	"acta2/internal/auth"
	"acta2/internal/memories"
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const memoryColumns = `id::text,scope,COALESCE(workspace_id::text,account_id::text,''),key,summary,content,revision,created_by::text,updated_by::text,created_at,updated_at`

func scanMemory(row pgx.Row) (memories.Memory, error) {
	var m memories.Memory
	e := row.Scan(&m.ID, &m.Scope, &m.ScopeID, &m.Key, &m.Summary, &m.Content, &m.Revision, &m.CreatedBy, &m.UpdatedBy, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = auth.ErrNotFound
	}
	return m, e
}
func (t workspaceTx) MemoryGet(ctx context.Context, id string) (memories.Memory, error) {
	return scanMemory(t.tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM memories WHERE id::text=$1`, id))
}
func (t workspaceTx) MemoryList(ctx context.Context, targets []string, query, key, id string) ([]memories.Memory, error) {
	rows, e := t.tx.Query(ctx, `SELECT id::text,scope,COALESCE(workspace_id::text,account_id::text,''),key,summary,''::text,revision,created_by::text,updated_by::text,created_at,updated_at FROM memories WHERE scope||':'||COALESCE(workspace_id::text,account_id::text,'')=ANY($1) AND ($2='' OR strpos(lower(key||' '||summary||' '||content),lower($2))>0) AND ($4='' OR (key,id::text)>($3,$4)) ORDER BY key,id::text LIMIT 51`, targets, query, key, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []memories.Memory{}
	for rows.Next() {
		m, e := scanMemory(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func memoryError(e error) error {
	var p *pgconn.PgError
	if errors.As(e, &p) && p.ConstraintName == "memory_scope_key" {
		return &accounts.FieldError{Field: "key", Message: "A memory with this key already exists in this scope. Recall it and use its id and revision to update."}
	}
	return e
}
func (t workspaceTx) MemorySave(ctx context.Context, in memories.Save, target string) (memories.Memory, error) {
	var out memories.Memory
	var e error
	if in.ID == "" {
		w, a := "", ""
		if in.Scope == "workspace" {
			w = target
		} else if in.Scope != "site" {
			a = target
		}
		out, e = scanMemory(t.tx.QueryRow(ctx, `INSERT INTO memories(id,scope,workspace_id,account_id,key,summary,content,created_by,updated_by) VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8,$8) RETURNING `+memoryColumns, uuid.NewString(), in.Scope, w, a, in.Key, in.Summary, in.Content, t.actor.ID))
	} else {
		old, readErr := scanMemory(t.tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM memories WHERE id::text=$1 FOR UPDATE`, in.ID))
		if readErr != nil {
			return out, readErr
		}
		if old.Revision != in.Revision {
			return out, memories.ErrConflict
		}
		out, e = scanMemory(t.tx.QueryRow(ctx, `UPDATE memories SET key=$2,summary=$3,content=$4,revision=revision+1,updated_by=$5,updated_at=now() WHERE id::text=$1 RETURNING `+memoryColumns, in.ID, in.Key, in.Summary, in.Content, t.actor.ID))
	}
	return out, memoryError(e)
}
func (t workspaceTx) MemoryDelete(ctx context.Context, id string, revision int64) error {
	result, e := t.tx.Exec(ctx, `DELETE FROM memories WHERE id::text=$1 AND revision=$2`, id, revision)
	if e != nil {
		return e
	}
	if result.RowsAffected() != 1 {
		return memories.ErrConflict
	}
	return nil
}
