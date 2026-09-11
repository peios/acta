package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"acta/internal/conversation"
	"acta/internal/threads"
	"github.com/jackc/pgx/v5"
)

func (s *Store) DiscoverThreads(ctx context.Context, owner string, list []threads.Descriptor) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, t := range list {
		raw, err := json.Marshal(t)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO provider_threads(id,owner_id,descriptor) VALUES($1,$2,$3) ON CONFLICT(id) DO NOTHING`, t.ID, owner, raw)
		if err != nil {
			return err
		}
		var priorRaw []byte
		var priorOwner string
		var deleted bool
		if err = tx.QueryRow(ctx, `SELECT owner_id::text,descriptor,deleted_at IS NOT NULL FROM provider_threads WHERE id=$1 FOR UPDATE`, t.ID).Scan(&priorOwner, &priorRaw, &deleted); err != nil {
			return err
		}
		if priorOwner != owner {
			return errors.New("conflicting thread discovery")
		}
		if deleted {
			continue
		}
		var prior threads.Descriptor
		if err = json.Unmarshal(priorRaw, &prior); err != nil {
			return err
		}
		if priorOwner != owner || prior.Provider != t.Provider || prior.CWD != t.CWD || prior.ProviderID != t.ProviderID {
			return errors.New("conflicting thread discovery")
		}
		if prior.Committed && !t.Committed {
			return errors.New("thread commitment cannot regress")
		}
		if t.Revision > prior.Revision {
			if err = providerExitNotification(ctx, tx, owner, prior, t); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE provider_threads SET descriptor=$2 WHERE id=$1`, t.ID, raw); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) ListThreads(ctx context.Context, owner string) ([]threads.Descriptor, error) {
	rows, err := s.pool.Query(ctx, `SELECT descriptor FROM provider_threads WHERE owner_id=$1 AND deleted_at IS NULL ORDER BY descriptor->>'created_at' DESC,id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []threads.Descriptor{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var t threads.Descriptor
		if err = json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) Thread(ctx context.Context, owner, id string) (threads.Descriptor, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT descriptor FROM provider_threads WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL`, id, owner).Scan(&raw)
	var t threads.Descriptor
	if err == nil {
		err = json.Unmarshal(raw, &t)
	}
	return t, err
}
func (s *Store) AppendThreadFrames(ctx context.Context, owner, id string, frames []threads.Frame) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var last int64
	var deleted bool
	if err = tx.QueryRow(ctx, `SELECT last_sequence,deleted_at IS NOT NULL FROM provider_threads WHERE id=$1 AND owner_id=$2 FOR UPDATE`, id, owner).Scan(&last, &deleted); err != nil {
		return 0, err
	}
	var provider string
	if err = tx.QueryRow(ctx, `SELECT descriptor->>'provider' FROM provider_threads WHERE id=$1`, id).Scan(&provider); err != nil {
		return 0, err
	}
	// Deleted threads still acknowledge valid incoming captures so a connected
	// harness does not retry forever. No payload or projection is retained.
	if deleted {
		for start := 0; start < len(frames); {
			end := start + 1
			for end < len(frames) && frames[end].Sequence == frames[start].Sequence {
				end++
			}
			bundle := frames[start:end]
			start = end
			if bundle[0].ThreadID != id || bundle[0].Provider != provider {
				return 0, errors.New("conflicting frame ownership or provider")
			}
			if err = threads.ValidateBundle(bundle); err != nil {
				return 0, err
			}
			last = max(last, bundle[0].Sequence)
		}
		if _, err = tx.Exec(ctx, `UPDATE provider_threads SET last_sequence=$2 WHERE id=$1`, id, last); err != nil {
			return 0, err
		}
		return last, tx.Commit(ctx)
	}
	current, err := loadConversation(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	repo := newConversationTx(ctx, tx, id)
	reducer := conversation.Reducer{Store: repo, Current: &current}
	for start := 0; start < len(frames); {
		end := start + 1
		for end < len(frames) && frames[end].Sequence == frames[start].Sequence {
			end++
		}
		bundle := frames[start:end]
		start = end
		if bundle[0].ThreadID != id || bundle[0].Provider != provider {
			return 0, errors.New("conflicting frame ownership or provider")
		}
		if err = threads.ValidateBundle(bundle); err != nil {
			return 0, err
		}
		sequence := bundle[0].Sequence
		if sequence > last+1 {
			return 0, errors.New("thread frame sequence gap")
		}
		if sequence <= last {
			var count int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM provider_thread_frames WHERE thread_id=$1 AND sequence=$2`, id, sequence).Scan(&count); err != nil {
				return 0, err
			}
			if count != len(bundle) {
				return 0, errors.New("conflicting replayed bundle size")
			}
		}
		for _, f := range bundle {
			raw, err := json.Marshal(f)
			if err != nil {
				return 0, err
			}
			if sequence <= last {
				var prior []byte
				if err = tx.QueryRow(ctx, `SELECT payload FROM provider_thread_frames WHERE thread_id=$1 AND sequence=$2 AND output_index=$3`, id, sequence, f.OutputIndex).Scan(&prior); err != nil {
					return 0, err
				}
				if !bytes.Equal(prior, raw) {
					return 0, errors.New("conflicting replayed thread frame")
				}
			} else if _, err = tx.Exec(ctx, `INSERT INTO provider_thread_frames(thread_id,sequence,output_index,payload) VALUES($1,$2,$3,$4)`, id, sequence, f.OutputIndex, raw); err != nil {
				return 0, err
			}
		}
		if sequence > last {
			for _, f := range bundle {
				if err = reducer.Apply(f); err != nil {
					return 0, err
				}
				if err = frameNotifications(ctx, tx, owner, id, f); err != nil {
					return 0, err
				}
			}
		}
		last = max(last, sequence)
	}
	if err = repo.flush(); err != nil {
		return 0, err
	}
	if err = saveConversation(ctx, tx, id, current); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `UPDATE provider_threads SET last_sequence=$2 WHERE id=$1`, id, last); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return last, nil
}
func (s *Store) ThreadFrames(ctx context.Context, owner, id string, after int64) ([]threads.Frame, error) {
	rows, err := s.pool.Query(ctx, `SELECT f.payload FROM provider_thread_frames f JOIN provider_threads t ON t.id=f.thread_id WHERE t.owner_id=$1 AND t.id=$2 AND t.deleted_at IS NULL AND f.sequence>$3 AND f.sequence <= $3+64 ORDER BY f.sequence,f.output_index`, owner, id, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []threads.Frame{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var f threads.Frame
		if err = json.Unmarshal(raw, &f); err != nil {
			return nil, err
		}
		if f.SchemaVersion == 0 {
			var old struct {
				Raw  json.RawMessage `json:"raw"`
				Kind string          `json:"kind"`
			}
			if err = json.Unmarshal(raw, &old); err != nil {
				return nil, err
			}
			text := string(old.Raw)
			kind := "debug/unknown"
			stream := "stdout"
			if old.Kind == "diagnostic" {
				kind = "debug/provider-diagnostic"
				stream = "stderr"
				_ = json.Unmarshal(old.Raw, &text)
			}
			f = threads.NewFrame(f.ProviderFrame, kind, map[string]any{"raw": text, "stream": stream, "reason": "Captured before normalized adapters were installed."})
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

var _ threads.Store = (*Store)(nil)

func (s *Store) RequestThreadControl(ctx context.Context, owner string, q threads.Control) error {
	raw, err := json.Marshal(q)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var found string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM provider_threads WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL FOR UPDATE`, q.ThreadID, owner).Scan(&found); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO provider_thread_commands(id,owner_id,thread_id,action,payload) SELECT $1,$2,t.id,$4,$5 FROM provider_threads t WHERE t.id=$3 AND t.owner_id=$2 AND t.deleted_at IS NULL ON CONFLICT(id) DO UPDATE SET id=EXCLUDED.id WHERE provider_thread_commands.owner_id=EXCLUDED.owner_id AND provider_thread_commands.thread_id=EXCLUDED.thread_id AND provider_thread_commands.payload=EXCLUDED.payload`, q.ID, owner, q.ThreadID, q.Action, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("invalid or conflicting thread command")
	}
	return tx.Commit(ctx)
}
func (s *Store) PendingThreadControls(ctx context.Context, owner, id string) ([]threads.Control, error) {
	rows, err := s.pool.Query(ctx, `SELECT payload FROM provider_thread_commands WHERE owner_id=$1 AND thread_id=$2 AND outcome IS NULL ORDER BY created_at,id LIMIT 32`, owner, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []threads.Control{}
	for rows.Next() {
		var q threads.Control
		var payload []byte
		if err = rows.Scan(&payload); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(payload, &q); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}
func (s *Store) CompleteThreadControl(ctx context.Context, owner string, result threads.Result) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Match ingestion's thread-first lock order so answers and request frames
	// cannot race each other into an outstanding notification.
	var id string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM provider_threads WHERE id=$1 AND owner_id=$2 FOR UPDATE`, result.ThreadID, owner).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE provider_thread_commands SET outcome=$4 WHERE id=$1 AND owner_id=$2 AND thread_id=$3 AND (outcome IS NULL OR (outcome->>'outcome'='uncertain' AND $5='accepted'))`, result.ID, owner, result.ThreadID, raw, result.Outcome); err != nil {
		return err
	}
	if result.Outcome == "accepted" {
		if err = reconcileNotificationAttention(ctx, tx, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ThreadControl(ctx context.Context, owner, id, command string) (threads.CommandStatus, error) {
	var status threads.CommandStatus
	var raw, payload []byte
	err := s.pool.QueryRow(ctx, `SELECT c.payload,c.outcome,
	 c.payload->>'action'='send' AND EXISTS (
	  SELECT 1 FROM thread_conversation_items i
	  WHERE i.thread_id=c.thread_id AND i.payload->>'kind'='user-message'
	   AND i.payload #>> '{frame,data,submission_id}'=c.id::text
	   AND i.run_id::text=c.payload->>'run_id' AND NOT i.deleted
	   AND i.payload #>> '{frame,data,state}' IN ('in_progress','completed')
	 )
	 FROM provider_thread_commands c WHERE c.owner_id=$1 AND c.thread_id=$2 AND c.id=$3`, owner, id, command).Scan(&payload, &raw, &status.MessageConfirmed)
	if err == nil {
		err = json.Unmarshal(payload, &status.Control)
	}
	if err == nil && raw != nil {
		err = json.Unmarshal(raw, &status.Result)
	}
	return status, err
}

// DeleteThread keeps only an owner-scoped tombstone and ingestion watermark.
// The row lock serializes deletion against discovery and frame projection.
func (s *Store) DeleteThread(ctx context.Context, owner, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var found string
	err = tx.QueryRow(ctx, `SELECT id::text FROM provider_threads WHERE id=$1 AND owner_id=$2 FOR UPDATE`, id, owner).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return threads.ErrNotFound
	}
	if err != nil {
		return err
	}
	for _, table := range []string{"notifications", "provider_thread_commands", "provider_thread_frames", "thread_conversation_items"} {
		if _, err = tx.Exec(ctx, `DELETE FROM `+table+` WHERE thread_id=$1`, id); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE provider_threads SET deleted_at=COALESCE(deleted_at,now()),descriptor=jsonb_build_object('provider',descriptor->>'provider'),conversation_state='{}',conversation_version=0 WHERE id=$1`, id)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
