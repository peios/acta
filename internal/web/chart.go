package web

import (
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
)

// Burn-up geometry. The chart is plain server-rendered SVG: a release page is a
// document, and a static picture of the history needs no client runtime.
const (
	chartW        = 720
	chartH        = 220
	chartPadL     = 38 // room for the points axis labels
	chartPadR     = 14
	chartPadT     = 12
	chartPadB     = 24 // room for the date labels
	sparkW        = 120
	sparkH        = 26
	sparkPadY     = 3
	historyWindow = 90 * 24 * time.Hour // how much history a chart shows
)

// burnup is everything release.html needs to draw a burn-up: two lines (work
// done, and total scope, so scope growth is visible rather than hidden in a
// shrinking percentage), an optional projection to the forecast ETA, and the
// target date.
//
// A burn-up rather than a burndown on purpose: when scope moves — which is the
// normal case here — a burndown flattens and tells you nothing about why, while
// a rising total line says "the work grew" in the same picture.
type burnup struct {
	W, H        int
	DonePoints  template.HTMLAttr // polyline points for work completed
	TotalPoints template.HTMLAttr // polyline points for total scope
	DoneArea    template.HTMLAttr // closed path under the done line
	// ProjPoints is the dashed ray from today's progress to the forecast ETA at
	// full scope; empty when there's no pace to project from.
	ProjPoints template.HTMLAttr
	HasProj    bool
	// SyntheticW is how far along the x axis the reconstructed (pre-snapshot)
	// history reaches, shaded to mark it as approximate.
	SyntheticW   float64
	HasSynthetic bool
	// TargetX is the target date's position; TargetLate is true when the ETA
	// falls past it, which colours the line.
	TargetX    float64
	HasTarget  bool
	TargetLate bool
	XLabels    []chartLabel
	YLabels    []chartLabel
	MaxPoints  int
}

type chartLabel struct {
	X, Y float64
	Text string
}

// buildBurnup lays out a subject's snapshot history. It returns false when
// there's too little history to draw anything honest — a single day is a dot,
// not a trend.
func buildBurnup(hist []store.ProgressSnapshot, cur board.Progress, f board.Forecast, now time.Time) (burnup, bool) {
	if len(hist) < 2 {
		return burnup{}, false
	}
	first := hist[0].Day
	last := hist[len(hist)-1].Day

	// The x domain runs from the first snapshot to the furthest thing worth
	// showing: today, the target, or where the forecast lands.
	end := last
	if today := truncDay(now); today.After(end) {
		end = today
	}
	if f.HasTarget && f.Target.After(end) {
		end = f.Target
	}
	if f.ETA != nil && f.ETA.After(end) {
		end = *f.ETA
	}
	span := end.Sub(first).Hours() / 24
	if span <= 0 {
		return burnup{}, false
	}

	maxPoints := cur.TotalPoints
	for _, sn := range hist {
		maxPoints = max(maxPoints, sn.TotalPoints)
	}
	if maxPoints <= 0 {
		return burnup{}, false
	}

	plotW := float64(chartW - chartPadL - chartPadR)
	plotH := float64(chartH - chartPadT - chartPadB)
	bottom := float64(chartPadT) + plotH
	x := func(day time.Time) float64 {
		return float64(chartPadL) + day.Sub(first).Hours()/24/span*plotW
	}
	y := func(points int) float64 {
		return bottom - float64(points)/float64(maxPoints)*plotH
	}

	c := burnup{W: chartW, H: chartH, MaxPoints: maxPoints}
	var done, total strings.Builder
	lastSynthetic := -1
	for i, sn := range hist {
		px, py := x(sn.Day), y(sn.DonePoints)
		fmt.Fprintf(&done, "%.1f,%.1f ", px, py)
		fmt.Fprintf(&total, "%.1f,%.1f ", px, y(sn.TotalPoints))
		if sn.Synthetic {
			lastSynthetic = i
		}
	}
	// The shading covers the reconstructed days: up to the first measured day
	// after them, so a single reconstructed day is still a visible band rather
	// than a zero-width sliver.
	if lastSynthetic >= 0 {
		edge := x(last)
		if lastSynthetic+1 < len(hist) {
			edge = x(hist[lastSynthetic+1].Day)
		}
		c.SyntheticW, c.HasSynthetic = edge-float64(chartPadL), true
	}
	c.DonePoints = template.HTMLAttr(strings.TrimSpace(done.String()))
	c.TotalPoints = template.HTMLAttr(strings.TrimSpace(total.String()))
	c.DoneArea = template.HTMLAttr(fmt.Sprintf("M%.1f,%.1f L%s L%.1f,%.1f Z",
		x(first), bottom, strings.TrimSpace(done.String()), x(last), bottom))

	if f.ETA != nil {
		c.ProjPoints = template.HTMLAttr(fmt.Sprintf("%.1f,%.1f %.1f,%.1f",
			x(last), y(hist[len(hist)-1].DonePoints), x(*f.ETA), y(cur.TotalPoints)))
		c.HasProj = true
	}
	if f.HasTarget {
		c.TargetX, c.HasTarget = x(f.Target), true
		c.TargetLate = f.ETA != nil && f.DaysLate > 0
	}

	c.YLabels = []chartLabel{
		{X: float64(chartPadL) - 6, Y: bottom + 3, Text: "0"},
		{X: float64(chartPadL) - 6, Y: y(maxPoints) + 8, Text: fmt.Sprintf("%d", maxPoints)},
	}
	c.XLabels = []chartLabel{
		{X: float64(chartPadL), Y: bottom + 15, Text: first.Format("2 Jan")},
		{X: float64(chartW - chartPadR), Y: bottom + 15, Text: end.Format("2 Jan")},
	}
	return c, true
}

// sparkline is the overview card's thumbnail: completion as a share of scope
// over the same history, small enough to read at a glance and not pretend to
// precision. Points are percentages so cards of wildly different sizes still
// compare.
type sparkline struct {
	W, H   int
	Points template.HTMLAttr
	Rising bool
}

func buildSparkline(hist []store.ProgressSnapshot) (sparkline, bool) {
	if len(hist) < 2 {
		return sparkline{}, false
	}
	pctAt := func(sn store.ProgressSnapshot) float64 {
		if sn.TotalPoints <= 0 {
			return 0
		}
		return float64(sn.DonePoints) / float64(sn.TotalPoints) * 100
	}
	span := float64(len(hist) - 1)
	usableH := float64(sparkH - 2*sparkPadY)
	var b strings.Builder
	for i, sn := range hist {
		px := float64(i) / span * float64(sparkW)
		py := float64(sparkPadY) + (1-pctAt(sn)/100)*usableH
		fmt.Fprintf(&b, "%.1f,%.1f ", px, py)
	}
	return sparkline{
		W: sparkW, H: sparkH,
		Points: template.HTMLAttr(strings.TrimSpace(b.String())),
		Rising: pctAt(hist[len(hist)-1]) > pctAt(hist[0]),
	}, true
}

// truncDay mirrors the board package's day flooring for chart-local maths.
func truncDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
