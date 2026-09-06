package agentsession

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/peios/acta/internal/agentsession/model"
	"github.com/peios/acta/internal/store"
)

// Projection: the stored transcript is the backend's own frames; what the
// browser receives is the common event model, computed from them here. The
// same per-backend Projector runs over history on read and over each frame on
// ingest, so live and replayed views agree. See internal/agentsession/model.

// NewProjector returns a fresh projector for a backend, or nil for one Acta
// cannot project (its frames then reach the browser as "unknown" events).
func NewProjector(backend string) model.Projector {
	if d := DriverFor(backend); d != nil {
		return d.Projector()
	}
	return nil
}

// sessionProjector is the live projector for one session, guarded so frames
// project in arrival order.
type sessionProjector struct {
	mu sync.Mutex
	p  model.Projector
}

// projectorFor returns the live projector for a session, building it (and
// catching it up on the stored transcript) the first time it is needed after
// a server start.
func (h *Hub) projectorFor(ctx context.Context, sessionID string) *sessionProjector {
	h.mu.Lock()
	if h.projectors == nil {
		h.projectors = map[string]*sessionProjector{}
	}
	sp := h.projectors[sessionID]
	if sp == nil {
		sp = &sessionProjector{}
		h.projectors[sessionID] = sp
	}
	h.mu.Unlock()
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.p == nil {
		as, err := h.svc.store.AgentSessionByID(ctx, sessionID)
		if err != nil {
			return nil
		}
		p := NewProjector(as.Backend)
		if p == nil {
			p = unknownProjector{}
		}
		evs, err := h.svc.Events(ctx, sessionID, 0, 0)
		if err != nil {
			slog.Error("agent session projector catch-up", "session", sessionID, "err", err)
		}
		for _, ev := range evs {
			p.Project(frameOf(ev, true))
		}
		sp.p = p
	}
	return sp
}

// dropProjector forgets a session's live projector (on delete).
func (h *Hub) dropProjector(sessionID string) {
	h.mu.Lock()
	delete(h.projectors, sessionID)
	h.mu.Unlock()
}

func frameOf(ev store.AgentSessionEvent, stored bool) model.Frame {
	return model.Frame{Seq: ev.Seq, Kind: ev.Kind, Payload: json.RawMessage(ev.Payload), At: ev.CreatedAt, Stored: stored}
}

// emit projects one frame (stored or live) through the session's projector
// and fans the events out to the session's browsers.
func (h *Hub) emit(ctx context.Context, sessionID string, ev store.AgentSessionEvent, stored bool) {
	sp := h.projectorFor(ctx, sessionID)
	if sp == nil {
		return
	}
	sp.mu.Lock()
	// a live frame that arrives before its stored predecessor has projected
	// would be out of order; the per-session lock serialises them
	evs := sp.p.Project(frameOf(ev, stored))
	sp.mu.Unlock()
	for _, e := range evs {
		h.fanout(sessionID, e)
		if stored {
			h.maybeAlert(sessionID, e)
		}
	}
}

// History projects a session's whole stored transcript afresh and returns
// the events with a source frame after afterSeq. The projector always starts
// from the beginning: its state at frame N depends on frames 1..N-1.
func (h *Hub) History(ctx context.Context, as store.AgentSession, afterSeq int64) ([]model.Event, int64, error) {
	evs, err := h.svc.Events(ctx, as.ID, 0, 0)
	if err != nil {
		return nil, 0, err
	}
	p := NewProjector(as.Backend)
	if p == nil {
		p = unknownProjector{}
	}
	var out []model.Event
	var last int64
	for _, ev := range evs {
		last = ev.Seq
		for _, e := range p.Project(frameOf(ev, true)) {
			if ev.Seq > afterSeq {
				out = append(out, e)
			}
		}
	}
	return out, last, nil
}

// unknownProjector shows every frame verbatim as an unknown event, for a
// backend Acta has no projector for.
type unknownProjector struct{}

func (unknownProjector) Project(f model.Frame) []model.Event {
	e := model.New(model.Unknown, f).Set("kind", f.Kind)
	e.Live = !f.Stored
	return []model.Event{e}
}

// --- windows ---
//
// A long transcript is not shipped whole: the page opens on its last turns
// and fetches earlier (or, once it has pruned its tail, later) turns as the
// reader scrolls. Cuts fall on turn boundaries — after a turn.idle event —
// so that the pairs a renderer joins (a call and its result, a request and
// its answer, a compaction block) stay together.

// Window is a run of events cut at turn boundaries, and whether more lie
// beyond it in the direction it was asked for.
type Window struct {
	Events []model.Event `json:"events"`
	More   bool          `json:"more"`
}

// turnsOf splits events into turns: each ends with a turn.idle event; a
// trailing run without one is the turn under way.
func turnsOf(evs []model.Event) [][]model.Event {
	var out [][]model.Event
	start := 0
	for i, e := range evs {
		if e.T == model.TurnIdle {
			out = append(out, evs[start:i+1])
			start = i + 1
		}
	}
	if start < len(evs) {
		out = append(out, evs[start:])
	}
	return out
}

// Tail is the last n turns.
func Tail(evs []model.Event, n int) Window {
	turns := turnsOf(evs)
	if n <= 0 || len(turns) <= n {
		return Window{Events: evs}
	}
	keep := turns[len(turns)-n:]
	return Window{Events: evs[len(evs)-countOf(keep):], More: true}
}

// Before is the n turns that end before the event with seq (exclusive).
func Before(evs []model.Event, seq int64, n int) Window {
	cut := len(evs)
	for i, e := range evs {
		if e.Seq >= seq {
			cut = i
			break
		}
	}
	return Tail(evs[:cut], n)
}

// After is the n turns that begin after the event with seq (exclusive).
func After(evs []model.Event, seq int64, n int) Window {
	start := len(evs)
	for i, e := range evs {
		if e.Seq > seq {
			start = i
			break
		}
	}
	rest := evs[start:]
	turns := turnsOf(rest)
	if n <= 0 || len(turns) <= n {
		return Window{Events: rest}
	}
	return Window{Events: rest[:countOf(turns[:n])], More: true}
}

func countOf(turns [][]model.Event) int {
	n := 0
	for _, t := range turns {
		n += len(t)
	}
	return n
}
