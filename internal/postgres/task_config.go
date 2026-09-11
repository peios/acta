package postgres

import (
	"acta/internal/accounts"
	"acta/internal/activity"
	"acta/internal/auth"
	"acta/internal/tasks"
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
)

func (t workspaceTx) TaskConfig(ctx context.Context, w string) (tasks.Config, error) {
	c := tasks.Config{Statuses: []tasks.Status{}, PreviousPrefixes: []string{}}
	e := t.tx.QueryRow(ctx, `SELECT prefix,b.creation_status::text,b.completed_status::text,version,revision,ARRAY(SELECT prefix FROM task_prefixes WHERE workspace_id=$1 AND prefix<>s.prefix ORDER BY prefix) FROM task_settings s JOIN task_boards b ON b.workspace_id=s.workspace_id AND b.slug='tasks' WHERE s.workspace_id=$1`, w).Scan(&c.Prefix, &c.Creation, &c.Completed, &c.Version, &c.Revision, &c.PreviousPrefixes)
	if e != nil {
		return c, e
	}
	boards, e := t.tx.Query(ctx, `SELECT slug,name,creation_status::text,COALESCE(completed_status::text,'') FROM task_boards WHERE workspace_id=$1 ORDER BY position`, w)
	if e != nil {
		return c, e
	}
	c.Boards = []tasks.Board{}
	for boards.Next() {
		var b tasks.Board
		if e = boards.Scan(&b.Slug, &b.Name, &b.Creation, &b.Completed); e != nil {
			boards.Close()
			return c, e
		}
		c.Boards = append(c.Boards, b)
	}
	e = boards.Err()
	boards.Close()
	if e != nil {
		return c, e
	}
	rows, e := t.tx.Query(ctx, `SELECT id::text,name,board FROM task_statuses WHERE workspace_id=$1 ORDER BY board DESC,position,id`, w)
	if e != nil {
		return c, e
	}
	defer rows.Close()
	for rows.Next() {
		var s tasks.Status
		if e = rows.Scan(&s.ID, &s.Name, &s.Board); e != nil {
			return c, e
		}
		c.Statuses = append(c.Statuses, s)
	}
	return c, rows.Err()
}
func taskDBError(e error) error {
	var p *pgconn.PgError
	if errors.As(e, &p) {
		if p.Code == "23505" {
			return &accounts.FieldError{Field: "prefix", Message: "That prefix or status name is already used or reserved."}
		}
		if p.Code == "23503" {
			return &accounts.FieldError{Field: "status_id", Message: "Choose a status in this workspace."}
		}
	}
	return e
}
func (t workspaceTx) taskChanged(ctx context.Context, w string) error {
	_, e := t.tx.Exec(ctx, `UPDATE task_settings SET revision=revision+1 WHERE workspace_id=$1`, w)
	return e
}
func (t workspaceTx) TaskPrefix(ctx context.Context, w, p string, v int64) error {
	r, e := t.tx.Exec(ctx, `UPDATE task_settings SET prefix=$2,version=version+1,revision=revision+1 WHERE workspace_id=$1 AND version=$3`, w, p, v)
	if e != nil {
		return taskDBError(e)
	}
	if r.RowsAffected() != 1 {
		return auth.ErrPermissionsChanged
	}
	return nil
}
func (t workspaceTx) TaskStatuses(ctx context.Context, w string, in tasks.StatusChange) error {
	board, e := tasks.BoardSlug(in.Board)
	if e != nil {
		return e
	}
	c, e := t.TaskConfig(ctx, w)
	if e != nil {
		return e
	}
	if c.Version != in.Version {
		return auth.ErrPermissionsChanged
	}
	keep := map[string]bool{}
	for _, s := range in.Statuses {
		keep[s.ID] = true
	}
	// Rename old rows out of the way to permit atomic swaps of names.
	if _, e = t.tx.Exec(ctx, `UPDATE task_statuses SET name=id::text WHERE workspace_id=$1 AND board=$2`, w, board); e != nil {
		return e
	}
	for i, s := range in.Statuses {
		r, e := t.tx.Exec(ctx, `INSERT INTO task_statuses(id,workspace_id,name,position,board) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,position=EXCLUDED.position WHERE task_statuses.workspace_id=EXCLUDED.workspace_id AND task_statuses.board=EXCLUDED.board`, s.ID, w, strings.TrimSpace(s.Name), i, board)
		if e != nil {
			return taskDBError(e)
		}
		if r.RowsAffected() != 1 {
			return auth.ErrNotFound
		}
	}
	for _, s := range c.Statuses {
		if s.Board != board {
			continue
		}
		if keep[s.ID] {
			continue
		}
		to := in.Replacements[s.ID]
		if !keep[to] {
			return &accounts.FieldError{Field: "statuses", Message: "Choose a replacement for each removed status."}
		}
		var newName string
		for _, candidate := range in.Statuses {
			if candidate.ID == to {
				newName = strings.TrimSpace(candidate.Name)
			}
		}
		rows, err := t.tx.Query(ctx, `SELECT id::text FROM tasks WHERE workspace_id=$1 AND status_id=$2 ORDER BY number`, w, s.ID)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err = t.recordActivity(ctx, w, "task", id, activity.Change{Kind: "task.changed", Field: "status_id", Reason: "status_replaced", Before: activity.Value{Items: []activity.Reference{{ID: s.ID, Label: s.Name}}}, After: activity.Value{Items: []activity.Reference{{ID: to, Label: newName}}}}); err != nil {
				return err
			}
		}

		if _, e = t.tx.Exec(ctx, `UPDATE tasks SET status_id=$3,status_version=status_version+1,updated_at=now() WHERE workspace_id=$1 AND status_id=$2`, w, s.ID, to); e != nil {
			return e
		}
		if _, e = t.tx.Exec(ctx, `DELETE FROM task_statuses WHERE id=$1 AND workspace_id=$2`, s.ID, w); e != nil {
			return e
		}
	}
	_, e = t.tx.Exec(ctx, `UPDATE task_boards SET creation_status=$3,completed_status=NULLIF($4,'')::uuid WHERE workspace_id=$1 AND slug=$2`, w, board, in.Creation, in.Completed)
	if e != nil {
		return taskDBError(e)
	}
	_, e = t.tx.Exec(ctx, `UPDATE task_settings SET version=version+1,revision=revision+1 WHERE workspace_id=$1`, w)
	return taskDBError(e)
}
