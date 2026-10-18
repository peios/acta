package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"acta/internal/localstate"
)

func TestRecoveryCleanupWithoutServer(t *testing.T) {
	// No PID file: do not invoke a nonexistent pg_ctl or manufacture a failure.
	if err := stopRecoveryPostgres("/nonexistent/pg_ctl", t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryCleanupReportsInspectionFailure(t *testing.T) {
	data := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(data, nil, 0600); err != nil {
		t.Fatal(err)
	}
	err := stopRecoveryPostgres("/nonexistent/pg_ctl", data)
	if err == nil || !strings.Contains(err.Error(), "inspect recovery PostgreSQL") {
		t.Fatalf("lost PID inspection failure: %v", err)
	}
}

func TestCutoverReportsStartAndCleanupFailures(t *testing.T) {
	for _, stopFails := range []bool{false, true} {
		name := "cleanup succeeds"
		if stopFails {
			name = "cleanup fails"
		}
		t.Run(name, func(t *testing.T) {
			target := t.TempDir()
			data := filepath.Join(target, "pgdata")
			if err := os.Mkdir(data, 0700); err != nil {
				t.Fatal(err)
			}
			for path, value := range map[string]any{
				"verified.json":      RestoreReport{Target: target, VerifiedAt: time.Now()},
				"acta-recovery.json": recovery{},
			} {
				if err := localstate.Write(filepath.Join(target, path), value); err != nil {
					t.Fatal(err)
				}
			}
			stopExit := "exit 0"
			if stopFails {
				stopExit = "exit 2"
			}
			// Fake only pg_ctl, never start a database. A partially failed start leaves
			// a PID marker; cleanup must still run and report its own failure.
			script := `#!/bin/sh
set -eu
data=$2
for action do :; done
case "$action" in
start) touch "$data/postmaster.pid"; exit 1 ;;
stop) touch "$data/cleanup-attempted"; ` + stopExit + ` ;;
*) exit 99 ;;
esac
`
			if err := os.WriteFile(filepath.Join(target, "pg_ctl"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			engine := PGBackRest{Config: Config{PostgresBin: target}}
			_, err := engine.PrepareCutover(context.Background(), target)
			if err == nil || !strings.Contains(err.Error(), "backup command failed") {
				t.Fatalf("lost start failure: %v", err)
			}
			if strings.Contains(err.Error(), "stop recovery PostgreSQL") != stopFails {
				t.Fatalf("unexpected cleanup result: %v", err)
			}
			if _, err := os.Stat(filepath.Join(data, "cleanup-attempted")); err != nil {
				t.Fatalf("cleanup did not run: %v", err)
			}
		})
	}
}
