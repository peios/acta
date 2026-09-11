package postgres

import (
	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/tasks"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const viewColumns = `id::text,name,filters,display,version,board`

func scanView(row pgx.Row) (tasks.View, error) {
	var v tasks.View
	var raw, display []byte
	e := row.Scan(&v.ID, &v.Name, &raw, &display, &v.Version, &v.Board)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, auth.ErrNotFound
	}
	if e != nil {
		return v, e
	}
	e = json.Unmarshal(raw, &v.Filters)
	if e != nil {
		return v, e
	}
	e = json.Unmarshal(display, &v.Display)
	return v, e
}
func (t workspaceTx) TaskViews(ctx context.Context, w string, requested ...string) ([]tasks.View, error) {
	board := "tasks"
	if len(requested) > 0 {
		board = requested[0]
	}
	board, err := tasks.BoardSlug(board)
	if err != nil {
		return nil, err
	}
	{
		inserted, e := t.tx.Exec(ctx, `INSERT INTO task_view_collections(account_id,workspace_id,board) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, t.actor.ID, w, board)
		if e != nil {
			return nil, e
		}
		if inserted.RowsAffected() > 0 {
			if _, e = t.CreateTaskView(ctx, w, "All tasks", tasks.ViewSettings{Board: board, Filters: tasks.ViewFilters{Statuses: []string{}, Assignees: []string{}}, Display: defaultViewDisplay()}); e != nil {
				return nil, e
			}
		}
	}
	rows, e := t.tx.Query(ctx, `SELECT `+viewColumns+` FROM task_views WHERE account_id=$1 AND workspace_id=$2 AND board=$3 ORDER BY position`, t.actor.ID, w, board)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []tasks.View{}
	for rows.Next() {
		v, e := scanView(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (t workspaceTx) CreateTaskView(ctx context.Context, w, name string, settings tasks.ViewSettings) (tasks.View, error) {
	settings, err := tasks.NormalizeViewSettings(settings)
	if err != nil {
		return tasks.View{}, err
	}
	var count int
	if e := t.tx.QueryRow(ctx, `SELECT count(*) FROM task_views WHERE account_id=$1 AND workspace_id=$2 AND board=$3`, t.actor.ID, w, settings.Board).Scan(&count); e != nil {
		return tasks.View{}, e
	}
	if count >= 50 {
		return tasks.View{}, &accounts.FieldError{Field: "name", Message: "You can have up to 50 tabs in a board."}
	}
	raw, e := json.Marshal(settings.Filters)
	if e != nil {
		return tasks.View{}, e
	}
	display, e := json.Marshal(settings.Display)
	if e != nil {
		return tasks.View{}, e
	}
	v, e := scanView(t.tx.QueryRow(ctx, `INSERT INTO task_views(id,account_id,workspace_id,name,filters,display,board,position) VALUES($1,$2,$3,$4,$5,$6,$7,COALESCE((SELECT max(position)+1 FROM task_views WHERE account_id=$2 AND workspace_id=$3 AND board=$7),0)) RETURNING `+viewColumns, uuid.NewString(), t.actor.ID, w, name, raw, display, settings.Board))
	return v, taskViewError(e)
}
func taskViewError(e error) error {
	var pg *pgconn.PgError
	if errors.As(e, &pg) && pg.ConstraintName == "task_view_name" {
		e = &accounts.FieldError{Field: "name", Message: "You already have a tab with this name."}
	}
	return e
}
func (t workspaceTx) SaveTaskView(ctx context.Context, w, id string, version int64, settings tasks.ViewSettings) (tasks.View, error) {
	old, e := scanView(t.tx.QueryRow(ctx, `SELECT `+viewColumns+` FROM task_views WHERE id::text=$1 AND account_id=$2 AND workspace_id=$3 FOR UPDATE`, id, t.actor.ID, w))
	if e != nil {
		return old, e
	}
	if old.Board != settings.Board {
		return old, &accounts.FieldError{Field: "board", Message: "A preset belongs to its original board."}
	}
	raw, e := json.Marshal(settings.Filters)
	if e != nil {
		return old, e
	}
	display, e := json.Marshal(settings.Display)
	if e != nil {
		return old, e
	}
	before, _ := json.Marshal(old.ViewSettings)
	after, _ := json.Marshal(settings)
	// An identical retry after an uncertain response succeeds without another write.
	if string(after) == string(before) {
		return old, nil
	}
	if old.Version != version {
		return old, auth.ErrPermissionsChanged
	}
	return scanView(t.tx.QueryRow(ctx, `UPDATE task_views SET filters=$4,display=$5,version=version+1 WHERE id::text=$1 AND account_id=$2 AND workspace_id=$3 RETURNING `+viewColumns, id, t.actor.ID, w, raw, display))
}

func (t workspaceTx) RenameTaskView(ctx context.Context, w, id string, version int64, name string) (tasks.View, error) {
	old, e := scanView(t.tx.QueryRow(ctx, `SELECT `+viewColumns+` FROM task_views WHERE id::text=$1 AND account_id=$2 AND workspace_id=$3 FOR UPDATE`, id, t.actor.ID, w))
	if e != nil {
		return old, e
	}
	if old.Name == name {
		return old, nil
	}
	if old.Version != version {
		return old, auth.ErrPermissionsChanged
	}
	v, e := scanView(t.tx.QueryRow(ctx, `UPDATE task_views SET name=$4,version=version+1 WHERE id::text=$1 AND account_id=$2 AND workspace_id=$3 RETURNING `+viewColumns, id, t.actor.ID, w, name))
	return v, taskViewError(e)
}
func (t workspaceTx) DeleteTaskView(ctx context.Context, w, id string, version int64) error {
	// Serialize collection changes so concurrent deletes cannot remove the last tab.
	if _, e := t.tx.Exec(ctx, `SELECT 1 FROM task_view_collections WHERE account_id=$1 AND workspace_id=$2 FOR UPDATE`, t.actor.ID, w); e != nil {
		return e
	}
	old, e := scanView(t.tx.QueryRow(ctx, `SELECT `+viewColumns+` FROM task_views WHERE id::text=$1 AND account_id=$2 AND workspace_id=$3 FOR UPDATE`, id, t.actor.ID, w))
	if e != nil {
		return e
	}
	if old.Version != version {
		return auth.ErrPermissionsChanged
	}
	var count int
	if e = t.tx.QueryRow(ctx, `SELECT count(*) FROM task_views WHERE account_id=$1 AND workspace_id=$2 AND board=$3`, t.actor.ID, w, old.Board).Scan(&count); e != nil {
		return e
	}
	if count <= 1 {
		return &accounts.FieldError{Field: "view", Message: "Keep at least one tab in this board."}
	}
	_, e = t.tx.Exec(ctx, `DELETE FROM task_views WHERE id::text=$1 AND account_id=$2 AND workspace_id=$3`, id, t.actor.ID, w)
	return e
}

func defaultViewDisplay() tasks.ViewDisplay {
	d, _ := tasks.NormalizeViewDisplay(tasks.ViewDisplay{})
	return d
}
