package board_test

import (
	"context"
	"testing"
	"time"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

// backfillSetup is setup() with a controllable clock, so a test can lay down
// activity "days ago" and then reconstruct it.
func backfillSetup(t *testing.T, clock *time.Time) (*board.Service, string, []store.Status) {
	t.Helper()
	ms := memstore.New()
	ctx := context.Background()
	ws, err := ms.CreateWorkspace(ctx, store.Workspace{Slug: "general", Name: "General"})
	if err != nil {
		t.Fatal(err)
	}
	svc := board.New(ms, board.WithClock(func() time.Time { return *clock }))
	if err := svc.SeedDefaults(ctx, ws.ID); err != nil {
		t.Fatal(err)
	}
	tasks, err := svc.DefaultBoard(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := svc.BoardStatuses(ctx, tasks.ID)
	if err != nil {
		t.Fatal(err)
	}
	return svc, ws.ID, statuses
}

func TestBackfillReconstructsHistoryFromActivity(t *testing.T) {
	clock := day0
	svc, wsID, statuses := backfillSetup(t, &clock)
	ctx := context.Background()
	todo, done := statuses[0].ID, statuses[len(statuses)-1].ID

	rel, err := svc.CreateRelease(ctx, wsID, "v1", "", "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Day 0: two items join the release.
	a, _ := svc.CreateItem(ctx, wsID, todo, "A")
	b, _ := svc.CreateItem(ctx, wsID, todo, "B")
	for _, it := range []store.Item{a, b} {
		if err := svc.SetItemRelease(ctx, it.ID, rel.ID); err != nil {
			t.Fatal(err)
		}
	}
	// Day 2: A is finished.
	clock = day0.AddDate(0, 0, 2)
	if err := svc.SetStatus(ctx, a.ID, done); err != nil {
		t.Fatal(err)
	}
	// Day 3: C joins — scope grew after the fact, which the total line shows.
	clock = day0.AddDate(0, 0, 3)
	c, _ := svc.CreateItem(ctx, wsID, todo, "C")
	if err := svc.SetItemRelease(ctx, c.ID, rel.ID); err != nil {
		t.Fatal(err)
	}

	now := day0.AddDate(0, 0, 4)
	if err := svc.BackfillProgress(ctx, wsID, now); err != nil {
		t.Fatal(err)
	}
	hist, err := svc.ProgressHistory(ctx, board.SubjectRelease, rel.ID, day0.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 5 { // days 0..4 inclusive
		t.Fatalf("history = %d days, want 5", len(hist))
	}
	for i, want := range []struct{ done, total int }{
		{0, 6}, // two unsized items = 3 points each
		{0, 6}, //
		{3, 6}, // A finishes
		{3, 9}, // C joins
		{3, 9}, //
	} {
		if got := hist[i]; got.DonePoints != want.done || got.TotalPoints != want.total {
			t.Errorf("day %d = %d/%d points, want %d/%d", i, got.DonePoints, got.TotalPoints, want.done, want.total)
		}
		if !hist[i].Synthetic {
			t.Errorf("day %d: reconstructed rows must be marked synthetic", i)
		}
	}
}

func TestBackfillNeitherRepeatsNorOverwritesMeasuredHistory(t *testing.T) {
	clock := day0
	svc, wsID, statuses := backfillSetup(t, &clock)
	ctx := context.Background()

	rel, _ := svc.CreateRelease(ctx, wsID, "v1", "", "", nil, "")
	a, _ := svc.CreateItem(ctx, wsID, statuses[0].ID, "A")
	if err := svc.SetItemRelease(ctx, a.ID, rel.ID); err != nil {
		t.Fatal(err)
	}

	now := day0.AddDate(0, 0, 2)
	if err := svc.BackfillProgress(ctx, wsID, now); err != nil {
		t.Fatal(err)
	}
	// A real measurement lands on the last of those days.
	if err := svc.SnapshotWorkspace(ctx, wsID, now); err != nil {
		t.Fatal(err)
	}

	// Running the backfill again must leave everything alone: this subject now
	// has history, and the measured row must survive regardless.
	if err := svc.BackfillProgress(ctx, wsID, now); err != nil {
		t.Fatal(err)
	}
	hist, _ := svc.ProgressHistory(ctx, board.SubjectRelease, rel.ID, day0.AddDate(0, 0, -1))
	if len(hist) != 3 {
		t.Fatalf("history = %d days, want 3", len(hist))
	}
	if last := hist[len(hist)-1]; last.Synthetic {
		t.Error("a re-run backfill overwrote a measured snapshot")
	}
}

func TestBackfillSkipsSubjectsWithNoItems(t *testing.T) {
	clock := day0
	svc, wsID, _ := backfillSetup(t, &clock)
	ctx := context.Background()

	rel, _ := svc.CreateRelease(ctx, wsID, "empty", "", "", nil, "")
	if err := svc.BackfillProgress(ctx, wsID, day0.AddDate(0, 0, 3)); err != nil {
		t.Fatal(err)
	}
	hist, _ := svc.ProgressHistory(ctx, board.SubjectRelease, rel.ID, day0.AddDate(0, 0, -1))
	if len(hist) != 0 {
		t.Fatalf("an empty release got %d reconstructed rows", len(hist))
	}
}
