package postgres

import (
	"context"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestTaskBoardsUpgradePreservesExistingWorkspace(t *testing.T) {
	raw := os.Getenv("ACTA_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set ACTA_TEST_DATABASE_URL")
	}
	ctx := t.Context()
	conn, e := pgx.Connect(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	schema := pgx.Identifier{"acta_board_upgrade_" + uuid.NewString()}.Sanitize()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = conn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		conn.Close(ctx)
	}()
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, e := conn.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	mustExec("CREATE SCHEMA " + schema)
	mustExec("SET search_path TO " + schema)
	names, e := fs.Glob(migrations, "migrations/*.sql")
	if e != nil {
		t.Fatal(e)
	}
	mustExec("BEGIN")
	for _, name := range names {
		if name >= "migrations/040" {
			break
		}
		sql, e := migrations.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		mustExec(string(sql))
	}
	owner, w, task, view := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	mustExec(`INSERT INTO accounts(id,username) VALUES($1,'board-upgrade')`, owner)
	mustExec(`INSERT INTO workspaces(id,name,slug) VALUES($1,'Board upgrade','board-upgrade')`, w)
	var status string
	if e = conn.QueryRow(ctx, `SELECT creation_status::text FROM task_settings WHERE workspace_id=$1`, w).Scan(&status); e != nil {
		t.Fatal(e)
	}
	// Existing users may already have a primary-board status named Backlog.
	mustExec(`UPDATE task_statuses SET name='Backlog' WHERE id=$1`, status)
	mustExec(`INSERT INTO tasks(id,workspace_id,number,title,status_id,created_by,created_at,updated_at) VALUES($1,$2,1,'Keep me',$3,$4,now(),now())`, task, w, status, owner)
	mustExec(`INSERT INTO task_view_collections(account_id,workspace_id) VALUES($1,$2)`, owner, w)
	mustExec(`INSERT INTO task_views(id,account_id,workspace_id,name,filters,position) VALUES($1,$2,$3,'Original','{}',0)`, view, owner, w)
	mustExec("COMMIT")
	sql, e := migrations.ReadFile("migrations/040_task_boards.sql")
	if e != nil {
		t.Fatal(e)
	}
	mustExec("BEGIN")
	mustExec(string(sql))
	mustExec("COMMIT")
	var savedStatus, board string
	if e = conn.QueryRow(ctx, `SELECT t.status_id::text,s.board FROM tasks t JOIN task_statuses s ON s.id=t.status_id WHERE t.id=$1`, task).Scan(&savedStatus, &board); e != nil {
		t.Fatal(e)
	}
	if savedStatus != status || board != "tasks" {
		t.Fatal("existing task moved", savedStatus, board)
	}
	if e = conn.QueryRow(ctx, `SELECT board FROM task_views WHERE id=$1`, view).Scan(&board); e != nil {
		t.Fatal(e)
	}
	if board != "tasks" {
		t.Fatal("preset moved")
	}
	var n int
	if e = conn.QueryRow(ctx, `SELECT count(*) FROM task_statuses WHERE workspace_id=$1 AND name='Backlog'`, w).Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 2 {
		t.Fatal("status name collision", n)
	}
	mustExec(`INSERT INTO workspaces(id,name,slug) VALUES($1,'After','after')`, uuid.NewString())
}
