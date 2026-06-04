// Package postgres is the Postgres-backed implementation of store.Store.
package postgres

import (
	"context"
	"embed"
	"encoding/json"
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

// Ping verifies database connectivity, for readiness checks.
func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

// createWithRetry runs an insert whose id comes from the gen_id() default,
// re-running so a fresh id is generated on the vanishingly rare primary-key
// collision. Only primary-key violations are retried; other unique violations
// (username, slug, …) are returned for the caller to interpret.
func createWithRetry[T any](insert func() (T, error)) (T, error) {
	var out T
	var err error
	for range 8 {
		if out, err = insert(); !isPKCollision(err) {
			return out, err
		}
	}
	return out, err
}

// retryInsert is createWithRetry for inserts that return only an error.
func retryInsert(insert func() error) error {
	var err error
	for range 8 {
		if err = insert(); !isPKCollision(err) {
			return err
		}
	}
	return err
}

func isPKCollision(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.HasSuffix(pgErr.ConstraintName, "_pkey")
}

// --- users ---

const userCols = `id::text, username, display, password_hash, COALESCE(agent_of_id::text, ''), created_at, disabled_at`

func (p *Postgres) CreateUser(ctx context.Context, u store.NewUser) (store.User, error) {
	out, err := createWithRetry(func() (store.User, error) {
		const q = `INSERT INTO users (username, display, password_hash, agent_of_id)
		           VALUES ($1, $2, $3, $4)
		           RETURNING ` + userCols
		var owner any
		if u.AgentOfID != "" {
			owner = u.AgentOfID
		}
		return scanUser(p.pool.QueryRow(ctx, q, u.Username, u.Display, u.PasswordHash, owner))
	})
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
	const q = `SELECT ` + userCols + ` FROM users WHERE username = $1`
	return scanUser(p.pool.QueryRow(ctx, q, username))
}

func (p *Postgres) UserByID(ctx context.Context, id string) (store.User, error) {
	const q = `SELECT ` + userCols + ` FROM users WHERE id = $1`
	return scanUser(p.pool.QueryRow(ctx, q, id))
}

func (p *Postgres) ListUsers(ctx context.Context) ([]store.User, error) {
	const q = `SELECT ` + userCols + ` FROM users ORDER BY lower(display), lower(username)`
	rows, err := p.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	return collectUsers(rows)
}

func (p *Postgres) AgentsByOwner(ctx context.Context, ownerID string) ([]store.User, error) {
	const q = `SELECT ` + userCols + ` FROM users WHERE agent_of_id = $1 ORDER BY lower(username)`
	rows, err := p.pool.Query(ctx, q, ownerID)
	if err != nil {
		return nil, err
	}
	return collectUsers(rows)
}

// DeleteUser removes the row; FKs cascade owned agents, credentials, tokens, and
// sessions, and null out items.created_by.
func (p *Postgres) DeleteUser(ctx context.Context, id string) error {
	const q = `DELETE FROM users WHERE id = $1`
	_, err := p.pool.Exec(ctx, q, id)
	return err
}

func (p *Postgres) SetUserPassword(ctx context.Context, id, passwordHash string) error {
	const q = `UPDATE users SET password_hash = $2 WHERE id = $1`
	ct, err := p.pool.Exec(ctx, q, id, passwordHash)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrUserNotFound
	}
	return nil
}

func (p *Postgres) SetUserDisabled(ctx context.Context, id string, disabled bool) error {
	const q = `UPDATE users SET disabled_at = CASE WHEN $2 THEN now() ELSE NULL END WHERE id = $1`
	ct, err := p.pool.Exec(ctx, q, id, disabled)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrUserNotFound
	}
	return nil
}

func scanUser(row pgx.Row) (store.User, error) {
	var u store.User
	err := row.Scan(&u.ID, &u.Username, &u.Display, &u.PasswordHash, &u.AgentOfID, &u.CreatedAt, &u.DisabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, store.ErrUserNotFound
	}
	return u, err
}

