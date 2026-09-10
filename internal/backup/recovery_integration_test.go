package backup

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Run only in the isolated network-disabled PostgreSQL test container. This
// exercises real pgBackRest and age, not mocked CLI responses.
func TestPGBackRestRecovery(t *testing.T) {
	path := os.Getenv("ACTA_BACKUP_E2E_CONFIG")
	if path == "" {
		t.Skip("set ACTA_BACKUP_E2E_CONFIG in the isolated backup test container")
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	e := PGBackRest{Config: c}
	p := Policy{Destination: c.Destinations[0].ID, Mode: "continuous", ArchiveAgeMinutes: 5, RetainFull: 2}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	seed, err := e.connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise actual account-security decryption by the recorded release.
	keyRaw, _ := Secret(c.SecurityKeyFile)
	key, _ := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(keyRaw)))
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)
	encrypted := aead.Seal(nonce, nonce, []byte(`{}`), []byte("account:10000000-0000-0000-0000-000000000001"))
	if _, err = seed.Exec(ctx, `UPDATE accounts SET security_data=$1 WHERE username='backup-review'`, encrypted); err != nil {
		t.Fatal(err)
	}
	_, err = seed.Exec(ctx, `DELETE FROM recovery_probe WHERE id>1;
 DELETE FROM browser_sessions;
 INSERT INTO browser_sessions(token_hash,account_id,created_at,last_seen_at,expires_at,kind) SELECT sha256(convert_to(k,'UTF8')),'10000000-0000-0000-0000-000000000001',now(),now(),now()+interval '1 day',k FROM unnest(ARRAY['browser','cli','mcp']) AS k;
 INSERT INTO oauth_clients VALUES('review','{}',now()) ON CONFLICT DO NOTHING;
 INSERT INTO oauth_tokens SELECT sha256(convert_to('token-'||kind,'UTF8')),id,'review','acta','access',now()+interval '1 day',false FROM browser_sessions WHERE kind='mcp';
 DELETE FROM notifications;
 INSERT INTO notifications(id,owner_id,task_id,notice_key,kind,title) VALUES('10000000-0000-0000-0000-000000000006','10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000003','review','task/changed','Old alert');
 INSERT INTO push_subscriptions(id,owner_id,session_id,endpoint,p256dh,auth) SELECT gen_random_uuid(),account_id,id,'https://invalid.test','test','test' FROM browser_sessions WHERE kind='browser';
 INSERT INTO push_deliveries(subscription_id,notification_id,revision) SELECT id,'10000000-0000-0000-0000-000000000006',1 FROM push_subscriptions;
 `)
	seed.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}
	point, err := e.Backup(ctx, p, uuid.NewString(), true)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Verify(ctx, p, point.Label); err != nil {
		t.Fatal(err)
	}
	wrong := filepath.Join(c.RestoreRoot, "wrong-"+uuid.NewString())
	if _, err = e.Restore(ctx, p, point.Label, "/missing/recovery-key", wrong, ""); err == nil {
		t.Fatal("restored without recovery key")
	}
	if _, err = os.Stat(wrong); !os.IsNotExist(err) {
		t.Fatal("created target before checking key")
	}
	target := filepath.Join(c.RestoreRoot, "ok-"+uuid.NewString())
	// Source PostgreSQL and source-only metadata are deliberately unavailable.
	sourceCTL := filepath.Join(c.PostgresBin, "pg_ctl")
	if _, err = command(ctx, sourceCTL, []string{"-D", "/review/source", "-m", "fast", "-w", "stop"}, nil); err != nil {
		t.Fatal(err)
	}
	sourceStarted := false
	defer func() {
		if !sourceStarted {
			command(context.Background(), sourceCTL, []string{"-D", "/review/source", "-l", "/review/source.log", "-w", "start"}, nil)
		}
	}()
	offline := e
	offline.Config.DatabaseURLFile = "/missing/database-url"
	offline.Config.SecurityKeyFile = "/missing/source-key"
	offline.Config.ReleaseFile = "/missing/source-release"
	if _, err = offline.Catalog(ctx, p); err != nil {
		t.Fatal("offline catalog", err)
	}
	report, err := offline.Restore(ctx, p, point.Label, c.RecoveryIdentityFile, target, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = command(ctx, sourceCTL, []string{"-D", "/review/source", "-l", "/review/source.log", "-w", "start"}, nil); err != nil {
		t.Fatal(err)
	}
	sourceStarted = true
	if report.Rows["tasks"] < 1 || report.DocumentsChecked < 1 {
		t.Fatal("did not restore seeded task and document", report)
	}
	if _, err = e.Restore(ctx, p, point.Label, c.RecoveryIdentityFile, target, ""); err == nil {
		t.Fatal("overwrote existing restore")
	}
	conn, err := e.connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err = conn.Exec(ctx, `INSERT INTO recovery_probe VALUES(2) ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	var at time.Time
	if err = conn.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `INSERT INTO recovery_probe VALUES(3) ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `SELECT pg_switch_wal()`); err != nil {
		t.Fatal(err)
	}
	if _, err = e.run(ctx, p, "check"); err != nil {
		t.Fatal(err)
	}
	pitr := filepath.Join(c.RestoreRoot, "time-"+uuid.NewString())
	if _, err = e.Restore(ctx, p, point.Label, c.RecoveryIdentityFile, pitr, at.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(pitr, "pgdata")
	pgctl := filepath.Join(c.PostgresBin, "pg_ctl")
	if _, err = command(ctx, pgctl, []string{"-D", data, "-l", filepath.Join(pitr, "probe.log"), "-o", "-c config_file=" + filepath.Join(pitr, "postgresql.conf"), "-w", "start"}, nil); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := os.Stat(filepath.Join(data, "postmaster.pid")); err == nil {
			command(context.Background(), pgctl, []string{"-D", data, "-m", "immediate", "-w", "stop"}, nil)
		}
	}()
	pc, err := pgx.ParseConfig("")
	if err != nil {
		t.Fatal(err)
	}
	pc.Host = filepath.Join(pitr, "socket")
	pc.Database = "acta"
	pc.User = "postgres"
	pc.TLSConfig = nil
	pc.Fallbacks = nil
	probe, err := pgx.ConnectConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int32
	err = probe.QueryRow(ctx, `SELECT array_agg(id ORDER BY id) FROM recovery_probe`).Scan(&ids)
	probe.Close(ctx)
	if err != nil || !reflect.DeepEqual(ids, []int32{1, 2}) {
		t.Fatalf("PITR replayed the wrong transactions: %v %v", ids, err)
	}
	if _, err = command(ctx, pgctl, []string{"-D", data, "-m", "fast", "-w", "stop"}, nil); err != nil {
		t.Fatal(err)
	}
	cutover, err := e.PrepareCutover(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if cutover.RevokedSessions != 3 || cutover.DiscardedAlerts != 1 {
		t.Fatal("cutover did not revoke restored access and alerts", cutover)
	}
	cutoverAgain, err := e.PrepareCutover(ctx, target)
	if err != nil || cutoverAgain.RevokedSessions != 0 || cutoverAgain.DiscardedAlerts != 0 {
		t.Fatal("preparation not repeatable", cutoverAgain, err)
	}
	// Check all cascading credentials and deliveries, and preserve user work.
	td := filepath.Join(target, "pgdata")
	if _, err = command(ctx, pgctl, []string{"-D", td, "-l", filepath.Join(target, "assert.log"), "-o", isolatedOptions(target), "-w", "start"}, nil); err != nil {
		t.Fatal(err)
	}
	pc.Host = filepath.Join(target, "socket")
	restored, err := pgx.ConnectConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	var stale, taskCount int
	err = restored.QueryRow(ctx, `SELECT (SELECT count(*) FROM browser_sessions)+(SELECT count(*) FROM oauth_tokens)+(SELECT count(*) FROM push_subscriptions)+(SELECT count(*) FROM push_deliveries)+(SELECT count(*) FROM notifications WHERE read_at IS NULL OR resolved_at IS NULL),(SELECT count(*) FROM tasks)`).Scan(&stale, &taskCount)
	restored.Close(ctx)
	command(ctx, pgctl, []string{"-D", td, "-m", "fast", "-w", "stop"}, nil)
	if err != nil || stale != 0 || taskCount != 1 {
		t.Fatal("cutover database assertions", stale, taskCount, err)
	}
	// Leave the verified targets for the shell-level assertions in the test
	// runner, including checking exactly which transactions were replayed.
	t.Logf("snapshot=%s pitr=%s set=%s", target, pitr, point.Label)
	if err = e.Drill(ctx, p, point.Label); err != nil {
		t.Fatal(err)
	}
	r, err := e.repository(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range r.Points {
		if v.Label == point.Label {
			found = v.RestoredAt != nil && v.IntegrityAt != nil
		}
	}
	if !found {
		t.Fatal("restore evidence was not persisted in repository")
	}
}

// Corrupt only the disposable local repository, restoring its exact bytes even
// on failure. A successful pgBackRest process alone must not imply integrity.
func TestPGBackRestRejectsCorruption(t *testing.T) {
	config := os.Getenv("ACTA_BACKUP_E2E_CONFIG")
	if config == "" {
		t.Skip("isolated container only")
	}
	c, err := LoadConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	e := PGBackRest{Config: c}
	p := Policy{Destination: c.Destinations[0].ID}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	r, err := e.repository(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	var label string
	for _, point := range r.Points {
		if point.Complete {
			label = point.Label
			break
		}
	}
	if label == "" {
		t.Fatal("run recovery test first")
	}
	root := os.Getenv("ACTA_BACKUP_E2E_REPOSITORY")
	if root == "" {
		t.Skip("set disposable local repository path")
	}
	var victim string
	err = filepath.WalkDir(filepath.Join(root, "backup", c.Stanza, label, "pg_data", "base"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			victim = path
			return fs.SkipAll
		}
		return nil
	})
	if err != nil || victim == "" {
		t.Fatal(err)
	}
	original, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	defer os.WriteFile(victim, original, 0600)
	if err = os.WriteFile(victim, []byte("deliberate isolated corruption"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = e.Verify(ctx, p, label); err == nil {
		t.Fatal("corrupt repository marked verified")
	}
}

func TestPGBackRestRetentionProtectsRestoredChain(t *testing.T) {
	path := os.Getenv("ACTA_BACKUP_E2E_CONFIG")
	if path == "" {
		t.Skip("isolated container only")
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	e := PGBackRest{Config: c}
	p := Policy{Destination: c.Destinations[0].ID, RetainFull: 2}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	labels := []string{}
	for i := 0; i < 3; i++ {
		point, err := e.Backup(ctx, p, uuid.NewString(), true)
		if err != nil {
			t.Fatal(err)
		}
		if err = e.Verify(ctx, p, point.Label); err != nil {
			t.Fatal(err)
		}
		labels = append(labels, point.Label)
		if i == 0 {
			if err = e.Drill(ctx, p, point.Label); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = e.Expire(ctx, p); err != nil {
		t.Fatal(err)
	}
	r, err := e.repository(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, point := range r.Points {
		if point.Label == labels[0] {
			found = true
		}
	}
	if !found {
		t.Fatal("retention deleted the last restored chain")
	}
	if err = e.Drill(ctx, p, labels[2]); err != nil {
		t.Fatal(err)
	}
	if err = e.Expire(ctx, p); err != nil {
		t.Fatal(err)
	}
	r, err = e.repository(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	full := 0
	for _, point := range r.Points {
		if point.Type == "full" {
			full++
		}
		if point.Label == labels[0] {
			t.Fatal("retention did not advance after newer restore proof")
		}
	}
	if full != 2 {
		t.Fatalf("retained %d full sets, wanted 2", full)
	}
	if err = e.Drill(ctx, p, labels[2]); err != nil {
		t.Fatal("retained backup could no longer restore", err)
	}
}
