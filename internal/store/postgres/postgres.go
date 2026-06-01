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

// --- credentials ---

func (p *Postgres) CreateCredential(ctx context.Context, c store.Credential) error {
	const q = `INSERT INTO credentials
	    (user_id, credential_id, public_key, sign_count, transports, aaguid, name)
	    VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)`
	_, err := p.pool.Exec(ctx, q, c.UserID, c.CredentialID, c.PublicKey,
		int64(c.SignCount), c.Transports, c.AAGUID, c.Name)
	return err
}

func (p *Postgres) CredentialsByUserID(ctx context.Context, userID string) ([]store.Credential, error) {
	const q = `SELECT id::text, user_id::text, credential_id, public_key, sign_count,
	                  transports, aaguid, name, created_at, last_used_at
	           FROM credentials WHERE user_id = $1::uuid ORDER BY created_at`
	rows, err := p.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Credential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *Postgres) CredentialByCredentialID(ctx context.Context, credentialID []byte) (store.Credential, error) {
	const q = `SELECT id::text, user_id::text, credential_id, public_key, sign_count,
	                  transports, aaguid, name, created_at, last_used_at
	           FROM credentials WHERE credential_id = $1`
	return scanCredential(p.pool.QueryRow(ctx, q, credentialID))
}

func scanCredential(row pgx.Row) (store.Credential, error) {
	var c store.Credential
	var signCount int64
	err := row.Scan(&c.ID, &c.UserID, &c.CredentialID, &c.PublicKey, &signCount,
		&c.Transports, &c.AAGUID, &c.Name, &c.CreatedAt, &c.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Credential{}, store.ErrCredentialNotFound
	}
	c.SignCount = uint32(signCount)
	return c, err
}

func (p *Postgres) TouchCredential(ctx context.Context, credentialID []byte, signCount uint32, lastUsed time.Time) error {
	const q = `UPDATE credentials SET sign_count = $2, last_used_at = $3 WHERE credential_id = $1`
	_, err := p.pool.Exec(ctx, q, credentialID, int64(signCount), lastUsed)
	return err
}

func (p *Postgres) DeleteCredential(ctx context.Context, id, userID string) error {
	const q = `DELETE FROM credentials WHERE id = $1::uuid AND user_id = $2::uuid`
	ct, err := p.pool.Exec(ctx, q, id, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrCredentialNotFound
	}
	return nil
}

// --- webauthn challenges ---

func (p *Postgres) CreateChallenge(ctx context.Context, c store.Challenge) error {
	const q = `INSERT INTO webauthn_challenges (id, user_id, data, expires_at)
	           VALUES ($1, $2::uuid, $3::jsonb, $4)`
	var userID any
	if c.UserID != "" {
		userID = c.UserID
	}
	_, err := p.pool.Exec(ctx, q, c.ID, userID, string(c.Data), c.ExpiresAt)
	return err
}

func (p *Postgres) ConsumeChallenge(ctx context.Context, id string) (store.Challenge, error) {
	const q = `DELETE FROM webauthn_challenges WHERE id = $1
	           RETURNING id, COALESCE(user_id::text, ''), data, expires_at`
	var c store.Challenge
	err := p.pool.QueryRow(ctx, q, id).Scan(&c.ID, &c.UserID, &c.Data, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Challenge{}, store.ErrChallengeNotFound
	}
	return c, err
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
