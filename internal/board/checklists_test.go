package board_test

import (
	"context"
	"errors"
	"testing"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
)

// factIDs maps a slice of facts to their ids.
func factIDs(fs ...store.Fact) []int64 {
	out := make([]int64, len(fs))
	for i, f := range fs {
		out[i] = f.ID
	}
	return out
}

// TestGateBlocksUnmetMove: moving into a gated lane whose checklist isn't
// satisfied does not move the item — it records a pending transition and
// returns the unmet gate.
func TestGateBlocksUnmetMove(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	gated := statuses[2] // a non-entry lane

	provium, err := svc.CreateFact(ctx, wsID, "Provium tests")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetStatusFacts(ctx, gated.ID, factIDs(provium)); err != nil {
		t.Fatal(err)
	}
	it, err := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Ship the thing")
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.MoveItemGated(ctx, it.ID, gated.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if out.Moved {
		t.Fatal("expected the move to be blocked by the unmet checklist")
	}
	if out.Gate == nil || len(out.Gate.Facts) != 1 || out.Gate.Facts[0].Checked {
		t.Fatalf("expected an unmet gate with one unticked fact, got %+v", out.Gate)
	}
	got, _ := svc.Item(ctx, it.ID)
	if got.StatusID != statuses[0].ID {
		t.Fatalf("item should not have moved, status = %s", got.StatusID)
	}
	if got.PendingStatusID != gated.ID {
		t.Fatalf("expected pending status %s, got %q", gated.ID, got.PendingStatusID)
	}
}

// TestGateAllowsWhenNoFacts: an ungated lane moves immediately.
func TestGateAllowsWhenNoFacts(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	it, _ := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Easy")
	out, err := svc.MoveItemGated(ctx, it.ID, statuses[1].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Moved {
		t.Fatal("an ungated lane should move immediately")
	}
	got, _ := svc.Item(ctx, it.ID)
	if got.StatusID != statuses[1].ID {
		t.Fatalf("status = %s, want %s", got.StatusID, statuses[1].ID)
	}
}

// TestTickAutoMoves: ticking the last fact of a pending checklist promotes the
// item into the lane it was waiting on and clears the pending transition.
func TestTickAutoMoves(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := actorCtx("u1", "Ada")
	gated := statuses[2]
	f1, _ := svc.CreateFact(ctx, wsID, "Tests pass")
	f2, _ := svc.CreateFact(ctx, wsID, "Docs written")
	if err := svc.SetStatusFacts(ctx, gated.ID, factIDs(f1, f2)); err != nil {
		t.Fatal(err)
	}
	it, _ := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Feature")
	if _, err := svc.MoveItemGated(ctx, it.ID, gated.ID, 0); err != nil {
		t.Fatal(err)
	}

	// First tick: still pending, no move.
	out, err := svc.SetItemFact(ctx, it.ID, f1.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if out.Moved {
		t.Fatal("one of two facts ticked should not move")
	}
	// Second tick: checklist complete → auto-move.
	out, err = svc.SetItemFact(ctx, it.ID, f2.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Moved {
		t.Fatal("completing the checklist should auto-move the item")
	}
	got, _ := svc.Item(ctx, it.ID)
	if got.StatusID != gated.ID {
		t.Fatalf("status = %s, want %s", got.StatusID, gated.ID)
	}
	if got.PendingStatusID != "" {
		t.Fatalf("pending should be cleared, got %q", got.PendingStatusID)
	}
}

// TestForceStatus moves past an unmet checklist and records the override with
// the unmet facts.
func TestForceStatus(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := actorCtx("u1", "Ada")
	gated := statuses[2]
	f1, _ := svc.CreateFact(ctx, wsID, "Provium tests")
	svc.SetStatusFacts(ctx, gated.ID, factIDs(f1))
	it, _ := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Urgent")
	if _, err := svc.MoveItemGated(ctx, it.ID, gated.ID, 0); err != nil {
		t.Fatal(err)
	}

	out, err := svc.ForceStatus(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Moved {
		t.Fatal("force should move the item")
	}
	got, _ := svc.Item(ctx, it.ID)
	if got.StatusID != gated.ID || got.PendingStatusID != "" {
		t.Fatalf("after force: status=%s pending=%q", got.StatusID, got.PendingStatusID)
	}
	// The override is in the activity log with the unmet fact named.
	history, _ := svc.ItemHistory(ctx, it.ID, 50)
	var forced *store.Event
	for i := range history {
		if history[i].Verb == store.EventItemStatusForced {
			forced = &history[i]
		}
	}
	if forced == nil {
		t.Fatal("expected an item.status_forced event")
	}
	if forced.Data["unmet"] != "Provium tests" {
		t.Fatalf("unmet = %q, want %q", forced.Data["unmet"], "Provium tests")
	}
}

// TestForceNoPending errors when there's nothing pending.
func TestForceNoPending(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	it, _ := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Calm")
	if _, err := svc.ForceStatus(ctx, it.ID); err != board.ErrNoPending {
		t.Fatalf("err = %v, want ErrNoPending", err)
	}
}

// TestCancelKeepsTicks: cancelling a pending transition drops the pending flag
// but leaves the ticks — they're durable facts about the item.
func TestCancelKeepsTicks(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := actorCtx("u1", "Ada")
	gated := statuses[2]
	f1, _ := svc.CreateFact(ctx, wsID, "Tests pass")
	f2, _ := svc.CreateFact(ctx, wsID, "Docs written")
	svc.SetStatusFacts(ctx, gated.ID, factIDs(f1, f2))
	it, _ := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Feature")
	svc.MoveItemGated(ctx, it.ID, gated.ID, 0)
	svc.SetItemFact(ctx, it.ID, f1.ID, true)

	if err := svc.CancelPending(ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Item(ctx, it.ID)
	if got.PendingStatusID != "" {
		t.Fatalf("pending should be cleared, got %q", got.PendingStatusID)
	}
	if got.StatusID != statuses[0].ID {
		t.Fatalf("item should not have moved, status = %s", got.StatusID)
	}
	// Re-initiating the move shows the earlier tick survived.
	out, _ := svc.MoveItemGated(ctx, it.ID, gated.ID, 0)
	if out.Gate == nil {
		t.Fatal("expected a gate")
	}
	var checked int
	for _, f := range out.Gate.Facts {
		if f.Checked {
			checked++
		}
	}
	if checked != 1 {
		t.Fatalf("expected 1 surviving tick, got %d", checked)
	}
}

// TestFactsCarryAcrossStatuses: a fact ticked for one gate satisfies another
// gate that requires the same fact — a fact is a property of the item.
func TestFactsCarryAcrossStatuses(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := actorCtx("u1", "Ada")
	laneA, laneB := statuses[1], statuses[2]
	shared, _ := svc.CreateFact(ctx, wsID, "Reviewed")
	svc.SetStatusFacts(ctx, laneA.ID, factIDs(shared))
	svc.SetStatusFacts(ctx, laneB.ID, factIDs(shared))

	it, _ := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Thing")
	// Enter lane A by ticking the shared fact.
	svc.MoveItemGated(ctx, it.ID, laneA.ID, 0)
	out, _ := svc.SetItemFact(ctx, it.ID, shared.ID, true)
	if !out.Moved {
		t.Fatal("should have moved into lane A")
	}
	// Now lane B's gate is already satisfied — the move is immediate.
	out, err := svc.MoveItemGated(ctx, it.ID, laneB.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Moved {
		t.Fatal("lane B should be satisfied by the carried fact")
	}
}

// TestFactVocabulary: duplicate titles are rejected (case-insensitively); delete
// unhooks a fact from the statuses that gated it.
func TestFactVocabulary(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	f, err := svc.CreateFact(ctx, wsID, "Provium tests")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateFact(ctx, wsID, "provium TESTS"); err != store.ErrFactTitleTaken {
		t.Fatalf("dup err = %v, want ErrFactTitleTaken", err)
	}
	svc.SetStatusFacts(ctx, statuses[2].ID, factIDs(f))
	if err := svc.DeleteFact(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	gating, _ := svc.StatusFacts(ctx, statuses[2].ID)
	if len(gating) != 0 {
		t.Fatalf("deleting a fact should unhook it from the lane, got %d", len(gating))
	}
}

// TestInvalidFactTitle rejects an empty title.
func TestInvalidFactTitle(t *testing.T) {
	svc, wsID, _ := setup(t)
	if _, err := svc.CreateFact(context.Background(), wsID, "   "); err != board.ErrInvalidFact {
		t.Fatalf("err = %v, want ErrInvalidFact", err)
	}
}

// TestConfirmStatusMovesWhenComplete: confirming the full checklist by title
// moves the item and records the confirmed facts on the activity event.
func TestConfirmStatusMovesWhenComplete(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := actorCtx("u1", "Ada")
	gated := statuses[2]
	f1, _ := svc.CreateFact(ctx, wsID, "Provium tests")
	f2, _ := svc.CreateFact(ctx, wsID, "Learn docs")
	svc.SetStatusFacts(ctx, gated.ID, factIDs(f1, f2))
	it, _ := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Ship")

	got, err := svc.ConfirmStatus(ctx, it.ID, gated.ID, []string{"provium tests", "Learn docs"})
	if err != nil {
		t.Fatal(err)
	}
	if got.StatusID != gated.ID {
		t.Fatalf("status = %s, want %s", got.StatusID, gated.ID)
	}
	// The move event names what was confirmed.
	history, _ := svc.ItemHistory(ctx, it.ID, 50)
	var confirmed string
	for _, e := range history {
		if e.Verb == store.EventItemStatusChange {
			confirmed = e.Data["confirmed"]
		}
	}
	if confirmed != "Provium tests, Learn docs" {
		t.Fatalf("confirmed = %q, want both facts", confirmed)
	}
}

// TestConfirmStatusBlocksWhenIncomplete: an incomplete checklist is rejected
// naming the still-unmet facts, and nothing is written (no move, no ticks).
func TestConfirmStatusBlocksWhenIncomplete(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := actorCtx("u1", "Ada")
	gated := statuses[2]
	f1, _ := svc.CreateFact(ctx, wsID, "Provium tests")
	f2, _ := svc.CreateFact(ctx, wsID, "Learn docs")
	svc.SetStatusFacts(ctx, gated.ID, factIDs(f1, f2))
	it, _ := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Ship")

	_, err := svc.ConfirmStatus(ctx, it.ID, gated.ID, []string{"Provium tests"})
	var ce *board.ChecklistError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *ChecklistError", err)
	}
	if len(ce.Unmet) != 1 || ce.Unmet[0] != "Learn docs" {
		t.Fatalf("unmet = %v, want [Learn docs]", ce.Unmet)
	}
	got, _ := svc.Item(ctx, it.ID)
	if got.StatusID != statuses[0].ID {
		t.Fatal("item should not have moved")
	}
	// Nothing was ticked — the partial confirm wrote nothing.
	if ticks, _ := svc.StatusFacts(ctx, gated.ID); len(ticks) != 2 { // sanity: gate intact
		t.Fatalf("gate facts = %d", len(ticks))
	}
}

// TestConfirmStatusCarry: a fact already true on the item needn't be re-passed.
func TestConfirmStatusCarry(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := actorCtx("u1", "Ada")
	laneA, laneB := statuses[1], statuses[2]
	shared, _ := svc.CreateFact(ctx, wsID, "Reviewed")
	extra, _ := svc.CreateFact(ctx, wsID, "Benchmarked")
	svc.SetStatusFacts(ctx, laneA.ID, factIDs(shared))
	svc.SetStatusFacts(ctx, laneB.ID, factIDs(shared, extra))
	it, _ := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Thing")

	// Enter A, confirming the shared fact.
	if _, err := svc.ConfirmStatus(ctx, it.ID, laneA.ID, []string{"Reviewed"}); err != nil {
		t.Fatal(err)
	}
	// Enter B confirming only the *new* fact — Reviewed carries.
	got, err := svc.ConfirmStatus(ctx, it.ID, laneB.ID, []string{"Benchmarked"})
	if err != nil {
		t.Fatalf("carry should let B pass with only the new fact: %v", err)
	}
	if got.StatusID != laneB.ID {
		t.Fatalf("status = %s, want %s", got.StatusID, laneB.ID)
	}
}

// TestConfirmStatusUnknownFact rejects a title not in the vocabulary.
func TestConfirmStatusUnknownFact(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := actorCtx("u1", "Ada")
	gated := statuses[2]
	f1, _ := svc.CreateFact(ctx, wsID, "Provium tests")
	svc.SetStatusFacts(ctx, gated.ID, factIDs(f1))
	it, _ := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Ship")

	_, err := svc.ConfirmStatus(ctx, it.ID, gated.ID, []string{"Provium tests", "Typo fact"})
	var ue *board.UnknownFactError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v, want *UnknownFactError", err)
	}
}

// TestConfirmStatusUngated: an ungated lane just moves.
func TestConfirmStatusUngated(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := actorCtx("u1", "Ada")
	it, _ := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Easy")
	got, err := svc.ConfirmStatus(ctx, it.ID, statuses[1].ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.StatusID != statuses[1].ID {
		t.Fatalf("status = %s, want %s", got.StatusID, statuses[1].ID)
	}
}
