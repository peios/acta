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

// --- workspaces ---

func (p *Postgres) CreateWorkspace(ctx context.Context, w store.Workspace) (store.Workspace, error) {
	const q = `INSERT INTO workspaces (slug, name, created_by)
	           VALUES ($1, $2, $3::uuid)
	           RETURNING id::text, slug, name, COALESCE(created_by::text, ''), created_at`
	var createdBy any
	if w.CreatedBy != "" {
		createdBy = w.CreatedBy
	}
	var out store.Workspace
	err := p.pool.QueryRow(ctx, q, w.Slug, w.Name, createdBy).
		Scan(&out.ID, &out.Slug, &out.Name, &out.CreatedBy, &out.CreatedAt)
	if err != nil {
		return store.Workspace{}, mapWorkspaceConflict(err)
	}
	return out, nil
}

func (p *Postgres) ListWorkspaces(ctx context.Context) ([]store.Workspace, error) {
	const q = `SELECT id::text, slug, name, COALESCE(created_by::text, ''), created_at
	           FROM workspaces ORDER BY created_at`
	rows, err := p.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (p *Postgres) WorkspaceByID(ctx context.Context, id string) (store.Workspace, error) {
	const q = `SELECT id::text, slug, name, COALESCE(created_by::text, ''), created_at
	           FROM workspaces WHERE id = $1::uuid`
	return scanWorkspace(p.pool.QueryRow(ctx, q, id))
}

func (p *Postgres) WorkspaceBySlug(ctx context.Context, slug string) (store.Workspace, error) {
	const q = `SELECT id::text, slug, name, COALESCE(created_by::text, ''), created_at
	           FROM workspaces WHERE slug = $1`
	return scanWorkspace(p.pool.QueryRow(ctx, q, slug))
}

func scanWorkspace(row pgx.Row) (store.Workspace, error) {
	var w store.Workspace
	err := row.Scan(&w.ID, &w.Slug, &w.Name, &w.CreatedBy, &w.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Workspace{}, store.ErrWorkspaceNotFound
	}
	return w, err
}

func (p *Postgres) RenameWorkspace(ctx context.Context, id, name string) error {
	const q = `UPDATE workspaces SET name = $2 WHERE id = $1::uuid`
	ct, err := p.pool.Exec(ctx, q, id, name)
	if err != nil {
		return mapWorkspaceConflict(err)
	}
	if ct.RowsAffected() == 0 {
		return store.ErrWorkspaceNotFound
	}
	return nil
}

func (p *Postgres) DeleteWorkspace(ctx context.Context, id string) error {
	const q = `DELETE FROM workspaces WHERE id = $1::uuid`
	ct, err := p.pool.Exec(ctx, q, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrWorkspaceNotFound
	}
	return nil
}

func (p *Postgres) CountWorkspaces(ctx context.Context) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT count(*) FROM workspaces`).Scan(&n)
	return n, err
}

// mapWorkspaceConflict turns a unique-violation into the matching sentinel by
// inspecting which constraint fired (name index vs the slug column).
func mapWorkspaceConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if strings.Contains(pgErr.ConstraintName, "slug") {
			return store.ErrWorkspaceSlugTaken
		}
		return store.ErrWorkspaceNameTaken
	}
	return err
}

// --- board: statuses ---

func (p *Postgres) CreateStatus(ctx context.Context, s store.Status) (store.Status, error) {
	const q = `INSERT INTO statuses (workspace_id, name, position)
	           VALUES ($1::uuid, $2, $3)
	           RETURNING id::text, workspace_id::text, name, position, created_at`
	var out store.Status
	err := p.pool.QueryRow(ctx, q, s.WorkspaceID, s.Name, s.Position).
		Scan(&out.ID, &out.WorkspaceID, &out.Name, &out.Position, &out.CreatedAt)
	return out, err
}

func (p *Postgres) StatusesByWorkspace(ctx context.Context, workspaceID string) ([]store.Status, error) {
	const q = `SELECT id::text, workspace_id::text, name, position, created_at
	           FROM statuses WHERE workspace_id = $1::uuid ORDER BY position`
	rows, err := p.pool.Query(ctx, q, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Status
	for rows.Next() {
		var s store.Status
		if err := rows.Scan(&s.ID, &s.WorkspaceID, &s.Name, &s.Position, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) StatusByID(ctx context.Context, id string) (store.Status, error) {
	const q = `SELECT id::text, workspace_id::text, name, position, created_at
	           FROM statuses WHERE id = $1::uuid`
	var s store.Status
	err := p.pool.QueryRow(ctx, q, id).Scan(&s.ID, &s.WorkspaceID, &s.Name, &s.Position, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Status{}, store.ErrStatusNotFound
	}
	return s, err
}

func (p *Postgres) RenameStatus(ctx context.Context, id, name string) error {
	ct, err := p.pool.Exec(ctx, `UPDATE statuses SET name = $2 WHERE id = $1::uuid`, id, name)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrStatusNotFound
	}
	return nil
}

func (p *Postgres) ReorderStatuses(ctx context.Context, workspaceID string, orderedIDs []string) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		for i, id := range orderedIDs {
			if _, err := tx.Exec(ctx,
				`UPDATE statuses SET position = $1 WHERE id = $2::uuid AND workspace_id = $3::uuid`,
				i, id, workspaceID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *Postgres) DeleteStatus(ctx context.Context, id string) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM statuses WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrStatusNotFound
	}
	return nil
}

// --- board: items ---

func (p *Postgres) CreateItem(ctx context.Context, i store.Item) (store.Item, error) {
	const q = `INSERT INTO items (workspace_id, status_id, title, position)
	           VALUES ($1::uuid, $2::uuid, $3, $4)
	           RETURNING id::text, workspace_id::text, status_id::text, title, position, created_at`
	var out store.Item
	err := p.pool.QueryRow(ctx, q, i.WorkspaceID, i.StatusID, i.Title, i.Position).
		Scan(&out.ID, &out.WorkspaceID, &out.StatusID, &out.Title, &out.Position, &out.CreatedAt)
	return out, err
}

func (p *Postgres) ItemsByWorkspace(ctx context.Context, workspaceID string) ([]store.Item, error) {
	const q = `SELECT id::text, workspace_id::text, status_id::text, title, position, created_at
	           FROM items WHERE workspace_id = $1::uuid ORDER BY position`
	return p.queryItems(ctx, q, workspaceID)
}

func (p *Postgres) ItemsByStatus(ctx context.Context, statusID string) ([]store.Item, error) {
	const q = `SELECT id::text, workspace_id::text, status_id::text, title, position, created_at
	           FROM items WHERE status_id = $1::uuid ORDER BY position`
	return p.queryItems(ctx, q, statusID)
}

func (p *Postgres) queryItems(ctx context.Context, q, arg string) ([]store.Item, error) {
	rows, err := p.pool.Query(ctx, q, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Item
	for rows.Next() {
		var i store.Item
		if err := rows.Scan(&i.ID, &i.WorkspaceID, &i.StatusID, &i.Title, &i.Position, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (p *Postgres) ItemByID(ctx context.Context, id string) (store.Item, error) {
	const q = `SELECT id::text, workspace_id::text, status_id::text, title, position, created_at
	           FROM items WHERE id = $1::uuid`
	var i store.Item
	err := p.pool.QueryRow(ctx, q, id).Scan(&i.ID, &i.WorkspaceID, &i.StatusID, &i.Title, &i.Position, &i.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Item{}, store.ErrItemNotFound
	}
	return i, err
}

func (p *Postgres) RenameItem(ctx context.Context, id, title string) error {
	ct, err := p.pool.Exec(ctx, `UPDATE items SET title = $2 WHERE id = $1::uuid`, id, title)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrItemNotFound
	}
	return nil
}

func (p *Postgres) ReorderItems(ctx context.Context, statusID string, orderedIDs []string) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		for i, id := range orderedIDs {
			if _, err := tx.Exec(ctx,
				`UPDATE items SET status_id = $1::uuid, position = $2 WHERE id = $3::uuid`,
				statusID, i, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *Postgres) DeleteItem(ctx context.Context, id string) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM items WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrItemNotFound
	}
	return nil
}

// inTx runs fn inside a transaction, rolling back on error.
func (p *Postgres) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
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
