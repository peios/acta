package backup

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"acta/internal/localstate"
	"github.com/gofrs/flock"
	"github.com/jackc/pgx/v5"
)

type CutoverReport struct {
	PreparedAt      time.Time `json:"prepared_at"`
	RevokedSessions int64     `json:"revoked_sessions"`
	DiscardedAlerts int64     `json:"discarded_alerts"`
	Target          string    `json:"target"`
}

// PrepareCutover changes only a previously verified, stopped recovery target.
// It deliberately does not reconfigure a proxy, start Acta or replace production.
func (e PGBackRest) PrepareCutover(ctx context.Context, target string) (CutoverReport, error) {
	var out CutoverReport
	if !regexp.MustCompile(`^/[a-zA-Z0-9_./-]+$`).MatchString(target) {
		return out, errors.New("invalid recovery target path")
	}
	var verified RestoreReport
	var meta recovery
	if err := ReadJSON(filepath.Join(target, "verified.json"), &verified); err != nil {
		return out, errors.New("target has not passed recovery verification")
	}
	if verified.Target != target || verified.VerifiedAt.IsZero() {
		return out, errors.New("verification record does not match this target")
	}
	if err := ReadJSON(filepath.Join(target, "acta-recovery.json"), &meta); err != nil {
		return out, err
	}
	l := flock.New(filepath.Join(target, "cutover.lock"))
	ok, err := l.TryLock()
	if err != nil {
		return out, err
	}
	if !ok {
		return out, ErrBusy
	}
	defer l.Unlock()
	data := filepath.Join(target, "pgdata")
	if _, err = os.Lstat(filepath.Join(data, "postmaster.pid")); err == nil {
		return out, errors.New("recovery PostgreSQL must be stopped before preparation")
	}
	pgctl := filepath.Join(e.Config.PostgresBin, "pg_ctl")
	options := isolatedOptions(target)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := os.Stat(filepath.Join(data, "postmaster.pid")); err == nil {
			_, _ = command(cleanup, pgctl, []string{"-D", data, "-m", "immediate", "-w", "stop"}, nil)
		}
	}()
	if _, err = command(ctx, pgctl, []string{"-D", data, "-l", filepath.Join(target, "prepare.log"), "-o", options, "-w", "-t", "120", "start"}, nil); err != nil {
		return out, err
	}
	u := url.URL{Scheme: "postgres", User: url.User(meta.DatabaseUser), Path: "/" + meta.Database, RawQuery: url.Values{"host": {filepath.Join(target, "socket")}, "port": {"5432"}, "sslmode": {"disable"}}.Encode()}
	c, err := pgx.Connect(ctx, u.String())
	if err != nil {
		return out, err
	}
	defer c.Close(ctx)
	if _, err = verifyDatabase(ctx, c, meta); err != nil {
		return out, err
	}
	tx, err := c.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	// Browser sessions own CLI/MCP credentials and push subscriptions through
	// foreign keys. Clear every in-flight authentication grant as well.
	for _, table := range []string{"security_flows", "oauth_requests", "device_requests", "setup_grants", "account_links"} {
		if _, err = tx.Exec(ctx, "DELETE FROM "+pgx.Identifier{table}.Sanitize()); err != nil {
			return out, err
		}
	}
	r, err := tx.Exec(ctx, `DELETE FROM browser_sessions`)
	if err != nil {
		return out, err
	}
	out.RevokedSessions = r.RowsAffected()
	if _, err = tx.Exec(ctx, `DELETE FROM push_deliveries`); err != nil {
		return out, err
	}
	r, err = tx.Exec(ctx, `UPDATE notifications SET read_at=clock_timestamp(),resolved_at=clock_timestamp() WHERE read_at IS NULL OR resolved_at IS NULL`)
	if err != nil {
		return out, err
	}
	out.DiscardedAlerts = r.RowsAffected()
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	if err = c.Close(ctx); err != nil {
		return out, err
	}
	if _, err = command(ctx, pgctl, []string{"-D", data, "-m", "fast", "-w", "stop"}, nil); err != nil {
		return out, err
	}
	out.Target = target
	out.PreparedAt = time.Now().UTC()
	if err = localstate.Write(filepath.Join(target, "prepared.json"), out); err != nil {
		return out, err
	}
	return out, nil
}

func isolatedOptions(target string) string {
	return "-c config_file=" + filepath.Join(target, "postgresql.conf") + " -c data_directory=" + filepath.Join(target, "pgdata") + " -c hba_file=" + filepath.Join(target, "pg_hba.conf") + " -c unix_socket_directories=" + filepath.Join(target, "socket") + " -c listen_addresses= -c port=5432 -c external_pid_file= -c archive_mode=off -c archive_command= -c archive_cleanup_command= -c recovery_end_command= -c primary_conninfo= -c primary_slot_name= -c shared_preload_libraries= -c max_logical_replication_workers=0"
}
