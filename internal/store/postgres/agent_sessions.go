package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/peios/acta/internal/store"
)

// --- agent sessions ---

const agentSessionCols = `id, owner_id::text, backend, cwd, title, options, created_at, updated_at`

func scanAgentSession(row interface{ Scan(...any) error }) (store.AgentSession, error) {
	var as store.AgentSession
	var opts []byte
	if err := row.Scan(&as.ID, &as.OwnerID, &as.Backend, &as.Cwd, &as.Title, &opts, &as.CreatedAt, &as.UpdatedAt); err != nil {
		return as, err
	}
	as.Options = map[string]any{}
	if len(opts) > 0 {
		_ = json.Unmarshal(opts, &as.Options)
	}
	return as, nil
}

func (p *Postgres) CreateAgentSession(ctx context.Context, as store.AgentSession) (store.AgentSession, error) {
	if as.ID == "" {
		return store.AgentSession{}, errors.New("postgres: agent session id is required")
	}
	opts := as.Options
	if opts == nil {
		opts = map[string]any{}
	}
	raw, err := json.Marshal(opts)
	if err != nil {
		return store.AgentSession{}, err
	}
	const q = `INSERT INTO agent_sessions (id, owner_id, backend, cwd, title, options)
	           VALUES ($1, $2, $3, $4, $5, $6)
	           RETURNING ` + agentSessionCols
	return scanAgentSession(p.pool.QueryRow(ctx, q, as.ID, as.OwnerID, as.Backend, as.Cwd, as.Title, raw))
}

func (p *Postgres) AgentSessionByID(ctx context.Context, id string) (store.AgentSession, error) {
	as, err := scanAgentSession(p.pool.QueryRow(ctx, `SELECT `+agentSessionCols+` FROM agent_sessions WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.AgentSession{}, store.ErrAgentSessionNotFound
	}
	return as, err
}

func (p *Postgres) AgentSessionsByOwner(ctx context.Context, ownerID string) ([]store.AgentSession, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+agentSessionCols+` FROM agent_sessions WHERE owner_id = $1 ORDER BY updated_at DESC, created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.AgentSession
	for rows.Next() {
		as, err := scanAgentSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, as)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateAgentSessionTitle(ctx context.Context, id, title string, updatedAt time.Time) (store.AgentSession, error) {
	const q = `UPDATE agent_sessions SET title = $2, updated_at = $3 WHERE id = $1 RETURNING ` + agentSessionCols
	as, err := scanAgentSession(p.pool.QueryRow(ctx, q, id, title, updatedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.AgentSession{}, store.ErrAgentSessionNotFound
	}
	return as, err
}

func (p *Postgres) UpdateAgentSessionOptions(ctx context.Context, id string, options map[string]any, updatedAt time.Time) (store.AgentSession, error) {
	if options == nil {
		options = map[string]any{}
	}
	opts, err := json.Marshal(options)
	if err != nil {
		return store.AgentSession{}, err
	}
	const q = `UPDATE agent_sessions SET options = $2, updated_at = $3 WHERE id = $1 RETURNING ` + agentSessionCols
	as, err := scanAgentSession(p.pool.QueryRow(ctx, q, id, opts, updatedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.AgentSession{}, store.ErrAgentSessionNotFound
	}
	return as, err
}

func (p *Postgres) DeleteAgentSession(ctx context.Context, id string) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM agent_sessions WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return store.ErrAgentSessionNotFound
	}
	return nil
}

const agentEventCols = `seq, session_id, kind, payload, created_at`

func scanAgentEvent(row interface{ Scan(...any) error }) (store.AgentSessionEvent, error) {
	var e store.AgentSessionEvent
	err := row.Scan(&e.Seq, &e.SessionID, &e.Kind, &e.Payload, &e.CreatedAt)
	return e, err
}

func (p *Postgres) AppendAgentSessionEvent(ctx context.Context, e store.AgentSessionEvent) (store.AgentSessionEvent, error) {
	payload := e.Payload
	if len(payload) == 0 {
		payload = []byte("null")
	}
	var out store.AgentSessionEvent
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		const q = `INSERT INTO agent_session_events (session_id, kind, payload)
		           VALUES ($1, $2, $3)
		           RETURNING ` + agentEventCols
		var err error
		out, err = scanAgentEvent(tx.QueryRow(ctx, q, e.SessionID, e.Kind, payload))
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign key: no such session
				return store.ErrAgentSessionNotFound
			}
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE agent_sessions SET updated_at = $2 WHERE id = $1`, e.SessionID, out.CreatedAt)
		return err
	})
	return out, err
}

func (p *Postgres) AppendAgentSessionEvents(ctx context.Context, events []store.AgentSessionEvent) ([]store.AgentSessionEvent, error) {
	if len(events) == 0 {
		return nil, nil
	}
	out := make([]store.AgentSessionEvent, 0, len(events))
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		latest := map[string]time.Time{}
		for _, e := range events {
			payload := e.Payload
			if len(payload) == 0 {
				payload = []byte("null")
			}
			var at *time.Time
			if !e.CreatedAt.IsZero() {
				t := e.CreatedAt
				at = &t
			}
			const q = `INSERT INTO agent_session_events (session_id, kind, payload, created_at)
			           VALUES ($1, $2, $3, COALESCE($4, now()))
			           RETURNING ` + agentEventCols
			row, err := scanAgentEvent(tx.QueryRow(ctx, q, e.SessionID, e.Kind, payload, at))
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "23503" {
					return store.ErrAgentSessionNotFound
				}
				return err
			}
			out = append(out, row)
			if row.CreatedAt.After(latest[e.SessionID]) {
				latest[e.SessionID] = row.CreatedAt
			}
		}
		for id, at := range latest {
			if _, err := tx.Exec(ctx, `UPDATE agent_sessions SET updated_at = GREATEST(updated_at, $2) WHERE id = $1`, id, at); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Postgres) AgentSessionEvents(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]store.AgentSessionEvent, error) {
	if _, err := p.AgentSessionByID(ctx, sessionID); err != nil {
		return nil, err
	}
	q := `SELECT ` + agentEventCols + ` FROM agent_session_events WHERE session_id = $1 AND seq > $2 ORDER BY seq`
	args := []any{sessionID, afterSeq}
	if limit > 0 {
		q += ` LIMIT $3`
		args = append(args, limit)
	}
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.AgentSessionEvent
	for rows.Next() {
		e, err := scanAgentEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
