package postgres

import (
	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/tasks"
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

// Progress counts active tasks targeted directly at the release; a task is
// finished when it holds its own board's completed status.
const releaseQuery = `SELECT r.id::text,r.workspace_id::text,r.name,r.codename,r.state,r.description,r.version,r.created_at,r.updated_at,
 count(t.id),count(t.id) FILTER (WHERE t.status_id=b.completed_status)
 FROM task_releases r LEFT JOIN tasks t ON t.release_id=r.id AND t.archived_at IS NULL
 LEFT JOIN task_statuses s ON s.id=t.status_id LEFT JOIN task_boards b ON b.workspace_id=s.workspace_id AND b.slug=s.board
 WHERE r.workspace_id=$1 AND ($2='' OR r.id::text=$2) GROUP BY r.id`

func scanRelease(row pgx.Row) (tasks.Release, error) {
	var r tasks.Release
	e := row.Scan(&r.ID, &r.WorkspaceID, &r.Name, &r.Codename, &r.State, &r.Description, &r.Version, &r.CreatedAt, &r.UpdatedAt, &r.Total, &r.Finished)
	if errors.Is(e, pgx.ErrNoRows) {
		e = auth.ErrNotFound
	}
	return r, e
}

// TaskReleases returns the workspace's releases in natural name order.
func (t workspaceTx) TaskReleases(ctx context.Context, w string) ([]tasks.Release, error) {
	rows, e := t.tx.Query(ctx, releaseQuery, w, "")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []tasks.Release{}
	for rows.Next() {
		r, e := scanRelease(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	tasks.SortReleases(out)
	return out, nil
}
func (t workspaceTx) TaskRelease(ctx context.Context, w, id string) (tasks.Release, error) {
	if _, e := uuid.Parse(id); e != nil {
		return tasks.Release{}, auth.ErrNotFound
	}
	return scanRelease(t.tx.QueryRow(ctx, releaseQuery, w, id))
}
func (t workspaceTx) TaskReleaseCreate(ctx context.Context, w string, in tasks.ReleaseCreate) (tasks.Release, error) {
	id := uuid.NewString()
	now := time.Now().UTC()
	_, e := t.tx.Exec(ctx, `INSERT INTO task_releases(id,workspace_id,name,codename,state,description,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8)`, id, w, in.Name, in.Codename, in.State, in.Description, t.actor.ID, now)
	if e != nil {
		return tasks.Release{}, releaseDBError(e)
	}
	if e = t.taskChanged(ctx, w); e != nil {
		return tasks.Release{}, e
	}
	return t.TaskRelease(ctx, w, id)
}

// TaskReleaseSave stores next if the release is still at version.
func (t workspaceTx) TaskReleaseSave(ctx context.Context, next tasks.Release, version int64) (tasks.Release, error) {
	r, e := t.tx.Exec(ctx, `UPDATE task_releases SET name=$3,codename=$4,state=$5,description=$6,version=version+1,updated_at=now() WHERE workspace_id=$1 AND id=$2 AND version=$7`, next.WorkspaceID, next.ID, next.Name, next.Codename, next.State, next.Description, version)
	if e != nil {
		return tasks.Release{}, releaseDBError(e)
	}
	if r.RowsAffected() != 1 {
		return tasks.Release{}, tasks.ErrReleaseConflict
	}
	if e = t.taskChanged(ctx, next.WorkspaceID); e != nil {
		return tasks.Release{}, e
	}
	return t.TaskRelease(ctx, next.WorkspaceID, next.ID)
}
func releaseDBError(e error) error {
	var p *pgconn.PgError
	if errors.As(e, &p) && p.Code == "23505" {
		return &accounts.FieldError{Field: "name", Message: "Another release in this workspace already uses this name."}
	}
	return e
}
