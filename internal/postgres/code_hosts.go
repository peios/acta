package postgres

import (
	"context"
	"errors"
	"time"

	"acta/internal/auth"
	"acta/internal/codehosts"
	"github.com/jackc/pgx/v5"
)

var _ codehosts.Store = (*Store)(nil)

func (s *Store) RenewCodeHost(ctx context.Context, owner string, session []byte, in codehosts.Heartbeat, now time.Time) (codehosts.Host, error) {
	out := codehosts.Host{}
	err := s.pool.QueryRow(ctx, `INSERT INTO code_hosts AS h(owner_id,id,instance_id,session_hash,name,os,arch,last_seen_at,lease_until)
 SELECT $1,$2,$3,b.token_hash,$5,$6,$7,$8,$9 FROM browser_sessions b JOIN accounts a ON a.id=b.account_id
 WHERE b.token_hash=$4 AND b.account_id=$1 AND b.expires_at>$8 AND b.last_seen_at>$10
 AND a.parent_id IS NULL AND a.disabled_at IS NULL AND NOT a.pending
 ON CONFLICT(owner_id,id) DO UPDATE SET instance_id=EXCLUDED.instance_id, session_hash=EXCLUDED.session_hash,
 name=EXCLUDED.name,os=EXCLUDED.os,arch=EXCLUDED.arch,last_seen_at=EXCLUDED.last_seen_at,lease_until=EXCLUDED.lease_until
 WHERE h.instance_id=EXCLUDED.instance_id OR h.lease_until<=$8 OR h.session_hash IS NULL
 OR NOT EXISTS (SELECT 1 FROM browser_sessions b WHERE b.token_hash=h.session_hash AND b.expires_at>$8 AND b.last_seen_at>$10)
 RETURNING id::text,name,os,arch,true,last_seen_at`, owner, in.ID, in.InstanceID, session, in.Name, in.OS, in.Arch, now, now.Add(codehosts.LeaseLifetime), now.Add(-auth.SessionIdle)).Scan(&out.ID, &out.Name, &out.OS, &out.Arch, &out.Online, &out.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = codehosts.ErrInUse
	}
	return out, err
}

func (s *Store) ReleaseCodeHost(ctx context.Context, owner string, session []byte, id, instance string) error {
	_, err := s.pool.Exec(ctx, `UPDATE code_hosts SET lease_until=last_seen_at WHERE owner_id=$1 AND id=$2 AND instance_id=$3 AND session_hash=$4`, owner, id, instance, session)
	return err
}

func (s *Store) ListCodeHosts(ctx context.Context, owner string, now time.Time) ([]codehosts.Host, error) {
	rows, err := s.pool.Query(ctx, `SELECT h.id::text,h.name,h.os,h.arch,
 (h.lease_until>$2 AND EXISTS(SELECT 1 FROM browser_sessions b WHERE b.token_hash=h.session_hash AND b.account_id=h.owner_id AND b.expires_at>$2 AND b.last_seen_at>$3)),h.last_seen_at
 FROM code_hosts h WHERE h.owner_id=$1 ORDER BY lower(h.name),h.id`, owner, now, now.Add(-auth.SessionIdle))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []codehosts.Host{}
	for rows.Next() {
		var h codehosts.Host
		if err = rows.Scan(&h.ID, &h.Name, &h.OS, &h.Arch, &h.Online, &h.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
