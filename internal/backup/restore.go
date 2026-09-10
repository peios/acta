package backup

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"acta2/internal/localstate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type RestoreReport struct {
	Backup           string           `json:"backup"`
	Target           string           `json:"target"`
	RequestedTime    string           `json:"requested_time,omitempty"`
	VerifiedAt       time.Time        `json:"verified_at"`
	Release          Release          `json:"release"`
	Rows             map[string]int64 `json:"rows"`
	DocumentsChecked int              `json:"documents_checked"`
}

// Restore never opens the source database. Repository configuration and an age
// identity are sufficient even when the original host and Acta are gone.
// target must not exist, including an empty directory or symlink.
func (e PGBackRest) Restore(ctx context.Context, p Policy, label, identity, target, targetTime string) (RestoreReport, error) {
	var report RestoreReport
	if len(filepath.Join(target, "socket")) > 90 {
		return report, errors.New("restore path is too long for an isolated PostgreSQL socket")
	}
	if !filepath.IsAbs(target) {
		return report, errors.New("restore target must be absolute")
	}
	if !regexp.MustCompile(`^/[a-zA-Z0-9_./-]+$`).MatchString(target) {
		return report, errors.New("restore target must use only letters, digits, slash, dot, underscore and hyphen")
	}
	if targetTime != "" {
		if _, err := time.Parse(time.RFC3339Nano, targetTime); err != nil {
			return report, errors.New("recovery time must be RFC3339 with a timezone")
		}
	}
	meta, err := e.openRecovery(ctx, p, label, identity)
	if err != nil {
		return report, err
	}
	executable, err := e.releaseExecutable(meta.Release)
	if err != nil {
		return report, err
	}
	if err = os.Mkdir(target, 0700); err != nil {
		return report, fmt.Errorf("restore requires a new target directory: %w", err)
	}
	// Failed targets are intentionally retained. A retry must use a fresh name.
	if err = localstate.Write(filepath.Join(target, "acta-recovery.json"), meta); err != nil {
		return report, err
	}
	data := filepath.Join(target, "pgdata")
	args := []string{"--set=" + label, "--pg1-path=" + data, "--tablespace-map-all=" + filepath.Join(target, "tablespaces"), "--archive-mode=off", "--target-action=promote"}
	if targetTime == "" {
		args = append(args, "--type=immediate")
	} else {
		at, _ := time.Parse(time.RFC3339Nano, targetTime)
		args = append(args, "--type=time", "--target="+at.UTC().Format("2006-01-02 15:04:05.999999999-07:00"))
	}
	if _, err = e.run(ctx, p, "restore", args...); err != nil {
		return report, err
	}
	// Run a network-isolated verification PostgreSQL with an explicit socket
	// and data path. Restored config cannot override these command-line values.
	socket := filepath.Join(target, "socket")
	if len(socket) > 90 {
		return report, errors.New("restore path is too long for an isolated PostgreSQL socket")
	}
	if err = os.Mkdir(socket, 0700); err != nil {
		return report, err
	}
	hba := filepath.Join(target, "pg_hba.conf")
	if err = os.WriteFile(hba, []byte("local all all trust\n"), 0600); err != nil {
		return report, err
	}
	config := filepath.Join(target, "postgresql.conf")
	// No shared_preload_libraries, cron or logical replication workers run in
	// the verification instance. WAL replay can still load required extensions;
	// run recovery in the documented isolated network/container boundary too.
	settings := fmt.Sprintf("data_directory = %s\nhba_file = %s\nlisten_addresses = ''\nunix_socket_directories = %s\nport = 5432\narchive_mode = off\narchive_command = ''\narchive_cleanup_command = ''\nrecovery_end_command = ''\nprimary_conninfo = ''\nprimary_slot_name = ''\nshared_preload_libraries = ''\nmax_logical_replication_workers = 0\n", pgQuote(data), pgQuote(hba), pgQuote(socket))
	for _, name := range []string{"max_connections", "max_worker_processes", "max_wal_senders", "max_prepared_transactions", "max_locks_per_transaction"} {
		value, ok := meta.Settings[name]
		if !ok {
			return report, errors.New("recovery record is missing required PostgreSQL replay settings")
		}
		if _, err := strconv.Atoi(value); err != nil {
			return report, errors.New("invalid replay setting")
		}
		settings += name + " = " + value + "\n"
	}
	if err = os.WriteFile(config, []byte(settings), 0600); err != nil {
		return report, err
	}
	// pg_ctl parses -o as a shell string internally. Forbid metacharacters in
	// the generated path rather than interpolating a user-controlled shell.
	if strings.ContainsAny(target, "'\"`$\\\n\r\t ") {
		return report, errors.New("restore target may not contain whitespace or shell metacharacters")
	}
	pgctl := filepath.Join(e.Config.PostgresBin, "pg_ctl")
	options := isolatedOptions(target)
	stopped := false
	defer func() {
		if !stopped {
			cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _ = command(cleanup, pgctl, []string{"-D", data, "-m", "immediate", "-w", "stop"}, nil)
		}
	}()
	if _, err = command(ctx, pgctl, []string{"-D", data, "-l", filepath.Join(target, "postgres.log"), "-o", options, "-w", "-t", "120", "start"}, nil); err != nil {
		return report, err
	}
	localURL := url.URL{Scheme: "postgres", User: url.User(meta.DatabaseUser), Path: "/" + meta.Database, RawQuery: url.Values{"host": {socket}, "port": {"5432"}, "sslmode": {"disable"}}.Encode()}
	cc, err := pgx.ParseConfig(localURL.String())
	if err != nil {
		return report, err
	}
	cc.Host = socket
	cc.Port = 5432
	cc.Database = meta.Database
	cc.User = meta.DatabaseUser
	cc.Password = ""
	cc.TLSConfig = nil
	cc.Fallbacks = nil
	c, err := pgx.ConnectConfig(ctx, cc)
	if err != nil {
		return report, errors.New("restored PostgreSQL did not accept a local verification connection")
	}
	report, err = verifyDatabase(ctx, c, meta)
	_ = c.Close(ctx)
	if err != nil {
		return report, err
	}
	keyPath := filepath.Join(target, "security.key")
	if err = os.WriteFile(keyPath, []byte(meta.SecurityKey), 0600); err != nil {
		return report, err
	}
	if _, err = commandEnv(ctx, executable, []string{"-verify-recovery"}, nil, []string{"ACTA_DATABASE_URL=" + cc.ConnString(), "ACTA_SECURITY_KEY_FILE=" + keyPath, "ACTA_PUBLIC_URL=" + meta.Release.PublicURL, "ACTA_DEV_ORIGIN=", "ACTA_RECOVERY_MODE=true", "ACTA_BACKUP_SOCKET=", "ACTA_BACKUP_TOKEN_FILE="}); err != nil {
		return report, errors.New("the recorded Acta release failed its read-only recovery check")
	}
	if _, err = command(ctx, pgctl, []string{"-D", data, "-m", "fast", "-w", "stop"}, nil); err != nil {
		return report, err
	}
	stopped = true
	report.Backup = label
	report.Target = target
	report.RequestedTime = targetTime
	report.VerifiedAt = time.Now().UTC()
	report.Release = meta.Release
	if err = localstate.Write(filepath.Join(target, "verified.json"), report); err != nil {
		return report, err
	}
	return report, nil
}

func pgQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func verifyDatabase(ctx context.Context, c *pgx.Conn, meta recovery) (RestoreReport, error) {
	r := RestoreReport{Rows: map[string]int64{}}
	var system string
	var version int
	if err := c.QueryRow(ctx, `SELECT system_identifier::text,current_setting('server_version_num')::int FROM pg_control_system()`).Scan(&system, &version); err != nil {
		return r, err
	}
	if system != meta.SystemID || version/10000 != meta.PostgresVersion/10000 {
		return r, errors.New("restored cluster identity or PostgreSQL major version differs from recovery record")
	}
	key, err := base64.RawStdEncoding.DecodeString(meta.SecurityKey)
	if err != nil || len(key) != 32 {
		return r, errors.New("recovery record contains an invalid security key")
	}
	var fingerprint []byte
	if err = c.QueryRow(ctx, `SELECT fingerprint FROM security_key WHERE singleton`).Scan(&fingerprint); err != nil {
		return r, err
	}
	sum := sha256.Sum256(key)
	if hex.EncodeToString(fingerprint) != hex.EncodeToString(sum[:]) {
		return r, errors.New("restored database does not match recovery security key")
	}
	migrations := map[string]string{}
	rows, err := c.Query(ctx, `SELECT name,checksum FROM schema_migrations ORDER BY name`)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var name, sum string
		if err = rows.Scan(&name, &sum); err != nil {
			rows.Close()
			return r, err
		}
		migrations[name] = sum
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	if !reflect.DeepEqual(meta.Migrations, migrations) {
		return r, errors.New("restored migration set differs from the backup release; use the matching recovery record for the selected time")
	}
	for _, table := range []string{"accounts", "tasks", "task_comments", "documents", "document_versions", "document_files", "provider_threads", "provider_thread_frames", "thread_conversation_items"} {
		var n int64
		if err = c.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&n); err != nil {
			return r, err
		}
		r.Rows[table] = n
	}
	rows, err = c.Query(ctx, `SELECT v.sha256,v.size,f.content FROM document_versions v LEFT JOIN document_files f USING(file_id)`)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var expected string
		var size int64
		var raw []byte
		if err = rows.Scan(&expected, &size, &raw); err != nil {
			return r, err
		}
		digest := sha256.Sum256(raw)
		if raw == nil || int64(len(raw)) != size || hex.EncodeToString(digest[:]) != expected {
			return r, errors.New("restored document bytes failed size or SHA-256 verification")
		}
		r.DocumentsChecked++
	}
	return r, rows.Err()
}

func (e PGBackRest) Drill(ctx context.Context, p Policy, label string) error {
	if e.Config.RecoveryIdentityFile == "" {
		return errors.New("restore identity has not been configured")
	}
	if err := os.MkdirAll(e.Config.RestoreRoot, 0700); err != nil {
		return err
	}
	target := filepath.Join(e.Config.RestoreRoot, "drill-"+uuid.NewString())
	_, err := e.Restore(ctx, p, label, e.Config.RecoveryIdentityFile, target, "")
	if err != nil {
		return err
	}
	if _, err = e.run(ctx, p, "annotate", "--set="+label, "--annotation=acta-restored="+time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	// The target was created by this invocation and its server was stopped.
	// Keep the small verification report outside it before removing the data.
	raw, err := os.ReadFile(filepath.Join(target, "verified.json"))
	if err != nil {
		return err
	}
	var report RestoreReport
	if err = json.Unmarshal(raw, &report); err != nil {
		return err
	}
	if err = localstate.Write(filepath.Join(e.Config.StateDir, "last-drill.json"), report); err != nil {
		return err
	}
	return os.RemoveAll(target)
}
