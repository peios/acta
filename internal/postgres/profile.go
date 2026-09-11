package postgres

import (
	"context"
	"errors"

	"acta/internal/accounts"
	"acta/internal/auth"
	"github.com/jackc/pgx/v5"
)

var _ accounts.ProfileStore = (*Store)(nil)

func (s *Store) UpdateProfile(ctx context.Context, id string, version int64, username string, displayName *string) (accounts.Account, error) {
	var empty accounts.Account
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if err = lockAccountFamily(ctx, tx, id); err != nil {
		return empty, err
	}
	var current int64
	err = tx.QueryRow(ctx, `SELECT profile_version FROM accounts WHERE id=$1 AND disabled_at IS NULL FOR UPDATE`, id).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, accounts.ErrAccountUnavailable
	}
	if err != nil {
		return empty, err
	}
	if current != version {
		return empty, accounts.ErrProfileChanged
	}
	a, err := scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.id=$1`, id))
	if err != nil {
		return empty, err
	}
	if accounts.RequiresMFASetup(a) {
		return empty, auth.ErrMFARequired
	}
	if !accounts.CanChangeProfile(a, username, displayName) {
		return empty, auth.ErrForbidden
	}
	// The database claim trigger reserves both old and new names in the same
	// transaction. A failed claim also rolls back display-name and version edits.
	_, err = tx.Exec(ctx, `UPDATE accounts SET username=$2,display_name=$3,profile_version=profile_version+1 WHERE id=$1`, id, username, displayName)
	if err != nil {
		return empty, nameError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO account_events(account_id,kind,occurred_at) VALUES($1,'profile_updated',now())`, id); err != nil {
		return empty, err
	}
	a, err = scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN accounts p ON p.id=a.parent_id WHERE a.id=$1`, id))
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return a, nil
}
