package auth

import (
	"acta/internal/accounts"
	"acta/internal/migration"
	"context"
	"time"
)

const MCPMigration = "migration.assistant"

// Delegation remains bounded by the live human superuser who owns the connection.
func CanDelegateMigration(a accounts.Account) bool {
	if a.IsAgent() {
		return a.Owner != nil && a.Owner.Available() && accounts.CheckPermission(*a.Owner, accounts.Superuser)
	}
	return a.Available() && accounts.CheckPermission(a, accounts.Superuser)
}

type MigrationStore interface {
	Migrate(context.Context, string, []byte, time.Time, string, migration.Request) (migration.Record, error)
	MigrationFind(context.Context, string, []byte, time.Time, migration.Find) (migration.Page, error)
}

func (m *Management) Migrate(ctx context.Context, op string, in migration.Request) (migration.Record, error) {
	a, ok := ctx.Value(mcpAuthorityKey{}).(mcpAuthority)
	if !ok {
		return migration.Record{}, ErrForbidden
	}
	return m.store.Migrate(ctx, a.account, a.digest, m.auth.now(), op, in)
}
func (m *Management) MigrationFind(ctx context.Context, in migration.Find) (migration.Page, error) {
	a, ok := ctx.Value(mcpAuthorityKey{}).(mcpAuthority)
	if !ok {
		return migration.Page{}, ErrForbidden
	}
	return m.store.MigrationFind(ctx, a.account, a.digest, m.auth.now(), in)
}
