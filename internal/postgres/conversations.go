package postgres

import (
	"acta2/internal/conversation"
	"acta2/internal/threads"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

const conversationVersion = 5

// The caller holds provider_threads FOR UPDATE. Cache only items touched by this
// ingestion batch; historical transcript size never determines live write work.
type conversationTx struct {
	ctx   context.Context
	tx    pgx.Tx
	id    string
	cache map[string]*conversation.Item
	dirty map[string]*conversation.Item
}

func newConversationTx(ctx context.Context, tx pgx.Tx, id string) *conversationTx {
	return &conversationTx{ctx, tx, id, map[string]*conversation.Item{}, map[string]*conversation.Item{}}
}

const itemColumns = `id,position_sequence,position_output,revision,run_id::text,turn_id,visibility,deleted,payload,internal,lane_id`

func scanConversation(row pgx.Row) (*conversation.Item, error) {
	var i conversation.Item
	var payload, internal []byte
	e := row.Scan(&i.ID, &i.Sequence, &i.OutputIndex, &i.Revision, &i.RunID, &i.TurnID, &i.Visibility, &i.Deleted, &payload, &internal, &i.LaneID)
	if e != nil {
		return nil, e
	}
	d := json.NewDecoder(bytes.NewReader(payload))
	d.UseNumber()
	if e = d.Decode(&i.Payload); e != nil {
		return nil, e
	}
	d = json.NewDecoder(bytes.NewReader(internal))
	d.UseNumber()
	if e = d.Decode(&i.Internal); e != nil {
		return nil, e
	}
	return &i, nil
}
func (r *conversationTx) Get(id string) (*conversation.Item, error) {
	if i, ok := r.cache[id]; ok {
		return i, nil
	}
	i, e := scanConversation(r.tx.QueryRow(r.ctx, `SELECT `+itemColumns+` FROM thread_conversation_items WHERE thread_id=$1 AND id=$2`, r.id, id))
	if errors.Is(e, pgx.ErrNoRows) {
		e = nil
	}
	if e == nil {
		r.cache[id] = i
	}
	return i, e
}
func (r *conversationTx) Save(i *conversation.Item) error {
	r.cache[i.ID] = i
	r.dirty[i.ID] = i
	return nil
}
func (r *conversationTx) InTurn(run, turn string) ([]*conversation.Item, error) {
	rows, e := r.tx.Query(r.ctx, `SELECT id FROM thread_conversation_items WHERE thread_id=$1 AND run_id=$2 AND turn_id=$3`, r.id, run, turn)
	if e != nil {
		return nil, e
	}
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids[id] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for id, i := range r.cache {
		if i != nil && i.RunID == run && i.TurnID == turn {
			ids[id] = true
		}
	}
	out := []*conversation.Item{}
	for id := range ids {
		i, e := r.Get(id)
		if e != nil {
			return nil, e
		}
		out = append(out, i)
	}
	return out, nil
}
func (r *conversationTx) flush() error {
	for _, i := range r.dirty {
		p, e := json.Marshal(i.Payload)
		if e != nil {
			return e
		}
		s, e := json.Marshal(i.Internal)
		if e != nil {
			return e
		}
		_, e = r.tx.Exec(r.ctx, `INSERT INTO thread_conversation_items(thread_id,id,position_sequence,position_output,revision,run_id,turn_id,visibility,deleted,payload,internal,lane_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(thread_id,id) DO UPDATE SET position_sequence=EXCLUDED.position_sequence,position_output=EXCLUDED.position_output,revision=EXCLUDED.revision,visibility=EXCLUDED.visibility,deleted=EXCLUDED.deleted,payload=EXCLUDED.payload,internal=EXCLUDED.internal`, r.id, i.ID, i.Sequence, i.OutputIndex, i.Revision, i.RunID, i.TurnID, i.Visibility, i.Deleted, p, s, i.LaneID)
		if e != nil {
			return e
		}
	}
	return nil
}
func loadConversation(ctx context.Context, tx pgx.Tx, id string) (conversation.Current, error) {
	var raw []byte
	var c conversation.Current
	e := tx.QueryRow(ctx, `SELECT conversation_state FROM provider_threads WHERE id=$1`, id).Scan(&raw)
	if e == nil {
		e = json.Unmarshal(raw, &c)
	}
	return c, e
}
func saveConversation(ctx context.Context, tx pgx.Tx, id string, c conversation.Current) error {
	raw, e := json.Marshal(c)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE provider_threads SET conversation_state=$2,conversation_version=$3 WHERE id=$1`, id, raw, conversationVersion)
	return e
}

// Backfill once at startup, before accepting ingestion or page reads. Each thread
// is independently atomic and rerunnable after interruption.
func (s *Store) backfillConversations(ctx context.Context) error {
	rows, e := s.pool.Query(ctx, `SELECT id::text FROM provider_threads WHERE conversation_version<>$1 AND deleted_at IS NULL`, conversationVersion)
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		e = rows.Scan(&id)
		if e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		if e = s.backfillConversation(ctx, id); e != nil {
			return fmt.Errorf("assemble thread %s: %w", id, e)
		}
	}
	return nil
}
func (s *Store) backfillConversation(ctx context.Context, id string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var version int
	if e = tx.QueryRow(ctx, `SELECT conversation_version FROM provider_threads WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&version); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		return e
	}
	if version == conversationVersion {
		return tx.Commit(ctx)
	}
	if _, e = tx.Exec(ctx, `DELETE FROM thread_conversation_items WHERE thread_id=$1`, id); e != nil {
		return e
	}
	c := conversation.Current{}
	var after int64
	for {
		rows, e := tx.Query(ctx, `SELECT payload FROM provider_thread_frames WHERE thread_id=$1 AND sequence>$2 AND sequence<=$2+128 ORDER BY sequence,output_index`, id, after)
		if e != nil {
			return e
		}
		frames := []threads.Frame{}
		for rows.Next() {
			var raw []byte
			if e = rows.Scan(&raw); e != nil {
				rows.Close()
				return e
			}
			var f threads.Frame
			if e = json.Unmarshal(raw, &f); e != nil {
				rows.Close()
				return e
			}
			if f.SchemaVersion == 0 {
				var legacy struct {
					Raw  json.RawMessage `json:"raw"`
					Kind string          `json:"kind"`
				}
				if e = json.Unmarshal(raw, &legacy); e != nil {
					rows.Close()
					return e
				}
				text := string(legacy.Raw)
				kind, stream := "debug/unknown", "stdout"
				if legacy.Kind == "diagnostic" {
					kind, stream = "debug/provider-diagnostic", "stderr"
					_ = json.Unmarshal(legacy.Raw, &text)
				}
				f = threads.NewFrame(f.ProviderFrame, kind, map[string]any{"raw": text, "stream": stream, "reason": "Captured before normalized adapters were installed."})
			}
			frames = append(frames, f)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(frames) == 0 {
			break
		}
		repo := newConversationTx(ctx, tx, id)
		reducer := conversation.Reducer{Store: repo, Current: &c}
		for _, f := range frames {
			if e = reducer.Apply(f); e != nil {
				return e
			}
			after = f.Sequence
		}
		if e = repo.flush(); e != nil {
			return e
		}
	}
	if e = saveConversation(ctx, tx, id, c); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

type positionCursor struct {
	Sequence int64  `json:"s"`
	Output   int    `json:"o"`
	ID       string `json:"i"`
}

func cursor(i *conversation.Item) string {
	b, _ := json.Marshal(positionCursor{i.Sequence, i.OutputIndex, i.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}
func (s *Store) Conversation(ctx context.Context, owner, id string, q conversation.Query) (conversation.Page, error) {
	out := conversation.Page{ProjectionVersion: conversationVersion, LaneID: q.LaneID, ThreadID: id, Items: []*conversation.Item{}}
	var before positionCursor
	if q.Before != "" {
		b, e := base64.RawURLEncoding.DecodeString(q.Before)
		if e != nil {
			return out, conversation.ErrCursor
		}
		if e = json.Unmarshal(b, &before); e != nil || before.Sequence < 1 || before.Output < 0 || before.ID == "" {
			return out, conversation.ErrCursor
		}
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	var state []byte
	if e = tx.QueryRow(ctx, `SELECT last_sequence,conversation_state FROM provider_threads WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL`, id, owner).Scan(&out.Revision, &state); e != nil {
		return out, e
	}
	if e = json.Unmarshal(state, &out.Current); e != nil {
		return out, e
	}
	out.CursorRevision = out.Revision
	limit := q.Limit
	if limit < 1 || limit > 100 {
		limit = 50
	}
	var rows pgx.Rows
	if q.After != nil {
		if *q.After < 0 || *q.After > out.Revision {
			return out, conversation.ErrCursor
		}
		// No fixed item-count cutoff can skip another update at the same revision.
		// Coalesce changed items at a consistent capture watermark.
		out.CursorRevision = min(out.Revision, *q.After+64)
		out.HasMore = out.CursorRevision < out.Revision
		rows, e = tx.Query(ctx, `SELECT `+itemColumns+` FROM thread_conversation_items WHERE thread_id=$1 AND revision>$2 AND revision<=$3 AND lane_id=$5 AND (visibility='normal' OR ($4 AND visibility='debug')) ORDER BY revision,id`, id, *q.After, out.CursorRevision, q.Debug, q.LaneID)
	} else {
		rows, e = tx.Query(ctx, `SELECT `+itemColumns+` FROM thread_conversation_items WHERE thread_id=$1 AND lane_id=$7 AND NOT deleted AND (visibility='normal' OR ($2 AND visibility='debug')) AND ($3::bigint=0 OR (position_sequence,position_output,id)<($3,$4,$5)) ORDER BY position_sequence DESC,position_output DESC,id DESC LIMIT $6`, id, q.Debug, before.Sequence, before.Output, before.ID, limit+1, q.LaneID)
	}
	if e != nil {
		return out, e
	}
	for rows.Next() {
		i, e := scanConversation(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, i)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if q.After == nil {
		if len(out.Items) > limit {
			out.HasMore = true
			out.Items = out.Items[:limit]
		}
		if len(out.Items) > 0 {
			out.Next = cursor(out.Items[len(out.Items)-1])
		}
		sort.Slice(out.Items, func(a, b int) bool {
			i, j := out.Items[a], out.Items[b]
			if i.Sequence != j.Sequence {
				return i.Sequence < j.Sequence
			}
			if i.OutputIndex != j.OutputIndex {
				return i.OutputIndex < j.OutputIndex
			}
			return i.ID < j.ID
		})
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	var clean func(*conversation.Current)
	clean = func(c *conversation.Current) {
		c.MCP = nil
		c.TerminalMCP = nil
		c.OpenMCP = ""
		for _, child := range c.Lanes {
			clean(child)
		}
	}
	clean(&out.Current)
	return out, nil
}
