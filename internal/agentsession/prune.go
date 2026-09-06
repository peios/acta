package agentsession

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/peios/acta/internal/agentsession/claude"
	"github.com/peios/acta/internal/agentsession/codex"
	"github.com/peios/acta/internal/agentsession/model"
	"github.com/peios/acta/internal/store"
)

// Pruning takes chosen parts out of a stored transcript, in place: the
// frames keep their seqs, kinds and times, and their shape, so the page
// still reads and a catch-up still finds its place by the last message.
// Nothing here touches the backend's own transcript on the harness host,
// which is what a resume reads; a re-read from it brings everything back
// (see Hub.Reimport). Explicit only, from the session's storage dialog.

// pruner rewrites one frame of a backend's transcript.
type pruner func(kind string, payload json.RawMessage, cats map[string]bool) (json.RawMessage, bool)

func prunerFor(backend string) pruner {
	switch backend {
	case "claude-code":
		return claude.Prune
	case "codex":
		return codex.Prune
	}
	return nil
}

// PruneEstimate is what one category would save, before compression.
type PruneEstimate struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Note   string `json:"note"`
	Frames int    `json:"frames"`
	Bytes  int64  `json:"bytes"`
}

var pruneLabels = map[string][2]string{
	model.PruneToolOutput: {"Tool output", "what commands printed, files read, search results"},
	model.PruneToolInput:  {"Tool input", "file contents written, long command text, diffs"},
	model.PruneThinking:   {"Thinking", "the model's reasoning between messages"},
	model.PruneImages:     {"Images", "images you attached to messages"},
}

// PruneEstimates says what each category would save for a session, and the
// transcript's size before compression, by running the prune without
// writing.
func (s *Service) PruneEstimates(ctx context.Context, id, ownerID string) ([]PruneEstimate, int64, error) {
	as, err := s.Get(ctx, id, ownerID)
	if err != nil {
		return nil, 0, err
	}
	evs, err := s.store.AgentSessionEvents(ctx, id, 0, 0)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	for _, ev := range evs {
		total += int64(len(ev.Payload))
	}
	pr := prunerFor(as.Backend)
	var out []PruneEstimate
	for _, cat := range model.PruneCategories {
		est := PruneEstimate{ID: cat, Label: pruneLabels[cat][0], Note: pruneLabels[cat][1]}
		if pr != nil {
			one := map[string]bool{cat: true}
			for _, ev := range evs {
				if b, changed := pr(ev.Kind, ev.Payload, one); changed {
					est.Frames++
					est.Bytes += int64(len(ev.Payload) - len(b))
				}
			}
		}
		out = append(out, est)
	}
	return out, total, nil
}

// PruneResult is what a prune did.
type PruneResult struct {
	Frames int
	Saved  int64
}

// ErrNothingToPrune is returned when no category was chosen.
var ErrNothingToPrune = errors.New("nothing chosen to prune")

// Prune rewrites a session's frames with the chosen categories taken out.
func (s *Service) Prune(ctx context.Context, id, ownerID string, cats map[string]bool) (PruneResult, error) {
	var res PruneResult
	if len(cats) == 0 {
		return res, ErrNothingToPrune
	}
	as, err := s.Get(ctx, id, ownerID)
	if err != nil {
		return res, err
	}
	pr := prunerFor(as.Backend)
	if pr == nil {
		return res, nil
	}
	evs, err := s.store.AgentSessionEvents(ctx, id, 0, 0)
	if err != nil {
		return res, err
	}
	updates := map[int64][]byte{}
	for _, ev := range evs {
		if b, changed := pr(ev.Kind, ev.Payload, cats); changed {
			updates[ev.Seq] = b
			res.Frames++
			res.Saved += int64(len(ev.Payload) - len(b))
		}
	}
	if len(updates) == 0 {
		return res, nil
	}
	if err := s.store.UpdateAgentSessionEventPayloads(ctx, id, updates); err != nil {
		return res, err
	}
	return res, nil
}

// ErrSessionRunning is returned when a re-read is asked of a session whose
// process is running: the process is writing the transcript being read.
var ErrSessionRunning = errors.New("the session is running; stop it first")

// Reimport replaces a session's stored transcript with a fresh read of the
// backend's own record on the harness that holds it (or, failing that, any
// harness of the owner: the transcript may be there). Everything stored
// goes first, so what comes back is the record as the backend keeps it;
// what Acta alone recorded (harness states, permission answers) is not
// in it. full asks for the whole transcript rather than the newest turns
// that fit the harness's cap.
func (h *Hub) Reimport(ctx context.Context, ownerID string, as store.AgentSession, full bool) error {
	c := h.harnessFor(ownerID, as.ID)
	if c == nil {
		c = h.harnessByLabel(ownerID, "")
	}
	if c == nil {
		return ErrNoHarness
	}
	if c.v < 3 {
		return ErrHarnessTooOld
	}
	if c.isRunning(as.ID) {
		return ErrSessionRunning
	}
	d := DriverFor(as.Backend)
	if d == nil {
		return errors.New("no driver for backend " + as.Backend)
	}
	cu, ok := d.Transcript(as, nil)
	if !ok {
		return errors.New("backend " + as.Backend + " keeps no transcript to read")
	}
	if err := h.svc.store.DeleteAgentSessionEvents(ctx, as.ID); err != nil {
		return err
	}
	h.Invalidate(as.ID)
	h.openRead(as.ID, "import", "import", c)
	c.send(Outbound{T: FrameRead, Session: as.ID, ID: "import", Backend: as.Backend, Path: cu.Path, Key: cu.Key, Hold: true, Full: full})
	return nil
}

// Holder names the connected harness that holds a session, and whether its
// process is running there; ok is false when none does.
func (h *Hub) Holder(ownerID, sessionID string) (label string, running, ok bool) {
	c := h.harnessFor(ownerID, sessionID)
	if c == nil {
		return "", false, false
	}
	return c.label, c.isRunning(sessionID), true
}

// AnyHarness names one connected harness of the owner, for a re-read of a
// session no harness holds; "" when none is connected.
func (h *Hub) AnyHarness(ownerID string) string {
	if c := h.harnessByLabel(ownerID, ""); c != nil {
		return c.label
	}
	return ""
}

// Invalidate forgets what the hub derived from a session's stored frames
// (the cached projection, the live projector) after they were rewritten or
// dropped; the next reader rebuilds from the store.
func (h *Hub) Invalidate(sessionID string) {
	h.histMu.Lock()
	delete(h.hist, sessionID)
	h.histMu.Unlock()
	h.dropProjector(sessionID)
}
