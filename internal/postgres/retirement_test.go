package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// An installation with execution history must still upgrade without losing its
// task inbox, queued task pushes, account identities, or migration checksums.
func TestRetireExecutionUpgrade(t *testing.T) {
	raw := os.Getenv("ACTA_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set ACTA_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := t.Context()
	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	schema := "acta_retirement_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE SCHEMA ` + pgx.Identifier{schema}.Sanitize())
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`); err != nil {
			t.Error(err)
		}
		conn.Close(cleanup)
	})
	exec(`SET search_path TO ` + pgx.Identifier{schema}.Sanitize())
	exec(`CREATE TABLE schema_migrations (name text PRIMARY KEY, checksum text NOT NULL)`)
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name == "migrations/043_retire_execution.sql" {
			continue
		}
		sql, err := migrations.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		exec(string(sql))
		exec(`INSERT INTO schema_migrations VALUES($1,$2)`, name, fmt.Sprintf("%x", sha256.Sum256(sql)))
	}
	exec(`INSERT INTO accounts(id,username,direct_permissions) VALUES
 ('10000000-0000-0000-0000-000000000001','owner',ARRAY['site.superuser']);
 INSERT INTO accounts(id,username,parent_id) VALUES
 ('10000000-0000-0000-0000-000000000002','helper','10000000-0000-0000-0000-000000000001');
 INSERT INTO workspaces(id,name,slug) VALUES('10000000-0000-0000-0000-000000000003','Keep','keep');
 INSERT INTO tasks(id,workspace_id,number,title,status_id,created_by,created_at,updated_at)
 SELECT '10000000-0000-0000-0000-000000000004',workspace_id,1,'Keep task',creation_status,'10000000-0000-0000-0000-000000000001',now(),now()
 FROM task_boards WHERE workspace_id='10000000-0000-0000-0000-000000000003' AND slug='tasks';
 INSERT INTO provider_threads(id,owner_id,descriptor) VALUES('10000000-0000-0000-0000-000000000005','10000000-0000-0000-0000-000000000001','{}');
 INSERT INTO notifications(id,owner_id,thread_id,run_id,notice_key,kind,title) VALUES
 ('10000000-0000-0000-0000-000000000006','10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000005','10000000-0000-0000-0000-000000000005','old','finished','Retire');
 INSERT INTO notifications(id,owner_id,task_id,notice_key,kind,title) VALUES
 ('10000000-0000-0000-0000-000000000007','10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000004','task','task','Keep notice');
 INSERT INTO browser_sessions(token_hash,id,account_id,created_at,last_seen_at,expires_at)
 VALUES(decode(repeat('01',32),'hex'),'10000000-0000-0000-0000-000000000008','10000000-0000-0000-0000-000000000001',now(),now(),now()+interval '1 day');
 INSERT INTO push_subscriptions(id,owner_id,session_id,endpoint,p256dh,auth) VALUES
 ('10000000-0000-0000-0000-000000000009','10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000008','https://push.example/upgrade','key','auth');
 INSERT INTO push_deliveries(subscription_id,notification_id,revision)
 SELECT '10000000-0000-0000-0000-000000000009',id,revision FROM notifications;`)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	store, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, table := range []string{"provider_threads", "provider_thread_frames", "provider_thread_commands", "thread_conversation_items", "code_hosts"} {
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Errorf("retired table remains: %s", table)
		}
	}
	for table, want := range map[string]int{"accounts": 2, "tasks": 1, "notifications": 1, "push_deliveries": 1, "schema_migrations": len(names)} {
		var count int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Errorf("%s: got %d rows, want %d", table, count, want)
		}
	}
	inbox, err := store.Notifications(ctx, "10000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if inbox.Total != 1 || len(inbox.Items) != 1 || inbox.Items[0].Title != "Keep notice" {
		t.Fatalf("task inbox lost: %+v", inbox)
	}
	// Opening again exercises checksum verification and skips the applied removal.
	if err := store.migrate(ctx); err != nil {
		t.Fatal(err)
	}
}
