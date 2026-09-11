package postgres

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"time"

	"acta/internal/auth"
	"github.com/jackc/pgx/v5"
)

var _ auth.SecurityStore = (*Store)(nil)

type securityTx struct {
	tx        pgx.Tx
	accountID string
}

func (s *Store) EnsureSecurityKey(ctx context.Context, fingerprint []byte) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO security_key(fingerprint) VALUES($1) ON CONFLICT DO NOTHING`, fingerprint)
	if err != nil {
		return err
	}
	var stored []byte
	if err = s.pool.QueryRow(ctx, `SELECT fingerprint FROM security_key WHERE singleton`).Scan(&stored); err != nil {
		return err
	}
	if !bytes.Equal(stored, fingerprint) {
		return errors.New("security encryption key does not match this database; restore the original ACTA_SECURITY_KEY_FILE")
	}
	return nil
}
func (s *Store) WithSecurity(ctx context.Context, id string, fn func(*auth.SecurityRecord, auth.SecurityTx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	return s.securityRecord(ctx, tx, id, false, fn)
}
func (s *Store) WithLoginSecurity(ctx context.Context, id string, fn func(*auth.SecurityRecord, auth.SecurityTx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	return s.securityRecord(ctx, tx, id, true, fn)
}
func (s *Store) securityRecord(ctx context.Context, tx pgx.Tx, id string, allowInactive bool, fn func(*auth.SecurityRecord, auth.SecurityTx) error) error {
	if err := s.mutateSecurityRecord(ctx, tx, id, allowInactive, fn); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) mutateSecurityRecord(ctx context.Context, tx pgx.Tx, id string, allowInactive bool, fn func(*auth.SecurityRecord, auth.SecurityTx) error) error {
	var err error
	r := auth.SecurityRecord{}
	if err = lockAccountFamily(ctx, tx, id); err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `SELECT security_version,security_changed_at,security_data FROM accounts WHERE id=$1 FOR UPDATE`, id).Scan(&r.Version, &r.ChangedAt, &r.Encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrUnauthenticated
	}
	if err != nil {
		return err
	}
	r.Account, err = scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.id=$1`, id))
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `SELECT password_hash FROM password_credentials WHERE account_id=$1`, id).Scan(&r.PasswordHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if !allowInactive && !r.Account.Available() {
		return auth.ErrUnauthenticated
	}
	beforeHash := r.PasswordHash
	if err = fn(&r, securityTx{tx, id}); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE accounts SET security_version=$2,security_changed_at=$3,security_data=$4,mfa_enrolled=$5 WHERE id=$1`, id, r.Version, r.ChangedAt, r.Encrypted, r.Account.MFAEnrolled); err != nil {
		return err
	}
	if r.PasswordHash != beforeHash {
		if _, err = tx.Exec(ctx, `INSERT INTO password_credentials(account_id,password_hash) VALUES($1,$2) ON CONFLICT(account_id) DO UPDATE SET password_hash=EXCLUDED.password_hash`, id, r.PasswordHash); err != nil {
			return err
		}
	}
	return nil
}
func readFlow(row pgx.Row) (auth.StoredFlow, error) {
	var f auth.StoredFlow
	err := row.Scan(&f.Digest, &f.AccountID, &f.ExpiresAt, &f.Encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		err = auth.ErrNotFound
	}
	return f, err
}
func (s *Store) CreateFlow(ctx context.Context, f auth.StoredFlow) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO security_flows(token_hash,account_id,expires_at,encrypted) VALUES($1,$2,$3,$4)`, f.Digest, f.AccountID, f.ExpiresAt, f.Encrypted)
	return err
}
func (s *Store) ReadFlow(ctx context.Context, digest []byte) (auth.StoredFlow, error) {
	return readFlow(s.pool.QueryRow(ctx, `SELECT token_hash,account_id::text,expires_at,encrypted FROM security_flows WHERE token_hash=$1`, digest))
}
func (t securityTx) Flow(ctx context.Context, digest []byte) (auth.StoredFlow, error) {
	return readFlow(t.tx.QueryRow(ctx, `SELECT token_hash,account_id::text,expires_at,encrypted FROM security_flows WHERE token_hash=$1 AND (account_id=$2 OR account_id IS NULL) FOR UPDATE`, digest, t.accountID))
}
func (t securityTx) SaveFlow(ctx context.Context, f auth.StoredFlow) error {
	_, err := t.tx.Exec(ctx, `UPDATE security_flows SET account_id=$2,expires_at=$3,encrypted=$4 WHERE token_hash=$1`, f.Digest, t.accountID, f.ExpiresAt, f.Encrypted)
	return err
}
func (t securityTx) DeleteFlow(ctx context.Context, digest []byte) error {
	_, err := t.tx.Exec(ctx, `DELETE FROM security_flows WHERE token_hash=$1 AND (account_id=$2 OR account_id IS NULL)`, digest, t.accountID)
	return err
}

const securitySessionColumns = `id::text,token_hash,account_id::text,description,created_at,last_seen_at,expires_at,proof,kind,tool_grants,authorized_by::text`

