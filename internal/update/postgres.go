package update

import (
	"context"
	"errors"
	"time"
)

// Fork a cold snapshot's WAL history before admitting writes. Only timeline
// history files may come from the archive: candidate WAL must never be replayed
// over the exact stopped snapshot. PostgreSQL chooses an unused timeline using
// those history files when archive recovery reaches the end of the local WAL.
const prepareRecovery = `set -eu
cat > /data/acta-update-recovery.conf <<'CONF'
restore_command = 'case "%f" in *.history) pgbackrest --config=/var/lib/acta-backup/config/pgbackrest.conf --stanza=acta archive-get "%f" "%p";; *) exit 1;; esac'
recovery_target = ''
recovery_target_name = ''
recovery_target_time = ''
recovery_target_xid = ''
recovery_target_lsn = ''
recovery_target_timeline = 'current'
recovery_target_action = 'promote'
recovery_end_command = ''
archive_cleanup_command = ''
CONF
line="include_if_exists = 'acta-update-recovery.conf'"
grep -Fxq "$line" /data/postgresql.auto.conf || printf '\n%s\n' "$line" >> /data/postgresql.auto.conf
touch /data/recovery.signal
chmod 600 /data/acta-update-recovery.conf /data/recovery.signal
chown --reference=/data/PG_VERSION /data/acta-update-recovery.conf /data/recovery.signal
sync`

func (d Docker) prepareRecovery(ctx context.Context, j Job, image string) error {
	volume, err := d.volume(ctx)
	if err != nil {
		return err
	}
	if err = d.stopCopy(ctx, j); err != nil {
		return err
	}
	name := d.Config.Project + "-copy-" + j.ID
	if _, err = command(ctx, "run", "--name", name, "--label", "acta.update.job="+j.ID, "--label", "acta.update.installation="+d.Config.Installation, "--network", "none", "--read-only", "--mount", "type=volume,src="+volume+",dst=/data", "--entrypoint", "/bin/sh", image, "-ec", prepareRecovery); err != nil {
		return err
	}
	_, err = command(ctx, "rm", name)
	return err
}

func (d Docker) finishRecovery(ctx context.Context) error {
	for {
		raw, err := d.compose(ctx, "exec", "-T", "-u", "postgres", "db", "psql", "-U", "postgres", "-d", "acta", "-At", "-v", "ON_ERROR_STOP=1", "-c", "SELECT NOT pg_is_in_recovery()")
		if err != nil {
			return err
		}
		if stringTrim(raw) == "t" {
			break
		}
		if stringTrim(raw) != "f" {
			return errors.New("unrecognised PostgreSQL recovery state")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	// Backups must not inherit our restricted, one-time restore command.
	_, err := d.compose(ctx, "exec", "-T", "-u", "postgres", "db", "python3", "-c", `import os; from pathlib import Path; root=Path("/var/lib/postgresql/data"); (root/"acta-update-recovery.conf").unlink(missing_ok=True); fd=os.open(root,os.O_RDONLY|os.O_DIRECTORY); os.fsync(fd); os.close(fd)`)
	return err
}
