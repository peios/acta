package web

import (
	"strings"
	"testing"
	"time"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
)

// day floors to the chart's day grid, mirroring how snapshots are stored.
func chartDay(n int) time.Time {
	return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
}

// TestBurnupGeometry pins the plot maths: y is inverted (more done = higher up),
// the series spans the full plot width, and the axes land where the template's
// hardcoded gridline expects them. An inverted axis draws a plausible-looking
// chart that says the opposite of the truth, so it's worth asserting.
func TestBurnupGeometry(t *testing.T) {
	hist := []store.ProgressSnapshot{
		{Day: chartDay(0), DonePoints: 0, TotalPoints: 10},
		{Day: chartDay(5), DonePoints: 5, TotalPoints: 10},
		{Day: chartDay(10), DonePoints: 10, TotalPoints: 10},
	}
	cur := board.Progress{DonePoints: 10, TotalPoints: 10}
	c, ok := buildBurnup(hist, cur, board.Forecast{}, chartDay(10))
	if !ok {
		t.Fatal("no chart from three days of history")
	}

	pts := strings.Fields(string(c.DonePoints))
	if len(pts) != 3 {
		t.Fatalf("done line has %d points, want 3", len(pts))
	}
	// Zero sits on the baseline, full sits at the top of the plot, and the
	// halfway point is halfway between the two.
	if pts[0] != "38.0,196.0" {
		t.Errorf("zero done = %s, want the baseline at 38.0,196.0", pts[0])
	}
	if pts[2] != "706.0,12.0" {
		t.Errorf("fully done = %s, want the top right at 706.0,12.0", pts[2])
	}
	if pts[1] != "372.0,104.0" {
		t.Errorf("half done = %s, want the middle at 372.0,104.0", pts[1])
	}
	if c.MaxPoints != 10 {
		t.Errorf("max = %d, want 10", c.MaxPoints)
	}
	if c.HasSynthetic {
		t.Error("no row was synthetic, so nothing should be shaded")
	}
}

func TestBurnupNeedsTwoDays(t *testing.T) {
	one := []store.ProgressSnapshot{{Day: chartDay(0), DonePoints: 1, TotalPoints: 4}}
	if _, ok := buildBurnup(one, board.Progress{DonePoints: 1, TotalPoints: 4}, board.Forecast{}, chartDay(0)); ok {
		t.Error("one day of history is a dot, not a trend")
	}
	if _, ok := buildSparkline(one); ok {
		t.Error("one day of history should not draw a sparkline either")
	}
}

// TestBurnupExtendsToTargetAndForecast checks the x domain grows to cover dates
// beyond the last snapshot — a target line off the right-hand edge would be
// invisible, which is the one moment you most want to see it.
func TestBurnupExtendsToTargetAndForecast(t *testing.T) {
	hist := []store.ProgressSnapshot{
		{Day: chartDay(0), DonePoints: 0, TotalPoints: 10, Synthetic: true},
		{Day: chartDay(4), DonePoints: 4, TotalPoints: 10},
	}
	target := chartDay(20)
	eta := chartDay(10)
	f := board.Forecast{HasTarget: true, Target: target, ETA: &eta, HasPace: true, PointsPerDay: 1}
	c, ok := buildBurnup(hist, board.Progress{DonePoints: 4, TotalPoints: 10}, f, chartDay(4))
	if !ok {
		t.Fatal("no chart")
	}
	// The domain now runs day 0 → day 20, so the target sits on the right edge
	// and the forecast ray lands halfway along it.
	if c.TargetX < 700 || c.TargetX > 706 {
		t.Errorf("target x = %.1f, want the right edge (~706)", c.TargetX)
	}
	if !c.HasProj || !strings.HasSuffix(string(c.ProjPoints), "372.0,12.0") {
		t.Errorf("forecast ray = %q, want it to end at full scope on day 10", c.ProjPoints)
	}
	if !c.HasSynthetic || c.SyntheticW <= 0 {
		t.Errorf("synthetic shading = %.1f wide (has=%v), want a positive width", c.SyntheticW, c.HasSynthetic)
	}
	if last := c.XLabels[len(c.XLabels)-1]; last.Text != "21 Mar" {
		t.Errorf("last x label = %q, want the target day 21 Mar", last.Text)
	}
}
