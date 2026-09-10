package postgres

import (
	"acta2/internal/guide"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (t workspaceTx) GuidePreference(ctx context.Context, key string) (guide.Preference, error) {
	var out guide.Preference
	err := t.tx.QueryRow(ctx, `SELECT content,revision FROM guide_preferences WHERE scope_key=$1`, key).Scan(&out.Content, &out.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return out, err
}
func (t workspaceTx) SaveGuidePreference(ctx context.Context, key string, in guide.Save) (guide.Preference, error) {
	var out guide.Preference
	// Conditional upsert also protects concurrent first saves. Clearing retains
	// the row and revision so a stale editor cannot resurrect removed policy.
	err := t.tx.QueryRow(ctx, `INSERT INTO guide_preferences(scope_key,account_id,content,updated_by)
 SELECT $1,CASE WHEN $1='site' THEN NULL ELSE $1::uuid END,$2,$3 WHERE $4::bigint=0
 ON CONFLICT(scope_key) DO NOTHING RETURNING content,revision`, key, in.Content, t.actor.ID, in.Revision).Scan(&out.Content, &out.Revision)
	if errors.Is(err, pgx.ErrNoRows) && in.Revision > 0 {
		err = t.tx.QueryRow(ctx, `UPDATE guide_preferences SET content=$2,revision=revision+1,updated_by=$3,updated_at=now() WHERE scope_key=$1 AND revision=$4 RETURNING content,revision`, key, in.Content, t.actor.ID, in.Revision).Scan(&out.Content, &out.Revision)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = guide.ErrConflict
	}
	return out, err
}