func scanSession(row pgx.Row) (auth.SecuritySession, error) {
	var s auth.SecuritySession
	err := row.Scan(&s.ID, &s.Digest, &s.AccountID, &s.Description, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.Proof, &s.Kind, &s.Tools, &s.AuthorizedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		err = auth.ErrUnauthenticated
	}
	return s, err
}
func (t securityTx) Session(ctx context.Context, digest []byte, now time.Time) (auth.SecuritySession, error) {
	return t.readSession(ctx, digest, now, true)
}
func (t securityTx) readSession(ctx context.Context, digest []byte, now time.Time, lock bool) (auth.SecuritySession, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	return scanSession(t.tx.QueryRow(ctx, `SELECT `+securitySessionColumns+` FROM browser_sessions WHERE token_hash=$1 AND account_id=$2 AND expires_at>$3 AND last_seen_at>$4`+suffix, digest, t.accountID, now, now.Add(-auth.SessionIdle)))
}
func (t securityTx) Sessions(ctx context.Context, now time.Time) ([]auth.SecuritySession, error) {
	rows, err := t.tx.Query(ctx, `SELECT `+securitySessionColumns+` FROM browser_sessions WHERE account_id=$1 AND expires_at>$2 AND last_seen_at>$3 ORDER BY created_at DESC,id`, t.accountID, now, now.Add(-auth.SessionIdle))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []auth.SecuritySession{}
	for rows.Next() {
		s, e := scanSession(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (t securityTx) PutSession(ctx context.Context, s auth.SecuritySession) error {
	if s.AuthorizedBy == "" {
		s.AuthorizedBy = t.accountID
	}
	if s.Kind == "" {
		s.Kind = "browser"
	}
	if s.Tools == nil {
		s.Tools = []string{}
	}
	_, err := t.tx.Exec(ctx, `INSERT INTO browser_sessions(id,token_hash,account_id,description,created_at,last_seen_at,expires_at,proof,kind,tool_grants,authorized_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(token_hash) DO UPDATE SET proof=EXCLUDED.proof`, s.ID, s.Digest, t.accountID, s.Description, s.CreatedAt, s.LastSeenAt, s.ExpiresAt, s.Proof, s.Kind, s.Tools, s.AuthorizedBy)
	return err
}
func (t securityTx) RevokeSessions(ctx context.Context, keep []byte, id string) error {
	_, err := t.tx.Exec(ctx, `WITH removed AS (DELETE FROM browser_sessions WHERE account_id=$1 AND token_hash<>$2 AND ($3='' OR id::text=$3) RETURNING account_id) INSERT INTO account_events(account_id,kind,occurred_at) SELECT account_id,'session_revoked',now() FROM removed`, t.accountID, keep, id)
	return err
}
func (t securityTx) Event(ctx context.Context, kind string, now time.Time) error {
	_, err := t.tx.Exec(ctx, `INSERT INTO account_events(account_id,kind,occurred_at) VALUES($1,$2,$3)`, t.accountID, kind, now)
	return err
}

func (s *Store) DeleteFlow(ctx context.Context, digest []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM security_flows WHERE token_hash=$1`, digest)
	return err
}
func (t securityTx) InsertFlow(ctx context.Context, f auth.StoredFlow) error {
	_, err := t.tx.Exec(ctx, `INSERT INTO security_flows(token_hash,account_id,expires_at,encrypted) VALUES($1,$2,$3,$4)`, f.Digest, t.accountID, f.ExpiresAt, f.Encrypted)
	return err
}

func (t securityTx) ClaimPasskey(ctx context.Context, id []byte) (bool, error) {
	tag, err := t.tx.Exec(ctx, `INSERT INTO passkey_owners(credential_id,account_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, t.accountID)
	return tag.RowsAffected() == 1, err
}
func (t securityTx) ReleasePasskey(ctx context.Context, id []byte) error {
	_, err := t.tx.Exec(ctx, `DELETE FROM passkey_owners WHERE credential_id=$1 AND account_id=$2`, id, t.accountID)
	return err
}

func (t securityTx) SetMCPGrants(ctx context.Context, id string, previous, grants []string, now time.Time) error {
	session, err := scanSession(t.tx.QueryRow(ctx, `SELECT `+securitySessionColumns+` FROM browser_sessions WHERE account_id=$1 AND id::text=$2 AND kind='mcp' AND expires_at>$3 AND last_seen_at>$4 FOR UPDATE`, t.accountID, id, now, now.Add(-auth.SessionIdle)))
	if errors.Is(err, auth.ErrUnauthenticated) {
		return auth.ErrNotFound
	}
	if err != nil {
		return err
	}
	current := append([]string{}, session.Tools...)
	slices.Sort(current)
	if !slices.Equal(slices.Compact(current), previous) {
		return auth.ErrSessionGrantsChanged
	}
	_, err = t.tx.Exec(ctx, `UPDATE browser_sessions SET tool_grants=$3 WHERE account_id=$1 AND id::text=$2`, t.accountID, id, grants)
	return err
}
