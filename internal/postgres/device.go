package postgres

import (
	"acta2/internal/auth"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

var _ auth.DeviceStore = (*Store)(nil)

const deviceColumns = `token_hash,code_hash,description,COALESCE(account_id::text,''),state,expires_at,encrypted`

func scanDevice(row pgx.Row) (auth.DeviceRequest, error) {
	var d auth.DeviceRequest
	err := row.Scan(&d.Digest, &d.CodeDigest, &d.Description, &d.AccountID, &d.State, &d.ExpiresAt, &d.Encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		err = auth.ErrDevice
	}
	return d, err
}
func (s *Store) CreateDevice(ctx context.Context, d auth.DeviceRequest) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO device_requests(token_hash,code_hash,description,state,expires_at) VALUES($1,$2,$3,$4,$5)`, d.Digest, d.CodeDigest, d.Description, d.State, d.ExpiresAt)
	return err
}
func (s *Store) ReadDevice(ctx context.Context, digest []byte, byCode bool) (auth.DeviceRequest, error) {
	column := "token_hash"
	if byCode {
		column = "code_hash"
	}
	return scanDevice(s.pool.QueryRow(ctx, `SELECT `+deviceColumns+` FROM device_requests WHERE `+column+`=$1`, digest))
}
func (s *Store) WithDevice(ctx context.Context, id string, digest []byte, fn func(*auth.SecurityRecord, auth.SecurityTx, *auth.DeviceRequest) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	return s.securityRecord(ctx, tx, id, false, func(r *auth.SecurityRecord, st auth.SecurityTx) error {
		d, err := scanDevice(tx.QueryRow(ctx, `SELECT `+deviceColumns+` FROM device_requests WHERE token_hash=$1 FOR UPDATE`, digest))
		if err != nil {
			return err
		}
		if d.AccountID != "" && d.AccountID != id {
			return auth.ErrDevice
		}
		if err = fn(r, st, &d); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE device_requests SET account_id=NULLIF($2,'')::uuid,state=$3,encrypted=$4 WHERE token_hash=$1`, digest, d.AccountID, d.State, d.Encrypted)
		return err
	})
}
