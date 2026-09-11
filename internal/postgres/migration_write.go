package postgres

import (
	"acta/internal/migration"
	"context"
	"time"
)

func (t workspaceTx) migrationWrite(ctx context.Context, op string, in migration.Request, now time.Time) (string, error) {
	if e := checkMigrationFields(op, in); e != nil {
		return "", e
	}
	switch in.Kind {
	case "workspace":
		return t.migrationWorkspace(ctx, op, in, now)
	case "task":
		return t.migrationTask(ctx, op, in, now)
	case "comment", "activity":
		return t.migrationActivity(ctx, op, in, now)
	case "memory":
		return t.migrationMemory(ctx, op, in, now)
	case "document":
		return t.migrationDocument(ctx, op, in, now)
	}
	return "", migration.Invalid("kind", "Unsupported kind.")
}
