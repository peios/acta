package postgres

import (
	"acta/internal/auth"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io/fs"
)

// VerifyRecovery is a read-only check by the exact recorded Acta executable.
// Unlike Open it never migrates, backfills, provisions keys or starts workers.
func VerifyRecovery(ctx context.Context, url string, key []byte) error {
	c, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer c.Close(ctx)
	tx, err := c.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		return err
	}
	if count != len(names) {
		return fmt.Errorf("recovery schema has %d migrations; this executable requires %d", count, len(names))
	}
	for _, name := range names {
		raw, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		var actual string
		if err = tx.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE name=$1`, name).Scan(&actual); err != nil {
			return err
		}
		if actual != fmt.Sprintf("%x", sha256.Sum256(raw)) {
			return fmt.Errorf("recovery migration %s does not match this executable", name)
		}
	}
	var fingerprint []byte
	if err = tx.QueryRow(ctx, `SELECT fingerprint FROM security_key WHERE singleton`).Scan(&fingerprint); err != nil {
		return err
	}
	if fmt.Sprintf("%x", fingerprint) != fmt.Sprintf("%x", sha256.Sum256(key)) {
		return fmt.Errorf("recovery security key does not match database")
	}
	rows, err := tx.Query(ctx, `SELECT id::text,security_data FROM accounts WHERE security_data IS NOT NULL`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var encrypted []byte
		if err = rows.Scan(&id, &encrypted); err != nil {
			rows.Close()
			return err
		}
		if err = auth.VerifyRecoverySecurity(key, id, encrypted); err != nil {
			rows.Close()
			return fmt.Errorf("cannot decrypt restored account security data: %w", err)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var bad int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM document_versions v LEFT JOIN document_files f USING(file_id) WHERE f.file_id IS NULL OR octet_length(f.content)<>v.size OR encode(sha256(f.content),'hex')<>v.sha256`).Scan(&bad); err != nil {
		return err
	}
	if bad != 0 {
		return fmt.Errorf("%d restored document versions failed verification", bad)
	}
	return tx.Commit(ctx)
}
