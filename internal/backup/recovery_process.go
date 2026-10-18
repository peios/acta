package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// stopRecoveryPostgres also covers partially failed starts. A missing PID file
// means no recovery server needs stopping. Cleanup has its own bounded lifetime
// so cancellation of the operation cannot prevent releasing its child process.
func stopRecoveryPostgres(pgctl, data string) error {
	if _, err := os.Stat(filepath.Join(data, "postmaster.pid")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect recovery PostgreSQL before cleanup: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := command(ctx, pgctl, []string{"-D", data, "-m", "immediate", "-w", "stop"}, nil); err != nil {
		return fmt.Errorf("stop recovery PostgreSQL: %w", err)
	}
	return nil
}
