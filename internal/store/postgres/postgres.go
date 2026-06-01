// Package postgres is the Postgres-backed implementation of store.Store.
package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peios/acta/internal/store"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Postgres struct {
	pool *pgxpool.Pool
}

// Connect opens a pool and verifies connectivity.
func Connect(ctx context.Context, url string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() { p.pool.Close() }

// --- users ---

func (p *Postgres) CreateUser(ctx context.Context, u store.NewUser) (store.User, error) {
	const q = `INSERT INTO users (username, display, password_hash)
	           VALUES ($1, $2, $3)
	           RETURNING id::text, username, display, password_hash, created_at`
	var out store.User
	err := p.pool.QueryRow(ctx, q, u.Username, u.Display, u.PasswordHash).
		Scan(&out.ID, &out.Username, &out.Display, &out.PasswordHash, &out.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return store.User{}, store.ErrUsernameTaken
		}
		return store.User{}, err
	}
	return out, nil
}

func (p *Postgres) UserByUsername(ctx context.Context, username string) (store.User, error) {
	const q = `SELECT id::text, username, display, password_hash, created_at
	           FROM users WHERE username = $1`
	return scanUser(p.pool.QueryRow(ctx, q, username))
}

func (p *Postgres) UserByID(ctx context.Context, id string) (store.User, error) {
	const q = `SELECT id::text, username, display, password_hash, created_at
	           FROM users WHERE id = $1::uuid`
	return scanUser(p.pool.QueryRow(ctx, q, id))
}

func scanUser(row pgx.Row) (store.User, error) {
	var u store.User
	err := row.Scan(&u.ID, &u.Username, &u.Display, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, store.ErrUserNotFound
	}
	return u, err
}

// --- sessions ---

func (p *Postgres) CreateSession(ctx context.Context, s store.Session) error {
	const q = `INSERT INTO sessions (id, user_id, created_at, expires_at, last_seen)
	           VALUES ($1, $2::uuid, $3, $4, $5)`
	_, err := p.pool.Exec(ctx, q, s.ID, s.UserID, s.CreatedAt, s.ExpiresAt, s.LastSeen)
	return err
}

func (p *Postgres) SessionByID(ctx context.Context, id string) (store.Session, error) {
	const q = `SELECT id, user_id::text, created_at, expires_at, last_seen
	           FROM sessions WHERE id = $1`
	var s store.Session
	err := p.pool.QueryRow(ctx, q, id).
		Scan(&s.ID, &s.UserID, &s.CreatedAt, &s.ExpiresAt, &s.LastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Session{}, store.ErrSessionNotFound
	}
	return s, err
}

func (p *Postgres) TouchSession(ctx context.Context, id string, lastSeen time.Time) error {
	const q = `UPDATE sessions SET last_seen = $2 WHERE id = $1`
	_, err := p.pool.Exec(ctx, q, id, lastSeen)
	return err
}

// DeleteSession is idempotent: deleting a missing session is not an error.
func (p *Postgres) DeleteSession(ctx context.Context, id string) error {
	const q = `DELETE FROM sessions WHERE id = $1`
	_, err := p.pool.Exec(ctx, q, id)
	return err
}

// DeleteExpiredSessions clears absolute-expired rows. Idle expiry is enforced
// at read time, so this sweep only needs the hard ceiling.
func (p *Postgres) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	const q = `DELETE FROM sessions WHERE expires_at < $1`
	ct, err := p.pool.Exec(ctx, q, now)
	return ct.RowsAffected(), err
}

// --- migrations ---

// Migrate applies any embedded migrations not yet recorded, each in its own
// transaction. No external migration tool or dependency.
func (p *Postgres) Migrate(ctx context.Context) error {
	const ensure = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    text        PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now())`
	if _, err := p.pool.Exec(ctx, ensure); err != nil {
		return err
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		var applied bool
		if err := p.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, f,
		).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}

		body, err := migrationsFS.ReadFile("migrations/" + f)
		if err != nil {
			return err
		}
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", f, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, f); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

var _ store.Store = (*Postgres)(nil)
