package board

import (
	"context"
	"errors"
	"strings"

	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

// MaxFactTitleLen bounds a checklist fact's title.
const MaxFactTitleLen = 80

var (
	// ErrInvalidFact is returned for an empty or over-long fact title.
	ErrInvalidFact = errors.New("board: invalid fact title")
	// ErrNoPending is returned when forcing/resolving a transition on an item
	// that has no pending status.
	ErrNoPending = errors.New("board: item has no pending status")
)

// FactView is one checklist fact paired with whether the item in question has
// ticked it — the row the Manage Checklist editor, the gating modal, and the
// Pending band all render.
type FactView struct {
	ID      int64
	Title   string
	Checked bool
}

// PendingGate describes an item's pending transition into a gated lane: the
// target status (name + colour for the band) and its checklist with each fact's
// tick state for this item.
type PendingGate struct {
	StatusID    string
	StatusName  string
	StatusColor string
	Facts       []FactView
}

// Met reports whether every fact in the gate is ticked.
func (g *PendingGate) Met() bool {
	for _, f := range g.Facts {
		if !f.Checked {
			return false
		}
	}
	return true
}

// MoveOutcome is the result of a gated status move. Moved is true when the item
// actually changed lane (the target wasn't gated, or every fact was already
// ticked). When Moved is false the move is pending: the item stays put and Gate
// describes the checklist still to satisfy.
type MoveOutcome struct {
	Moved bool
	Gate  *PendingGate
}

func actorID(ctx context.Context) string {
	if p, ok := identity.FromContext(ctx); ok && p != nil {
		return p.ID
	}
	return ""
}

// --- fact vocabulary (workspace-scoped) ---

// WorkspaceFacts returns a workspace's whole checklist vocabulary, ordered.
func (s *Service) WorkspaceFacts(ctx context.Context, workspaceID string) ([]store.Fact, error) {
	return s.store.FactsByWorkspace(ctx, workspaceID)
}

// CreateFact adds a fact to a workspace's vocabulary, returning ErrFactTitleTaken
// on a duplicate title (case-insensitive) and ErrInvalidFact on a bad title.
func (s *Service) CreateFact(ctx context.Context, workspaceID, title string) (store.Fact, error) {
	t, err := cleanFactTitle(title)
	if err != nil {
		return store.Fact{}, err
	}
	return s.store.CreateFact(ctx, workspaceID, t)
}

// RenameFact retitles a fact everywhere it's referenced (the join rows key on its
// id, so the rename is non-breaking).
func (s *Service) RenameFact(ctx context.Context, id int64, title string) error {
	t, err := cleanFactTitle(title)
	if err != nil {
		return err
	}
	return s.store.RenameFact(ctx, id, t)
}

// DeleteFact removes a fact from the vocabulary, unhooking it from every status
// that required it and every item that ticked it (both cascade).
func (s *Service) DeleteFact(ctx context.Context, id int64) error {
	return s.store.DeleteFact(ctx, id)
}

// --- status gating ---

// StatusFacts returns the facts gating entry into a status, ordered.
func (s *Service) StatusFacts(ctx context.Context, statusID string) ([]store.Fact, error) {
	return s.store.FactsByStatus(ctx, statusID)
}

// SetStatusFacts replaces the set of facts gating a status (the Manage Checklist
// editor saves the whole list).
func (s *Service) SetStatusFacts(ctx context.Context, statusID string, factIDs []int64) error {
	return s.store.SetStatusFacts(ctx, statusID, factIDs)
}

// --- item ticks + the gate ---

// gateFor returns the facts gating toStatusID, each tagged with whether item has
// ticked it, and whether all of them are ticked. A lane with no checklist yields
// (nil, true): nothing to gate.
func (s *Service) gateFor(ctx context.Context, itemID, toStatusID string) ([]FactView, bool, error) {
	gates, err := s.store.FactsByStatus(ctx, toStatusID)
	if err != nil {
		return nil, false, err
	}
	if len(gates) == 0 {
		return nil, true, nil
	}
	ticks, err := s.store.TicksByItem(ctx, itemID)
	if err != nil {
		return nil, false, err
	}
	ticked := make(map[int64]bool, len(ticks))
	for _, t := range ticks {
		ticked[t.FactID] = true
	}
	met := true
	views := make([]FactView, len(gates))
	for i, f := range gates {
		c := ticked[f.ID]
		if !c {
			met = false
		}
		views[i] = FactView{ID: f.ID, Title: f.Title, Checked: c}
	}
	return views, met, nil
}

// gateView resolves a target status and its computed fact views into a
// PendingGate (the shape the UI renders).
func (s *Service) gateView(ctx context.Context, statusID string, facts []FactView) (*PendingGate, error) {
	st, err := s.store.StatusByID(ctx, statusID)
	if err != nil {
		return nil, err
	}
	return &PendingGate{StatusID: st.ID, StatusName: st.Name, StatusColor: ColorFor(st), Facts: facts}, nil
}

// PendingFor returns the item's pending transition, or nil when it has none.
func (s *Service) PendingFor(ctx context.Context, item store.Item) (*PendingGate, error) {
	if item.PendingStatusID == "" {
		return nil, nil
	}
	facts, _, err := s.gateFor(ctx, item.ID, item.PendingStatusID)
	if err != nil {
		return nil, err
	}
	return s.gateView(ctx, item.PendingStatusID, facts)
}

// SetItemFact ticks or unticks one of an item's facts, attributed to the actor.
// If the change satisfies a pending transition's whole checklist, the item moves
// automatically (Moved true on the outcome).
func (s *Service) SetItemFact(ctx context.Context, itemID string, factID int64, checked bool) (MoveOutcome, error) {
	if err := s.store.SetItemFact(ctx, itemID, factID, checked, actorID(ctx)); err != nil {
		return MoveOutcome{}, err
	}
	item, err := s.store.ItemByID(ctx, itemID)
	if err != nil {
		return MoveOutcome{}, err
	}
	if item.PendingStatusID == "" {
		return MoveOutcome{Moved: false}, nil
	}
	facts, met, err := s.gateFor(ctx, itemID, item.PendingStatusID)
	if err != nil {
		return MoveOutcome{}, err
	}
	if !met {
		gate, err := s.gateView(ctx, item.PendingStatusID, facts)
		return MoveOutcome{Moved: false, Gate: gate}, err
	}
	// Checklist complete — promote the item into the lane it was waiting on,
	// recording the confirmed facts on the move (every gate fact is ticked now).
	target := item.PendingStatusID
	if err := s.store.SetItemPending(ctx, itemID, ""); err != nil {
		return MoveOutcome{}, err
	}
	confirmed := make([]string, len(facts))
	for i, f := range facts {
		confirmed[i] = f.Title
	}
	extra := map[string]string{"confirmed": strings.Join(confirmed, ", ")}
	if err := s.setStatus(ctx, itemID, target, extra); err != nil {
		return MoveOutcome{}, err
	}
	return MoveOutcome{Moved: true}, nil
}

// --- gated transitions ---

// MoveItemGated is MoveItem honouring the destination lane's checklist. When the
// lane is gated and the item hasn't satisfied it, the item does NOT move: its
// pending transition is recorded and the outcome carries the unmet Gate. A move
// within the same lane (a reorder) or into an ungated/satisfied lane proceeds.
func (s *Service) MoveItemGated(ctx context.Context, itemID, toStatusID string, index int) (MoveOutcome, error) {
	return s.gatedMove(ctx, itemID, toStatusID, func() error {
		return s.MoveItem(ctx, itemID, toStatusID, index)
	})
}

// SetStatusGated is SetStatus honouring the destination lane's checklist (the
// modal status picker). Same gate semantics as MoveItemGated; positioning is
// SetStatus's (end of lane for a top-level item, in place for a subtask).
func (s *Service) SetStatusGated(ctx context.Context, itemID, toStatusID string) (MoveOutcome, error) {
	return s.gatedMove(ctx, itemID, toStatusID, func() error {
		return s.SetStatus(ctx, itemID, toStatusID)
	})
}

// gatedMove is the shared body of the gated transitions: check the gate, either
// record a pending transition (blocked) or run move() and clear any stale
// pending (proceeded).
func (s *Service) gatedMove(ctx context.Context, itemID, toStatusID string, move func() error) (MoveOutcome, error) {
	item, err := s.store.ItemByID(ctx, itemID)
	if err != nil {
		return MoveOutcome{}, err
	}
	if item.StatusID != toStatusID {
		facts, met, err := s.gateFor(ctx, itemID, toStatusID)
		if err != nil {
			return MoveOutcome{}, err
		}
		if len(facts) > 0 && !met {
			if err := s.store.SetItemPending(ctx, itemID, toStatusID); err != nil {
				return MoveOutcome{}, err
			}
			gate, err := s.gateView(ctx, toStatusID, facts)
			return MoveOutcome{Moved: false, Gate: gate}, err
		}
	}
	if err := move(); err != nil {
		return MoveOutcome{}, err
	}
	// A concrete move resolves any pending intent (even into a different lane).
	if item.PendingStatusID != "" {
		_ = s.store.SetItemPending(ctx, itemID, "")
	}
	return MoveOutcome{Moved: true}, nil
}

// ForceStatus overrides an item's pending checklist and moves it now, recording
// which facts were skipped for the audit trail. Returns ErrNoPending if the item
// has no pending transition.
func (s *Service) ForceStatus(ctx context.Context, itemID string) (MoveOutcome, error) {
	item, err := s.store.ItemByID(ctx, itemID)
	if err != nil {
		return MoveOutcome{}, err
	}
	target := item.PendingStatusID
	if target == "" {
		return MoveOutcome{}, ErrNoPending
	}
	facts, _, err := s.gateFor(ctx, itemID, target)
	if err != nil {
		return MoveOutcome{}, err
	}
	var unmet []string
	for _, f := range facts {
		if !f.Checked {
			unmet = append(unmet, f.Title)
		}
	}
	to, err := s.store.StatusByID(ctx, target)
	if err != nil {
		return MoveOutcome{}, err
	}
	if err := s.store.SetItemPending(ctx, itemID, ""); err != nil {
		return MoveOutcome{}, err
	}
	if err := s.SetStatus(ctx, itemID, target); err != nil {
		return MoveOutcome{}, err
	}
	// Audit the override alongside the move's own status-change event.
	item.StatusID = target
	s.recordEvent(ctx, item, store.EventItemStatusForced,
		map[string]string{"to": to.Name, "unmet": strings.Join(unmet, ", ")})
	return MoveOutcome{Moved: true}, nil
}

// CancelPending drops an item's pending transition (it stays where it is). The
// ticks themselves persist — they're durable facts about the item.
func (s *Service) CancelPending(ctx context.Context, itemID string) error {
	return s.store.SetItemPending(ctx, itemID, "")
}

// ChecklistError reports a gated move blocked by an unmet checklist: the target
// lane and the facts still required (not yet true on the item).
type ChecklistError struct {
	Status string
	Unmet  []string
}

func (e *ChecklistError) Error() string {
	return "checklist incomplete for " + e.Status + " — still required: " + strings.Join(e.Unmet, ", ")
}

// UnknownFactError reports a checklist title absent from the workspace's fact
// vocabulary (a typo, most likely).
type UnknownFactError struct{ Title string }

func (e *UnknownFactError) Error() string { return "unknown fact: " + e.Title }

// ConfirmStatus moves an item into a (possibly gated) status, confirming the
// named facts (by title) as part of the move — the agent (MCP) entry point. It
// is atomic: if the target's checklist is met by the item's already-true facts
// plus the ones named here, those are ticked (attributed to the actor), the item
// moves, and the move event records what was confirmed. Otherwise nothing is
// written and a *ChecklistError naming the still-unmet facts is returned; an
// unrecognised title yields *UnknownFactError. Unlike the UI paths it never
// records a pending transition and never forces.
func (s *Service) ConfirmStatus(ctx context.Context, itemID, statusID string, confirm []string) (store.Item, error) {
	item, err := s.store.ItemByID(ctx, itemID)
	if err != nil {
		return store.Item{}, err
	}
	if err := s.requireStatusInWorkspace(ctx, statusID, item.WorkspaceID); err != nil {
		return store.Item{}, err
	}
	to, err := s.store.StatusByID(ctx, statusID)
	if err != nil {
		return store.Item{}, err
	}
	confirmIDs, err := s.resolveFactTitles(ctx, item.WorkspaceID, confirm)
	if err != nil {
		return store.Item{}, err
	}
	gate, err := s.store.FactsByStatus(ctx, statusID)
	if err != nil {
		return store.Item{}, err
	}
	ticks, err := s.store.TicksByItem(ctx, itemID)
	if err != nil {
		return store.Item{}, err
	}
	ticked := make(map[int64]bool, len(ticks))
	for _, t := range ticks {
		ticked[t.FactID] = true
	}
	// Partition the gate's facts: already true (carried), confirmed now, or still
	// missing.
	var unmet, confirmed []string
	var toTick []store.Fact
	for _, f := range gate {
		switch {
		case ticked[f.ID]:
			// already true — nothing to do
		case confirmIDs[f.ID]:
			toTick = append(toTick, f)
		default:
			unmet = append(unmet, f.Title)
		}
	}
	if len(unmet) > 0 {
		return store.Item{}, &ChecklistError{Status: to.Name, Unmet: unmet}
	}
	by := actorID(ctx)
	for _, f := range toTick {
		if err := s.store.SetItemFact(ctx, itemID, f.ID, true, by); err != nil {
			return store.Item{}, err
		}
		confirmed = append(confirmed, f.Title)
	}
	if item.PendingStatusID != "" {
		_ = s.store.SetItemPending(ctx, itemID, "")
	}
	extra := map[string]string{}
	if len(confirmed) > 0 {
		extra["confirmed"] = strings.Join(confirmed, ", ")
	}
	if err := s.setStatus(ctx, itemID, statusID, extra); err != nil {
		return store.Item{}, err
	}
	return s.store.ItemByID(ctx, itemID)
}

// resolveFactTitles maps fact titles (case-insensitive, trimmed) to a set of
// fact ids, erroring on any title absent from the workspace vocabulary.
func (s *Service) resolveFactTitles(ctx context.Context, workspaceID string, titles []string) (map[int64]bool, error) {
	out := make(map[int64]bool, len(titles))
	if len(titles) == 0 {
		return out, nil
	}
	vocab, err := s.store.FactsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	byTitle := make(map[string]int64, len(vocab))
	for _, f := range vocab {
		byTitle[strings.ToLower(f.Title)] = f.ID
	}
	for _, t := range titles {
		key := strings.ToLower(strings.TrimSpace(t))
		if key == "" {
			continue
		}
		id, ok := byTitle[key]
		if !ok {
			return nil, &UnknownFactError{Title: strings.TrimSpace(t)}
		}
		out[id] = true
	}
	return out, nil
}

func cleanFactTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > MaxFactTitleLen {
		return "", ErrInvalidFact
	}
	return title, nil
}
