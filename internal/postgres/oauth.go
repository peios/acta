package postgres

import (
	"acta2/internal/auth"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ auth.OAuthStore = (*Store)(nil)

func (s *Store) SaveOAuthClient(ctx context.Context, c auth.OAuthClient) error {
	_, e := s.pool.Exec(ctx, `INSERT INTO oauth_clients(id,metadata) VALUES($1,$2)`, c.ID, c)
	return e
}
func (s *Store) OAuthClient(ctx context.Context, id string) (auth.OAuthClient, error) {
	var c auth.OAuthClient
	e := s.pool.QueryRow(ctx, `SELECT metadata FROM oauth_clients WHERE id=$1`, id).Scan(&c)
	if errors.Is(e, pgx.ErrNoRows) {
		e = auth.ErrNotFound
	}
	return c, e
}
func (s *Store) CreateOAuthRequest(ctx context.Context, r auth.OAuthRequest) error {
	_, e := s.pool.Exec(ctx, `INSERT INTO oauth_requests(request_hash,data,expires_at) VALUES($1,$2,$3)`, r.Digest, r, r.ExpiresAt)
	return e
}
func scanOAuthRequest(row pgx.Row) (auth.OAuthRequest, error) {
	var r auth.OAuthRequest
	e := row.Scan(&r)
	if errors.Is(e, pgx.ErrNoRows) {
		e = auth.ErrNotFound
	}
	return r, e
}
func (s *Store) OAuthRequest(ctx context.Context, digest []byte, code bool) (auth.OAuthRequest, error) {
	column := "request_hash"
	if code {
		column = "code_hash"
	}
	return scanOAuthRequest(s.pool.QueryRow(ctx, `SELECT data FROM oauth_requests WHERE `+column+`=$1`, digest))
}
func (s *Store) WithOAuthRequest(ctx context.Context, id string, digest []byte, fn func(*auth.SecurityRecord, auth.SecurityTx, *auth.OAuthRequest) error) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	return s.securityRecord(ctx, tx, id, false, func(r *auth.SecurityRecord, st auth.SecurityTx) error {
		req, e := scanOAuthRequest(tx.QueryRow(ctx, `SELECT data FROM oauth_requests WHERE request_hash=$1 FOR UPDATE`, digest))
		if e != nil {
			return e
		}
		if req.AccountID != "" && req.AccountID != id {
			return auth.ErrUnauthenticated
		}
		if e = fn(r, st, &req); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE oauth_requests SET data=$2,code_hash=$3,expires_at=$4 WHERE request_hash=$1`, digest, req, req.CodeDigest, req.ExpiresAt)
		return e
	})
}

const oauthTokenColumns = `t.token_hash,s.token_hash,s.account_id::text,t.session_id::text,t.client_id,t.resource,t.kind,t.expires_at,t.used`

func scanOAuthToken(row pgx.Row) (auth.OAuthToken, error) {
	var t auth.OAuthToken
	e := row.Scan(&t.Digest, &t.SessionDigest, &t.AccountID, &t.SessionID, &t.ClientID, &t.Resource, &t.Kind, &t.ExpiresAt, &t.Used)
	if errors.Is(e, pgx.ErrNoRows) {
		e = auth.ErrNotFound
	}
	return t, e
}
func (s *Store) ReadOAuthToken(ctx context.Context, digest []byte) (auth.OAuthToken, error) {
	return scanOAuthToken(s.pool.QueryRow(ctx, `SELECT `+oauthTokenColumns+` FROM oauth_tokens t JOIN browser_sessions s ON s.id=t.session_id WHERE t.token_hash=$1`, digest))
}
func (t securityTx) OAuthToken(ctx context.Context, digest []byte) (auth.OAuthToken, error) {
	return scanOAuthToken(t.tx.QueryRow(ctx, `SELECT `+oauthTokenColumns+` FROM oauth_tokens t JOIN browser_sessions s ON s.id=t.session_id WHERE t.token_hash=$1 AND s.account_id=$2 FOR UPDATE OF t`, digest, t.accountID))
}
func (t securityTx) PutOAuthToken(ctx context.Context, v auth.OAuthToken) error {
	result, e := t.tx.Exec(ctx, `INSERT INTO oauth_tokens(token_hash,session_id,client_id,resource,kind,expires_at) SELECT $1,id,$3,$4,$5,$6 FROM browser_sessions WHERE id=$2 AND account_id=$7`, v.Digest, v.SessionID, v.ClientID, v.Resource, v.Kind, v.ExpiresAt, t.accountID)
	if e == nil && result.RowsAffected() != 1 {
		return auth.ErrUnauthenticated
	}
	return e
}
func (t securityTx) ConsumeOAuthToken(ctx context.Context, digest []byte) error {
	_, e := t.tx.Exec(ctx, `UPDATE oauth_tokens SET used=true WHERE token_hash=$1`, digest)
	return e
}
func (t securityTx) TouchSession(ctx context.Context, id string, now time.Time) error {
	_, e := t.tx.Exec(ctx, `UPDATE browser_sessions SET last_seen_at=GREATEST(last_seen_at,$3) WHERE id=$1 AND account_id=$2`, id, t.accountID, now)
	return e
}