func collectUsers(rows pgx.Rows) ([]store.User, error) {
	defer rows.Close()
	var out []store.User
	for rows.Next() {
		var u store.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Display, &u.PasswordHash, &u.AgentOfID, &u.CreatedAt, &u.DisabledAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// --- sessions ---

// sessionCols lists session columns in scanSession order. public_id is the
// non-secret handle the account UI revokes by; the token (id) never leaves the
// server.
const sessionCols = `id, public_id::text, user_id::text, user_agent, created_at, expires_at, last_seen`

func scanSession(row pgx.Row) (store.Session, error) {
	var s store.Session
	err := row.Scan(&s.ID, &s.PublicID, &s.UserID, &s.UserAgent, &s.CreatedAt, &s.ExpiresAt, &s.LastSeen)
	return s, err
}

func (p *Postgres) CreateSession(ctx context.Context, s store.Session) error {
	// public_id defaults to gen_id() in the DB.
	const q = `INSERT INTO sessions (id, user_id, created_at, expires_at, last_seen, user_agent)
	           VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := p.pool.Exec(ctx, q, s.ID, s.UserID, s.CreatedAt, s.ExpiresAt, s.LastSeen, s.UserAgent)
	return err
}

func (p *Postgres) SessionByID(ctx context.Context, id string) (store.Session, error) {
	const q = `SELECT ` + sessionCols + ` FROM sessions WHERE id = $1`
	s, err := scanSession(p.pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Session{}, store.ErrSessionNotFound
	}
	return s, err
}

func (p *Postgres) SessionsByUserID(ctx context.Context, userID string, now time.Time) ([]store.Session, error) {
	const q = `SELECT ` + sessionCols + `
	           FROM sessions WHERE user_id = $1 AND expires_at > $2
	           ORDER BY last_seen DESC`
	rows, err := p.pool.Query(ctx, q, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
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

func (p *Postgres) DeleteUserSession(ctx context.Context, publicID, userID string) error {
	const q = `DELETE FROM sessions WHERE public_id = $1 AND user_id = $2`
	_, err := p.pool.Exec(ctx, q, publicID, userID)
	return err
}

func (p *Postgres) DeleteOtherSessions(ctx context.Context, userID, keepID string) (int64, error) {
	const q = `DELETE FROM sessions WHERE user_id = $1 AND id <> $2`
	ct, err := p.pool.Exec(ctx, q, userID, keepID)
	return ct.RowsAffected(), err
}

// DeleteExpiredSessions clears absolute-expired rows. Idle expiry is enforced
// at read time, so this sweep only needs the hard ceiling.
func (p *Postgres) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	const q = `DELETE FROM sessions WHERE expires_at < $1`
	ct, err := p.pool.Exec(ctx, q, now)
	return ct.RowsAffected(), err
}

// --- api tokens ---

// apiTokenCols omits token_hash deliberately: the hash never round-trips out of
// the store, so a listed or authenticated token can't leak its own secret.
const apiTokenCols = `id::text, user_id::text, name, prefix, created_at, last_used_at`

func scanAPIToken(row pgx.Row) (store.APIToken, error) {
	var t store.APIToken
	err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &t.CreatedAt, &t.LastUsedAt)
	return t, err
}

func (p *Postgres) CreateAPIToken(ctx context.Context, t store.APIToken) (store.APIToken, error) {
	return createWithRetry(func() (store.APIToken, error) {
		const q = `INSERT INTO api_tokens (user_id, name, token_hash, prefix)
		           VALUES ($1, $2, $3, $4)
		           RETURNING ` + apiTokenCols
		return scanAPIToken(p.pool.QueryRow(ctx, q, t.UserID, t.Name, t.Hash, t.Prefix))
	})
}

func (p *Postgres) APITokensByUserID(ctx context.Context, userID string) ([]store.APIToken, error) {
	const q = `SELECT ` + apiTokenCols + `
	           FROM api_tokens WHERE user_id = $1
	           ORDER BY created_at DESC`
	rows, err := p.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (p *Postgres) APITokenByHash(ctx context.Context, hash []byte) (store.APIToken, error) {
	const q = `SELECT ` + apiTokenCols + ` FROM api_tokens WHERE token_hash = $1`
	t, err := scanAPIToken(p.pool.QueryRow(ctx, q, hash))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.APIToken{}, store.ErrAPITokenNotFound
	}
	return t, err
}

func (p *Postgres) TouchAPIToken(ctx context.Context, id string, lastUsed time.Time) error {
	const q = `UPDATE api_tokens SET last_used_at = $2 WHERE id = $1`
	_, err := p.pool.Exec(ctx, q, id, lastUsed)
	return err
}

func (p *Postgres) DeleteAPIToken(ctx context.Context, id, userID string) error {
	const q = `DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`
	ct, err := p.pool.Exec(ctx, q, id, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrAPITokenNotFound
	}
	return nil
}

// --- credentials ---

func (p *Postgres) CreateCredential(ctx context.Context, c store.Credential) error {
	return retryInsert(func() error {
		const q = `INSERT INTO credentials
		    (user_id, credential_id, public_key, sign_count, transports, aaguid, name)
		    VALUES ($1, $2, $3, $4, $5, $6, $7)`
		_, err := p.pool.Exec(ctx, q, c.UserID, c.CredentialID, c.PublicKey,
			int64(c.SignCount), c.Transports, c.AAGUID, c.Name)
		return err
	})
}

func (p *Postgres) CredentialsByUserID(ctx context.Context, userID string) ([]store.Credential, error) {
	const q = `SELECT id::text, user_id::text, credential_id, public_key, sign_count,
	                  transports, aaguid, name, created_at, last_used_at
	           FROM credentials WHERE user_id = $1 ORDER BY created_at`
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
	const q = `DELETE FROM credentials WHERE id = $1 AND user_id = $2`
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
	           VALUES ($1, $2, $3::jsonb, $4)`
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
	out, err := createWithRetry(func() (store.Workspace, error) {
		const q = `INSERT INTO workspaces (slug, name, created_by, item_prefix)
		           VALUES ($1, $2, $3, $4)
		           RETURNING id::text, slug, name, COALESCE(created_by::text, ''), created_at, item_prefix`
		var createdBy any
		if w.CreatedBy != "" {
			createdBy = w.CreatedBy
		}
		var out store.Workspace
		err := p.pool.QueryRow(ctx, q, w.Slug, w.Name, createdBy, w.ItemPrefix).
			Scan(&out.ID, &out.Slug, &out.Name, &out.CreatedBy, &out.CreatedAt, &out.ItemPrefix)
		return out, err
	})
	if err != nil {
		return store.Workspace{}, mapWorkspaceConflict(err)
	}
	return out, nil
}

func (p *Postgres) ListWorkspaces(ctx context.Context) ([]store.Workspace, error) {
	const q = `SELECT id::text, slug, name, COALESCE(created_by::text, ''), created_at, item_prefix
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
	const q = `SELECT id::text, slug, name, COALESCE(created_by::text, ''), created_at, item_prefix
	           FROM workspaces WHERE id = $1`
	return scanWorkspace(p.pool.QueryRow(ctx, q, id))
}

func (p *Postgres) WorkspaceBySlug(ctx context.Context, slug string) (store.Workspace, error) {
	const q = `SELECT id::text, slug, name, COALESCE(created_by::text, ''), created_at, item_prefix
	           FROM workspaces WHERE slug = $1`
	return scanWorkspace(p.pool.QueryRow(ctx, q, slug))
}

func scanWorkspace(row pgx.Row) (store.Workspace, error) {
	var w store.Workspace
	err := row.Scan(&w.ID, &w.Slug, &w.Name, &w.CreatedBy, &w.CreatedAt, &w.ItemPrefix)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Workspace{}, store.ErrWorkspaceNotFound
	}
	return w, err
}

func (p *Postgres) WorkspaceByPrefix(ctx context.Context, prefix string) (store.Workspace, error) {
	const q = `SELECT id::text, slug, name, COALESCE(created_by::text, ''), created_at, item_prefix
	           FROM workspaces WHERE item_prefix <> '' AND lower(item_prefix) = lower($1)`
	return scanWorkspace(p.pool.QueryRow(ctx, q, prefix))
}

func (p *Postgres) RenameWorkspace(ctx context.Context, id, name string) error {
	const q = `UPDATE workspaces SET name = $2 WHERE id = $1`
	ct, err := p.pool.Exec(ctx, q, id, name)
	if err != nil {
		return mapWorkspaceConflict(err)
	}
	if ct.RowsAffected() == 0 {
		return store.ErrWorkspaceNotFound
	}
	return nil
}

func (p *Postgres) UpdateWorkspace(ctx context.Context, id, name, slug, prefix string) error {
	const q = `UPDATE workspaces SET name = $2, slug = $3, item_prefix = $4 WHERE id = $1`
	ct, err := p.pool.Exec(ctx, q, id, name, slug, prefix)
	if err != nil {
		return mapWorkspaceConflict(err)
	}
	if ct.RowsAffected() == 0 {
		return store.ErrWorkspaceNotFound
	}
	return nil
}

func (p *Postgres) DeleteWorkspace(ctx context.Context, id string) error {
	const q = `DELETE FROM workspaces WHERE id = $1`
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
		switch {
		case strings.Contains(pgErr.ConstraintName, "slug"):
			return store.ErrWorkspaceSlugTaken
		case strings.Contains(pgErr.ConstraintName, "prefix"):
			return store.ErrWorkspacePrefixTaken
		default:
			return store.ErrWorkspaceNameTaken
		}
	}
	return err
}

// --- boards ---

const boardCols = `id::text, workspace_id::text, name, slug, position, created_at`

func scanBoard(row pgx.Row) (store.Board, error) {
	var b store.Board
	err := row.Scan(&b.ID, &b.WorkspaceID, &b.Name, &b.Slug, &b.Position, &b.CreatedAt)
	return b, err
}

func (p *Postgres) CreateBoard(ctx context.Context, b store.Board) (store.Board, error) {
	return createWithRetry(func() (store.Board, error) {
		const q = `INSERT INTO boards (workspace_id, name, slug, position)
		           VALUES ($1, $2, $3, $4)
		           RETURNING ` + boardCols
		board, err := scanBoard(p.pool.QueryRow(ctx, q, b.WorkspaceID, b.Name, b.Slug, b.Position))
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return store.Board{}, store.ErrWorkspaceSlugTaken
		}
		return board, err
	})
}

func (p *Postgres) BoardsByWorkspace(ctx context.Context, workspaceID string) ([]store.Board, error) {
	const q = `SELECT ` + boardCols + ` FROM boards WHERE workspace_id = $1 ORDER BY position`
	rows, err := p.pool.Query(ctx, q, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Board
	for rows.Next() {
		b, err := scanBoard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (p *Postgres) BoardByID(ctx context.Context, id string) (store.Board, error) {
	const q = `SELECT ` + boardCols + ` FROM boards WHERE id = $1`
	b, err := scanBoard(p.pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Board{}, store.ErrBoardNotFound
	}
	return b, err
}

func (p *Postgres) BoardBySlug(ctx context.Context, workspaceID, slug string) (store.Board, error) {
	const q = `SELECT ` + boardCols + ` FROM boards WHERE workspace_id = $1 AND slug = $2`
	b, err := scanBoard(p.pool.QueryRow(ctx, q, workspaceID, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Board{}, store.ErrBoardNotFound
	}
	return b, err
}

// --- board: statuses ---

const statusCols = `id::text, workspace_id::text, board_id::text, name, position, color, is_entry, created_at`

func scanStatus(row pgx.Row) (store.Status, error) {
	var s store.Status
	err := row.Scan(&s.ID, &s.WorkspaceID, &s.BoardID, &s.Name, &s.Position, &s.Color, &s.IsEntry, &s.CreatedAt)
	return s, err
}

func (p *Postgres) queryStatuses(ctx context.Context, q string, args ...any) ([]store.Status, error) {
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Status
	for rows.Next() {
		s, err := scanStatus(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateStatus(ctx context.Context, s store.Status) (store.Status, error) {
	return createWithRetry(func() (store.Status, error) {
		const q = `INSERT INTO statuses (workspace_id, board_id, name, position, color, is_entry)
		           VALUES ($1, $2, $3, $4, $5, $6)
		           RETURNING ` + statusCols
		return scanStatus(p.pool.QueryRow(ctx, q,
			s.WorkspaceID, s.BoardID, s.Name, s.Position, s.Color, s.IsEntry))
	})
}

func (p *Postgres) StatusesByWorkspace(ctx context.Context, workspaceID string) ([]store.Status, error) {
	// Order by board first so the default board's lanes lead (and lanes never
	// interleave across boards, which share a 0-based position sequence).
	const q = `SELECT s.id::text, s.workspace_id::text, s.board_id::text, s.name,
	                  s.position, s.color, s.is_entry, s.created_at
	           FROM statuses s JOIN boards b ON s.board_id = b.id
	           WHERE s.workspace_id = $1 ORDER BY b.position, s.position`
	return p.queryStatuses(ctx, q, workspaceID)
}

func (p *Postgres) StatusesByBoard(ctx context.Context, boardID string) ([]store.Status, error) {
	const q = `SELECT ` + statusCols + ` FROM statuses WHERE board_id = $1 ORDER BY position`
	return p.queryStatuses(ctx, q, boardID)
}

func (p *Postgres) StatusByID(ctx context.Context, id string) (store.Status, error) {
	const q = `SELECT ` + statusCols + ` FROM statuses WHERE id = $1`
	s, err := scanStatus(p.pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Status{}, store.ErrStatusNotFound
	}
	return s, err
}

func (p *Postgres) RenameStatus(ctx context.Context, id, name string) error {
	ct, err := p.pool.Exec(ctx, `UPDATE statuses SET name = $2 WHERE id = $1`, id, name)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrStatusNotFound
	}
	return nil
}

func (p *Postgres) SetStatusColor(ctx context.Context, id, color string) error {
	ct, err := p.pool.Exec(ctx, `UPDATE statuses SET color = $2 WHERE id = $1`, id, color)
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
				`UPDATE statuses SET position = $1 WHERE id = $2 AND workspace_id = $3`,
				i, id, workspaceID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *Postgres) DeleteStatus(ctx context.Context, id string) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM statuses WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrStatusNotFound
	}
	return nil
}

// --- board: items ---

const itemCols = `id::text, ref_num, workspace_id::text, status_id::text, COALESCE(parent_id::text, ''),
                  title, description, COALESCE(assignee_id::text, ''), position, is_milestone,
                  ms_position, archived_at, created_at, COALESCE(created_by::text, '')`

func scanItem(row pgx.Row) (store.Item, error) {
	var i store.Item
	err := row.Scan(&i.ID, &i.RefNum, &i.WorkspaceID, &i.StatusID, &i.ParentID, &i.Title, &i.Description,
		&i.AssigneeID, &i.Position, &i.IsMilestone, &i.MSPosition, &i.ArchivedAt, &i.CreatedAt, &i.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Item{}, store.ErrItemNotFound
	}
	return i, err
}

func (p *Postgres) CreateItem(ctx context.Context, i store.Item) (store.Item, error) {
	return createWithRetry(func() (store.Item, error) {
		// Bump the workspace's monotonic counter and stamp the new item with it,
		// atomically, so ref numbers are unique per workspace and never reused.
		const q = `WITH seq AS (
		               UPDATE workspaces SET item_seq = item_seq + 1 WHERE id = $1 RETURNING item_seq
		           )
		           INSERT INTO items (workspace_id, status_id, title, position, parent_id, created_by, ref_num)
		           VALUES ($1, $2, $3, $4, $5, $6, (SELECT item_seq FROM seq))
		           RETURNING ` + itemCols
		var parent, creator any
		if i.ParentID != "" {
			parent = i.ParentID
		}
		if i.CreatedBy != "" {
			creator = i.CreatedBy
		}
		return scanItem(p.pool.QueryRow(ctx, q, i.WorkspaceID, i.StatusID, i.Title, i.Position, parent, creator))
	})
}

func (p *Postgres) ItemByRef(ctx context.Context, workspaceID string, refNum int) (store.Item, error) {
	q := `SELECT ` + itemCols + ` FROM items WHERE workspace_id = $1 AND ref_num = $2`
	return scanItem(p.pool.QueryRow(ctx, q, workspaceID, refNum))
}

func (p *Postgres) ItemsByWorkspace(ctx context.Context, workspaceID string) ([]store.Item, error) {
	q := `SELECT ` + itemCols + ` FROM items
	      WHERE workspace_id = $1 AND parent_id IS NULL AND archived_at IS NULL ORDER BY position`
	return p.queryItems(ctx, q, workspaceID)
}

func (p *Postgres) AllItemsByWorkspace(ctx context.Context, workspaceID string) ([]store.Item, error) {
	q := `SELECT ` + itemCols + ` FROM items
	      WHERE workspace_id = $1 AND archived_at IS NULL ORDER BY lower(title)`
	return p.queryItems(ctx, q, workspaceID)
}

func (p *Postgres) ItemsByStatus(ctx context.Context, statusID string) ([]store.Item, error) {
	q := `SELECT ` + itemCols + ` FROM items
	      WHERE status_id = $1 AND parent_id IS NULL AND archived_at IS NULL ORDER BY position`
	return p.queryItems(ctx, q, statusID)
}

// ArchivedItemsByWorkspace returns the archived subtree roots — archived items
// whose parent isn't itself archived — so a cascade-archived child isn't listed
// separately from its parent.
func (p *Postgres) ArchivedItemsByWorkspace(ctx context.Context, workspaceID string) ([]store.Item, error) {
	q := `SELECT ` + itemCols + ` FROM items i
	      WHERE i.workspace_id = $1 AND i.archived_at IS NOT NULL
	        AND (i.parent_id IS NULL OR NOT EXISTS (
	              SELECT 1 FROM items p WHERE p.id = i.parent_id AND p.archived_at IS NOT NULL))
	      ORDER BY i.archived_at DESC`
	return p.queryItems(ctx, q, workspaceID)
}

func (p *Postgres) ChildrenByParent(ctx context.Context, parentID string, includeArchived bool) ([]store.Item, error) {
	q := `SELECT ` + itemCols + ` FROM items WHERE parent_id = $1`
	if !includeArchived {
		q += ` AND archived_at IS NULL`
	}
	q += ` ORDER BY position`
	return p.queryItems(ctx, q, parentID)
}

func (p *Postgres) SubtaskCountsByWorkspace(ctx context.Context, workspaceID, doneStatusID string) (map[string]store.SubtaskCount, error) {
	const q = `SELECT parent_id::text,
	                  count(*) AS total,
	                  count(*) FILTER (WHERE status_id = $2) AS done
	           FROM items
	           WHERE workspace_id = $1 AND parent_id IS NOT NULL AND archived_at IS NULL
	           GROUP BY parent_id`
	var done any
	if doneStatusID != "" {
		done = doneStatusID
	}
	rows, err := p.pool.Query(ctx, q, workspaceID, done)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]store.SubtaskCount{}
	for rows.Next() {
		var pid string
		var total, doneN int
		if err := rows.Scan(&pid, &total, &doneN); err != nil {
			return nil, err
		}
		out[pid] = store.SubtaskCount{Done: doneN, Total: total}
	}
	return out, rows.Err()
}

func (p *Postgres) queryItems(ctx context.Context, q, arg string) ([]store.Item, error) {
	rows, err := p.pool.Query(ctx, q, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Item
	for rows.Next() {
		i, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (p *Postgres) ItemByID(ctx context.Context, id string) (store.Item, error) {
	q := `SELECT ` + itemCols + ` FROM items WHERE id = $1`
	return scanItem(p.pool.QueryRow(ctx, q, id))
}

func (p *Postgres) RenameItem(ctx context.Context, id, title string) error {
	return p.execItem(ctx, `UPDATE items SET title = $2 WHERE id = $1`, id, title)
}

func (p *Postgres) UpdateItemDescription(ctx context.Context, id, description string) error {
	return p.execItem(ctx, `UPDATE items SET description = $2 WHERE id = $1`, id, description)
}

func (p *Postgres) SetItemAssignee(ctx context.Context, id, assigneeID string) error {
	// $2 is always present; nil becomes SQL NULL (unassigned).
	var a any
	if assigneeID != "" {
		a = assigneeID
	}
	ct, err := p.pool.Exec(ctx, `UPDATE items SET assignee_id = $2 WHERE id = $1`, id, a)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrItemNotFound
	}
	return nil
}

func (p *Postgres) ArchiveItem(ctx context.Context, id string) error {
	return p.execItem(ctx, `UPDATE items SET archived_at = now() WHERE id = $1`, id, nil)
}

func (p *Postgres) UnarchiveItem(ctx context.Context, id string) error {
	return p.execItem(ctx, `UPDATE items SET archived_at = NULL WHERE id = $1`, id, nil)
}

// execItem runs an item UPDATE, mapping no-rows-affected to ErrItemNotFound.
// arg is the second placeholder ($2); pass nil for statements that have none.
func (p *Postgres) execItem(ctx context.Context, q, id string, arg any) error {
	var ct pgconn.CommandTag
	var err error
	if arg == nil {
		ct, err = p.pool.Exec(ctx, q, id)
	} else {
		ct, err = p.pool.Exec(ctx, q, id, arg)
	}
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
				`UPDATE items SET status_id = $1, position = $2 WHERE id = $3`,
				statusID, i, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *Postgres) SetItemStatus(ctx context.Context, id, statusID string) error {
	return p.execItem(ctx, `UPDATE items SET status_id = $2 WHERE id = $1`, id, statusID)
}

func (p *Postgres) SetItemMilestone(ctx context.Context, id string, isMilestone bool) error {
	return p.execItem(ctx, `UPDATE items SET is_milestone = $2 WHERE id = $1`, id, isMilestone)
}

// ReorderMilestones renumbers each id's ms_position to its index in the slice.
// The is_milestone guard means a non-milestone id (or one from another
// workspace) is a no-op rather than a way to shuffle ordinary cards.
func (p *Postgres) ReorderMilestones(ctx context.Context, workspaceID string, orderedIDs []string) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		for i, id := range orderedIDs {
			if _, err := tx.Exec(ctx,
				`UPDATE items SET ms_position = $1 WHERE id = $2 AND workspace_id = $3 AND is_milestone = true`,
				i, id, workspaceID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *Postgres) SetItemParent(ctx context.Context, id, parentID string) error {
	// $2 is always present; nil becomes SQL NULL (a top-level item).
	var parent any
	if parentID != "" {
		parent = parentID
	}
	ct, err := p.pool.Exec(ctx, `UPDATE items SET parent_id = $2 WHERE id = $1`, id, parent)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrItemNotFound
	}
	return nil
}

// SetItemPositions renumbers items to their index in the slice, without
// touching status or parent — used to reorder a parent's subtasks.
func (p *Postgres) SetItemPositions(ctx context.Context, orderedIDs []string) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		for i, id := range orderedIDs {
			if _, err := tx.Exec(ctx, `UPDATE items SET position = $1 WHERE id = $2`, i, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *Postgres) DeleteItem(ctx context.Context, id string) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM items WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrItemNotFound
	}
	return nil
}

// --- comments ---

func (p *Postgres) CreateComment(ctx context.Context, c store.Comment) (store.Comment, error) {
	return createWithRetry(func() (store.Comment, error) {
		const q = `INSERT INTO comments (item_id, author_id, body)
		           VALUES ($1, $2, $3)
		           RETURNING id::text, item_id::text, COALESCE(author_id::text, ''), body, created_at`
		var author any
		if c.AuthorID != "" {
			author = c.AuthorID
		}
		var out store.Comment
		err := p.pool.QueryRow(ctx, q, c.ItemID, author, c.Body).
			Scan(&out.ID, &out.ItemID, &out.AuthorID, &out.Body, &out.CreatedAt)
		return out, err
	})
}

const commentCols = `id::text, item_id::text, COALESCE(author_id::text, ''), body, created_at, edited_at, deleted_at`

func scanComment(row interface{ Scan(...any) error }) (store.Comment, error) {
	var c store.Comment
	err := row.Scan(&c.ID, &c.ItemID, &c.AuthorID, &c.Body, &c.CreatedAt, &c.EditedAt, &c.DeletedAt)
	return c, err
}

func (p *Postgres) CommentsByItem(ctx context.Context, itemID string) ([]store.Comment, error) {
	q := `SELECT ` + commentCols + ` FROM comments WHERE item_id = $1 ORDER BY created_at`
	rows, err := p.pool.Query(ctx, q, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *Postgres) CommentByID(ctx context.Context, id string) (store.Comment, error) {
	q := `SELECT ` + commentCols + ` FROM comments WHERE id = $1`
	c, err := scanComment(p.pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Comment{}, store.ErrCommentNotFound
	}
	return c, err
}

func (p *Postgres) UpdateComment(ctx context.Context, id, body string, editedAt time.Time) (store.Comment, error) {
	q := `UPDATE comments SET body = $2, edited_at = $3 WHERE id = $1 RETURNING ` + commentCols
	c, err := scanComment(p.pool.QueryRow(ctx, q, id, body, editedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Comment{}, store.ErrCommentNotFound
	}
	return c, err
}

func (p *Postgres) SoftDeleteComment(ctx context.Context, id string, deletedAt time.Time) (store.Comment, error) {
	q := `UPDATE comments SET deleted_at = $2 WHERE id = $1 RETURNING ` + commentCols
	c, err := scanComment(p.pool.QueryRow(ctx, q, id, deletedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Comment{}, store.ErrCommentNotFound
	}
	return c, err
}

// --- activity log ---

const eventCols = `id::text, workspace_id::text, board_id, item_id, item_title, actor_id, actor_name, verb, data, created_at`

func (p *Postgres) RecordEvent(ctx context.Context, e store.Event) (store.Event, error) {
	return createWithRetry(func() (store.Event, error) {
		data, err := json.Marshal(normalizeEventData(e.Data))
		if err != nil {
			return store.Event{}, err
		}
		const q = `INSERT INTO events
		             (workspace_id, board_id, item_id, item_title, actor_id, actor_name, verb, data)
		           VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
		           RETURNING ` + eventCols
		row := p.pool.QueryRow(ctx, q,
			e.WorkspaceID, e.BoardID, e.ItemID, e.ItemTitle, e.ActorID, e.ActorName, e.Verb, data)
		return scanEvent(row)
	})
}

func (p *Postgres) EventsByItem(ctx context.Context, itemID string, limit int) ([]store.Event, error) {
	const q = `SELECT ` + eventCols + `
	           FROM events WHERE item_id = $1
	           ORDER BY created_at DESC, id DESC LIMIT $2`
	return p.queryEvents(ctx, q, itemID, clampEventLimit(limit))
}

func (p *Postgres) EventsByWorkspace(ctx context.Context, workspaceID string, limit int) ([]store.Event, error) {
	const q = `SELECT ` + eventCols + `
	           FROM events WHERE workspace_id = $1
	           ORDER BY created_at DESC, id DESC LIMIT $2`
	return p.queryEvents(ctx, q, workspaceID, clampEventLimit(limit))
}

func (p *Postgres) EventsByBoard(ctx context.Context, boardID string, limit int) ([]store.Event, error) {
	const q = `SELECT ` + eventCols + `
	           FROM events WHERE board_id = $1
	           ORDER BY created_at DESC, id DESC LIMIT $2`
	return p.queryEvents(ctx, q, boardID, clampEventLimit(limit))
}

func (p *Postgres) LatestEventForActor(ctx context.Context, itemID, actorID, verb string, since time.Time) (store.Event, bool, error) {
	const q = `SELECT ` + eventCols + `
	           FROM events
	           WHERE item_id = $1 AND actor_id = $2 AND verb = $3 AND created_at >= $4
	           ORDER BY created_at DESC, id DESC LIMIT 1`
	e, err := scanEvent(p.pool.QueryRow(ctx, q, itemID, actorID, verb, since))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Event{}, false, nil
	}
	if err != nil {
		return store.Event{}, false, err
	}
	return e, true, nil
}

func (p *Postgres) TouchEvent(ctx context.Context, id string, at time.Time, data map[string]string) error {
	raw, err := json.Marshal(normalizeEventData(data))
	if err != nil {
		return err
	}
	const q = `UPDATE events SET created_at = $2, data = $3::jsonb WHERE id = $1`
	_, err = p.pool.Exec(ctx, q, id, at, raw)
	return err
}

func (p *Postgres) queryEvents(ctx context.Context, q string, args ...any) ([]store.Event, error) {
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanEvent(row pgx.Row) (store.Event, error) {
	var (
		e   store.Event
		raw []byte
	)
	if err := row.Scan(&e.ID, &e.WorkspaceID, &e.BoardID, &e.ItemID, &e.ItemTitle,
		&e.ActorID, &e.ActorName, &e.Verb, &raw, &e.CreatedAt); err != nil {
		return store.Event{}, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &e.Data); err != nil {
			return store.Event{}, err
		}
	}
	if e.Data == nil {
		e.Data = map[string]string{}
	}
	return e, nil
}

// normalizeEventData makes a nil map marshal to {} rather than null.
func normalizeEventData(d map[string]string) map[string]string {
	if d == nil {
		return map[string]string{}
	}
	return d
}

func clampEventLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 200
	}
	return limit
}

// --- notifications ---

const notifCols = `id::text, recipient_id::text, kind, workspace_id, workspace_slug,
                   item_id, item_title, actor_id, actor_name, comment_id, excerpt,
                   created_at, read_at`

func (p *Postgres) CreateNotification(ctx context.Context, n store.Notification) (store.Notification, error) {
	return createWithRetry(func() (store.Notification, error) {
		const q = `INSERT INTO notifications
		             (recipient_id, kind, workspace_id, workspace_slug, item_id,
		              item_title, actor_id, actor_name, comment_id, excerpt)
		           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		           RETURNING ` + notifCols
		row := p.pool.QueryRow(ctx, q,
			n.RecipientID, n.Kind, n.WorkspaceID, n.WorkspaceSlug, n.ItemID,
			n.ItemTitle, n.ActorID, n.ActorName, n.CommentID, n.Excerpt)
		return scanNotification(row)
	})
}

func (p *Postgres) NotificationsByRecipient(ctx context.Context, recipientID string, limit int) ([]store.Notification, error) {
	const q = `SELECT ` + notifCols + `
	           FROM notifications WHERE recipient_id = $1
	           ORDER BY created_at DESC, id DESC LIMIT $2`
	rows, err := p.pool.Query(ctx, q, recipientID, clampEventLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (p *Postgres) UnreadNotificationsByRecipient(ctx context.Context, recipientID string, limit int) ([]store.Notification, error) {
	const q = `SELECT ` + notifCols + `
	           FROM notifications WHERE recipient_id = $1 AND read_at IS NULL
	           ORDER BY created_at DESC, id DESC LIMIT $2`
	rows, err := p.pool.Query(ctx, q, recipientID, clampEventLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (p *Postgres) UnreadNotificationCount(ctx context.Context, recipientID string) (int, error) {
	const q = `SELECT count(*) FROM notifications WHERE recipient_id = $1 AND read_at IS NULL`
	var n int
	err := p.pool.QueryRow(ctx, q, recipientID).Scan(&n)
	return n, err
}

func (p *Postgres) MarkNotificationRead(ctx context.Context, id, recipientID string) error {
	const q = `UPDATE notifications SET read_at = now()
	           WHERE id = $1 AND recipient_id = $2 AND read_at IS NULL`
	_, err := p.pool.Exec(ctx, q, id, recipientID)
	return err
}

func (p *Postgres) MarkAllNotificationsRead(ctx context.Context, recipientID string) error {
	const q = `UPDATE notifications SET read_at = now()
	           WHERE recipient_id = $1 AND read_at IS NULL`
	_, err := p.pool.Exec(ctx, q, recipientID)
	return err
}

func scanNotification(row pgx.Row) (store.Notification, error) {
	var n store.Notification
	if err := row.Scan(&n.ID, &n.RecipientID, &n.Kind, &n.WorkspaceID, &n.WorkspaceSlug,
		&n.ItemID, &n.ItemTitle, &n.ActorID, &n.ActorName, &n.CommentID, &n.Excerpt,
		&n.CreatedAt, &n.ReadAt); err != nil {
		return store.Notification{}, err
	}
	return n, nil
}

// --- app settings ---

func (p *Postgres) AppSetting(ctx context.Context, key string) (string, error) {
	const q = `SELECT value FROM app_settings WHERE key = $1`
	var v string
	err := p.pool.QueryRow(ctx, q, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (p *Postgres) SetAppSetting(ctx context.Context, key, value string) error {
	const q = `INSERT INTO app_settings (key, value) VALUES ($1, $2)
	           ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`
	_, err := p.pool.Exec(ctx, q, key, value)
	return err
}

// --- mcp prompts ---

func mapMCPPromptConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "name") {
		return store.ErrMCPPromptNameTaken
	}
	return err
}

func (p *Postgres) CreateMCPPrompt(ctx context.Context, m store.MCPPrompt) (store.MCPPrompt, error) {
	argsJSON, err := json.Marshal(normalizeArgs(m.Arguments))
	if err != nil {
		return store.MCPPrompt{}, err
	}
	out, err := createWithRetry(func() (store.MCPPrompt, error) {
		const q = `INSERT INTO mcp_prompts (name, title, description, body, arguments, position)
		           VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		           RETURNING id::text, name, title, description, body, arguments, position, created_at, updated_at`
		return scanMCPPrompt(p.pool.QueryRow(ctx, q, m.Name, m.Title, m.Description, m.Body, string(argsJSON), m.Position))
	})
	if err != nil {
		return store.MCPPrompt{}, mapMCPPromptConflict(err)
	}
	return out, nil
}

func (p *Postgres) ListMCPPrompts(ctx context.Context) ([]store.MCPPrompt, error) {
	const q = `SELECT id::text, name, title, description, body, arguments, position, created_at, updated_at
	           FROM mcp_prompts ORDER BY position, created_at`
	rows, err := p.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.MCPPrompt
	for rows.Next() {
		m, err := scanMCPPrompt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (p *Postgres) MCPPromptByID(ctx context.Context, id string) (store.MCPPrompt, error) {
	const q = `SELECT id::text, name, title, description, body, arguments, position, created_at, updated_at
	           FROM mcp_prompts WHERE id = $1`
	m, err := scanMCPPrompt(p.pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.MCPPrompt{}, store.ErrMCPPromptNotFound
	}
	return m, err
}

func (p *Postgres) UpdateMCPPrompt(ctx context.Context, m store.MCPPrompt) error {
	argsJSON, err := json.Marshal(normalizeArgs(m.Arguments))
	if err != nil {
		return err
	}
	const q = `UPDATE mcp_prompts
	           SET name = $2, title = $3, description = $4, body = $5, arguments = $6::jsonb, position = $7, updated_at = now()
	           WHERE id = $1`
	ct, err := p.pool.Exec(ctx, q, m.ID, m.Name, m.Title, m.Description, m.Body, string(argsJSON), m.Position)
	if err != nil {
		return mapMCPPromptConflict(err)
	}
	if ct.RowsAffected() == 0 {
		return store.ErrMCPPromptNotFound
	}
	return nil
}

func (p *Postgres) DeleteMCPPrompt(ctx context.Context, id string) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM mcp_prompts WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrMCPPromptNotFound
	}
	return nil
}

// scanMCPPrompt reads one prompt row; the arguments jsonb comes back as bytes.
// It accepts both QueryRow results and a Rows cursor (both expose Scan).
func scanMCPPrompt(row pgx.Row) (store.MCPPrompt, error) {
	var m store.MCPPrompt
	var argsJSON []byte
	if err := row.Scan(&m.ID, &m.Name, &m.Title, &m.Description, &m.Body, &argsJSON, &m.Position, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return store.MCPPrompt{}, err
	}
	if len(argsJSON) > 0 {
		if err := json.Unmarshal(argsJSON, &m.Arguments); err != nil {
			return store.MCPPrompt{}, err
		}
	}
	return m, nil
}

// normalizeArgs keeps a nil slice from marshalling to JSON null (the column is
// NOT NULL and the round-trip should yield [] not null).
func normalizeArgs(a []store.MCPPromptArg) []store.MCPPromptArg {
	if a == nil {
		return []store.MCPPromptArg{}
	}
	return a
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
