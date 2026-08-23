package board_test

import (
	"context"
	"testing"
	"time"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
)

// day is a fixed date to hang the history tests off, so nothing depends on when
// the suite runs.
var day0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func TestProgressWeightsUnsizedItemsAsMedium(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	todo, done := statuses[0].ID, statuses[len(statuses)-1].ID

	rel, err := svc.CreateRelease(ctx, wsID, "v1", "", "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// An XL, an XS, and one left unsized.
	big, _ := svc.CreateItem(ctx, wsID, todo, "big")
	small, _ := svc.CreateItem(ctx, wsID, todo, "small")
	plain, _ := svc.CreateItem(ctx, wsID, todo, "plain")
	if err := svc.SetSize(ctx, big.ID, 5); err != nil { // XL = 8
		t.Fatal(err)
	}
	if err := svc.SetSize(ctx, small.ID, 1); err != nil { // XS = 1
		t.Fatal(err)
	}
	for _, it := range []store.Item{big, small, plain} {
		if err := svc.SetItemRelease(ctx, it.ID, rel.ID); err != nil {
			t.Fatal(err)
		}
	}

	prog, err := svc.ReleaseProgress(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	// 8 + 1 + 3 (the unsized item counts as a medium).
	if got := prog[rel.ID]; got.TotalPoints != 12 || got.TotalItems != 3 {
		t.Fatalf("total = %d points / %d items, want 12/3", got.TotalPoints, got.TotalItems)
	}

	// Finishing the XL is most of the release by points, but only a third of it
	// by head count — which is the whole reason points are the measure.
	if err := svc.SetStatus(ctx, big.ID, done); err != nil {
		t.Fatal(err)
	}
	prog, _ = svc.ReleaseProgress(ctx, wsID)
	got := prog[rel.ID]
	if got.DonePoints != 8 || got.DoneItems != 1 {
		t.Fatalf("done = %d points / %d items, want 8/1", got.DonePoints, got.DoneItems)
	}
	if got.Pct() != 67 {
		t.Fatalf("pct = %d, want 67 (8 of 12 points)", got.Pct())
	}
	if got.RemainingPoints() != 4 {
		t.Fatalf("remaining = %d, want 4", got.RemainingPoints())
	}
}

func TestSnapshotWorkspaceRecordsAndOverwritesTheDay(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	todo, done := statuses[0].ID, statuses[len(statuses)-1].ID

	rel, _ := svc.CreateRelease(ctx, wsID, "v1", "", "", nil, "")
	a, _ := svc.CreateItem(ctx, wsID, todo, "A")
	b, _ := svc.CreateItem(ctx, wsID, todo, "B")
	for _, it := range []store.Item{a, b} {
		if err := svc.SetItemRelease(ctx, it.ID, rel.ID); err != nil {
			t.Fatal(err)
		}
	}

	if err := svc.SnapshotWorkspace(ctx, wsID, day0); err != nil {
		t.Fatal(err)
	}
	hist, err := svc.ProgressHistory(ctx, board.SubjectRelease, rel.ID, day0.AddDate(0, 0, -7))
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].TotalPoints != 6 || hist[0].DonePoints != 0 {
		t.Fatalf("first snapshot = %+v, want one row at 0/6 points", hist)
	}
	if hist[0].Synthetic {
		t.Error("a measured snapshot must not be marked synthetic")
	}

	// Re-measuring the same day replaces the row rather than appending one.
	if err := svc.SetStatus(ctx, a.ID, done); err != nil {
		t.Fatal(err)
	}
	if err := svc.SnapshotWorkspace(ctx, wsID, day0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	hist, _ = svc.ProgressHistory(ctx, board.SubjectRelease, rel.ID, day0.AddDate(0, 0, -7))
	if len(hist) != 1 {
		t.Fatalf("same-day re-measure appended: %d rows, want 1", len(hist))
	}
	if hist[0].DonePoints != 3 {
		t.Fatalf("done = %d points, want 3", hist[0].DonePoints)
	}

	// A later day is a new row.
	if err := svc.SnapshotWorkspace(ctx, wsID, day0.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	hist, _ = svc.ProgressHistory(ctx, board.SubjectRelease, rel.ID, day0.AddDate(0, 0, -7))
	if len(hist) != 2 {
		t.Fatalf("next day = %d rows, want 2", len(hist))
	}
}

func TestShippingFreezesHistory(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	rel, _ := svc.CreateRelease(ctx, wsID, "v1", "", "", nil, "")
	a, _ := svc.CreateItem(ctx, wsID, statuses[0].ID, "A")
	if err := svc.SetItemRelease(ctx, a.ID, rel.ID); err != nil {
		t.Fatal(err)
	}

	// Shipping takes a final reading of its own...
	if err := svc.SetReleaseStatus(ctx, rel.ID, "shipped"); err != nil {
		t.Fatal(err)
	}
	hist, err := svc.ProgressHistory(ctx, board.SubjectRelease, rel.ID, time.Now().AddDate(0, 0, -7))
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 {
		t.Fatalf("shipping recorded %d rows, want a final 1", len(hist))
	}

	// ...and from then on the sweep leaves it alone.
	if err := svc.SnapshotWorkspace(ctx, wsID, time.Now().AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	hist, _ = svc.ProgressHistory(ctx, board.SubjectRelease, rel.ID, time.Now().AddDate(0, 0, -7))
	if len(hist) != 1 {
		t.Fatalf("a shipped release kept accruing history: %d rows", len(hist))
	}
}

func TestDeletingAReleaseDropsItsHistory(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	rel, _ := svc.CreateRelease(ctx, wsID, "v1", "", "", nil, "")
	a, _ := svc.CreateItem(ctx, wsID, statuses[0].ID, "A")
	if err := svc.SetItemRelease(ctx, a.ID, rel.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SnapshotWorkspace(ctx, wsID, day0); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteRelease(ctx, rel.ID); err != nil {
		t.Fatal(err)
	}
	hist, err := svc.ProgressHistory(ctx, board.SubjectRelease, rel.ID, day0.AddDate(0, 0, -7))
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 0 {
		t.Fatalf("deleted release kept %d history rows", len(hist))
	}
}

func TestProjectForecast(t *testing.T) {
	// Ten days of history at a steady 4 points a day, 60 of 100 points done.
	var hist []store.ProgressSnapshot
	for i := range 11 {
		hist = append(hist, store.ProgressSnapshot{
			Day:         day0.AddDate(0, 0, i),
			DonePoints:  20 + 4*i,
			TotalPoints: 100,
		})
	}
	now := day0.AddDate(0, 0, 10)
	cur := board.Progress{DonePoints: 60, TotalPoints: 100}

	t.Run("pace and eta", func(t *testing.T) {
		f := board.Project(hist, cur, nil, now)
		if !f.HasPace || f.PointsPerDay != 4 {
			t.Fatalf("pace = %v (has=%v), want 4/day", f.PointsPerDay, f.HasPace)
		}
		if f.Remaining != 40 {
			t.Fatalf("remaining = %d, want 40", f.Remaining)
		}
		// 40 points left at 4 a day: ten days out, floored to a whole day.
		wantETA := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)
		if f.ETA == nil || !f.ETA.Equal(wantETA) {
			t.Fatalf("eta = %v, want %v", f.ETA, wantETA)
		}
	})

	t.Run("against a target", func(t *testing.T) {
		target := now.AddDate(0, 0, 4) // 6 days before the work lands
		f := board.Project(hist, cur, &target, now)
		if !f.HasTarget || f.DaysLate != 6 {
			t.Fatalf("days late = %d (target=%v), want 6", f.DaysLate, f.HasTarget)
		}
		early := now.AddDate(0, 0, 30)
		if f := board.Project(hist, cur, &early, now); f.DaysLate != -20 {
			t.Fatalf("days late = %d, want -20 for an early finish", f.DaysLate)
		}
	})

	t.Run("no pace, no guess", func(t *testing.T) {
		// Work that hasn't moved has no measurable pace — and no ETA is better
		// than a made-up one.
		flat := []store.ProgressSnapshot{
			{Day: day0, DonePoints: 20, TotalPoints: 100},
			{Day: day0.AddDate(0, 0, 5), DonePoints: 20, TotalPoints: 100},
		}
		f := board.Project(flat, board.Progress{DonePoints: 20, TotalPoints: 100}, nil, day0.AddDate(0, 0, 5))
		if f.HasPace || f.ETA != nil {
			t.Fatalf("flat history produced a forecast: %+v", f)
		}
	})

	t.Run("finished work", func(t *testing.T) {
		f := board.Project(hist, board.Progress{DonePoints: 100, TotalPoints: 100}, nil, now)
		if !f.Done || f.ETA != nil {
			t.Fatalf("complete work: done=%v eta=%v", f.Done, f.ETA)
		}
	})

	t.Run("history outside the window is ignored", func(t *testing.T) {
		old := []store.ProgressSnapshot{
			{Day: now.AddDate(0, 0, -200), DonePoints: 0, TotalPoints: 100},
			{Day: now.AddDate(0, 0, -190), DonePoints: 60, TotalPoints: 100},
		}
		if f := board.Project(old, cur, nil, now); f.HasPace {
			t.Fatal("stale history should not set a pace")
		}
	})
}

func TestSizePoints(t *testing.T) {
	for _, tc := range []struct{ size, want int }{
		{0, 3}, {1, 1}, {2, 2}, {3, 3}, {4, 5}, {5, 8},
		{99, 3}, // an unknown value falls back to medium rather than vanishing
	} {
		if got := board.SizePoints(tc.size); got != tc.want {
			t.Errorf("SizePoints(%d) = %d, want %d", tc.size, got, tc.want)
		}
	}
}
