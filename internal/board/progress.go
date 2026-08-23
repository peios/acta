package board

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/peios/acta/internal/store"
)

// Subject types a progress snapshot can be about. They're stored as plain
// strings (progress_snapshots.subject_id carries no FK) so the same history
// machinery serves releases and projects without a table each.
const (
	SubjectRelease = "release"
	SubjectProject = "project"
)

// sizePoints is what an item of each size is worth when measuring progress.
// An unset size counts as a medium: sizing an item is how you mark it as
// unusually large or small, so the unmarked case is the ordinary one — not a
// zero, which would make a board of unsized items look permanently empty.
var sizePoints = map[int]int{
	0: 3, // unset — treated as medium
	1: 1, // XS
	2: 2, // S
	3: 3, // M
	4: 5, // L
	5: 8, // XL
}

// SizePoints is the weight of one item of the given size (board.Sizes values).
func SizePoints(size int) int {
	if p, ok := sizePoints[size]; ok {
		return p
	}
	return sizePoints[0]
}

// Progress is a release's or project's rollup over its top-level items, both as
// a head count and as size-weighted points. Points are the honest measure of
// "how far through are we" — a burn-up counting an XL the same as an XS lurches
// — so Pct, velocity and the forecast all use them, while the item counts stay
// for the human-readable "6 of 11".
type Progress struct {
	DoneItems   int
	TotalItems  int
	DonePoints  int
	TotalPoints int
}

// Pct is completion as a whole percentage of points, 0 for an empty subject.
func (p Progress) Pct() int { return pctOf(p.DonePoints, p.TotalPoints) }

// RemainingPoints is the work left, never negative.
func (p Progress) RemainingPoints() int { return max(p.TotalPoints-p.DonePoints, 0) }

func pctOf(done, total int) int {
	if total <= 0 {
		return 0
	}
	return int(math.Round(float64(done) / float64(total) * 100))
}

// progressFrom weights a store-level per-size tally into a Progress.
func progressFrom(c store.SizeCounts) Progress {
	var p Progress
	for size, n := range c {
		w := SizePoints(size)
		p.TotalItems += n.Total
		p.DoneItems += n.Done
		p.TotalPoints += n.Total * w
		p.DonePoints += n.Done * w
	}
	return p
}

// --- current progress ---

// ReleaseProgress returns per-release progress for a workspace, Done counting
// items in their board's last lane — the same "done = last lane" rule the board
// uses. Releases with no items are absent from the map (their zero value reads
// correctly anyway).
func (s *Service) ReleaseProgress(ctx context.Context, workspaceID string) (map[string]Progress, error) {
	return s.progressBy(ctx, workspaceID, s.store.ReleaseSizeCounts)
}

// ProjectProgress is ReleaseProgress for projects.
func (s *Service) ProjectProgress(ctx context.Context, workspaceID string) (map[string]Progress, error) {
	return s.progressBy(ctx, workspaceID, s.store.ProjectSizeCounts)
}

type sizeCountFn func(ctx context.Context, workspaceID string, doneStatusIDs []string) (map[string]store.SizeCounts, error)

func (s *Service) progressBy(ctx context.Context, workspaceID string, counts sizeCountFn) (map[string]Progress, error) {
	done, err := s.doneStatusIDs(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	raw, err := counts(ctx, workspaceID, done)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Progress, len(raw))
	for id, c := range raw {
		out[id] = progressFrom(c)
	}
	return out, nil
}

// --- snapshots ---

// snapshotFreshness is how long a lazily-written snapshot is trusted before a
// page view refreshes it. The daily sweep is the real writer; this only keeps an
// instance that was asleep at midnight from showing a gap, so re-measuring on
// every render would be pure waste.
const snapshotFreshness = 5 * time.Minute

// snapshotGuard remembers when each workspace was last measured, so
// EnsureSnapshot can be called freely from read paths.
type snapshotGuard struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (g *snapshotGuard) stale(workspaceID string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if at, ok := g.last[workspaceID]; ok && now.Sub(at) < snapshotFreshness {
		return false
	}
	if g.last == nil {
		g.last = map[string]time.Time{}
	}
	g.last[workspaceID] = now
	return true
}

// EnsureSnapshot measures a workspace's progress for today unless it was
// measured in the last few minutes — the write-on-read that keeps history
// unbroken on an instance that isn't running at midnight. A failure to record
// history should never fail the page that asked, so callers may ignore the
// error; it is returned for tests and the sweep.
func (s *Service) EnsureSnapshot(ctx context.Context, workspaceID string) error {
	now := s.now()
	if !s.snapshots.stale(workspaceID, now) {
		return nil
	}
	return s.SnapshotWorkspace(ctx, workspaceID, now)
}

// SnapshotWorkspace records where every release and project in a workspace
// stands at the end of day. Re-running it for the same day overwrites that day's
// rows, so it's safe to call as often as you like.
//
// Shipped releases are skipped: their history froze when they shipped (the ship
// transition takes a final snapshot), and there's no reason to keep writing a
// flat line for a changelog entry forever.
func (s *Service) SnapshotWorkspace(ctx context.Context, workspaceID string, day time.Time) error {
	day = truncDay(day)
	releases, err := s.store.ReleasesByWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	projects, err := s.store.ProjectsByWorkspace(ctx, workspaceID, false)
	if err != nil {
		return err
	}
	relProgress, err := s.ReleaseProgress(ctx, workspaceID)
	if err != nil {
		return err
	}
	projProgress, err := s.ProjectProgress(ctx, workspaceID)
	if err != nil {
		return err
	}
	snaps := make([]store.ProgressSnapshot, 0, len(releases)+len(projects))
	for _, r := range releases {
		if r.Status == "shipped" {
			continue
		}
		snaps = append(snaps, snapshotOf(SubjectRelease, r.ID, day, relProgress[r.ID], false))
	}
	for _, p := range projects {
		snaps = append(snaps, snapshotOf(SubjectProject, p.ID, day, projProgress[p.ID], false))
	}
	return s.store.UpsertProgressSnapshots(ctx, snaps)
}

// SnapshotAll measures every workspace — the daily sweep's body. One workspace's
// failure doesn't stop the others; the first error is returned once all have
// been attempted.
func (s *Service) SnapshotAll(ctx context.Context, day time.Time) error {
	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, ws := range workspaces {
		if err := s.SnapshotWorkspace(ctx, ws.ID, day); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func snapshotOf(subjectType, subjectID string, day time.Time, p Progress, synthetic bool) store.ProgressSnapshot {
	return store.ProgressSnapshot{
		SubjectType: subjectType, SubjectID: subjectID, Day: day,
		DoneItems: p.DoneItems, TotalItems: p.TotalItems,
		DonePoints: p.DonePoints, TotalPoints: p.TotalPoints,
		Synthetic: synthetic,
	}
}

// ProgressHistory returns one subject's daily snapshots from since onward,
// oldest first.
func (s *Service) ProgressHistory(ctx context.Context, subjectType, subjectID string, since time.Time) ([]store.ProgressSnapshot, error) {
	byID, err := s.store.ProgressSnapshotsBySubjects(ctx, subjectType, []string{subjectID}, truncDay(since))
	if err != nil {
		return nil, err
	}
	return byID[subjectID], nil
}

// ProgressHistories is ProgressHistory for many subjects at once, keyed by
// subject id — the overview's sparklines in one round trip.
func (s *Service) ProgressHistories(ctx context.Context, subjectType string, subjectIDs []string, since time.Time) (map[string][]store.ProgressSnapshot, error) {
	return s.store.ProgressSnapshotsBySubjects(ctx, subjectType, subjectIDs, truncDay(since))
}

// --- forecast ---

// VelocityWindow is how far back the forecast looks when measuring pace. Long
// enough to survive a quiet week, short enough to notice that things have
// changed.
const VelocityWindow = 28 * 24 * time.Hour

// Forecast is what a subject's recent pace implies about when it finishes, and
// how that lands against its target date.
type Forecast struct {
	// PointsPerDay is the measured pace over the velocity window; 0 when there's
	// too little history, or when nothing has been completed in it.
	PointsPerDay float64
	// Days is the span of history the pace was measured over.
	Days int
	// Remaining is the points left to do.
	Remaining int
	// ETA is when the remaining work lands at the current pace, nil when there's
	// no pace to extrapolate from (HasPace is false) or the work is done.
	ETA     *time.Time
	HasPace bool
	// Done reports that there's nothing left — the forecast is moot.
	Done bool
	// Target is the subject's target date, if it has one, and DaysLate is how far
	// past it the ETA falls (negative for early). Both are only meaningful when
	// HasTarget is true.
	Target    time.Time
	HasTarget bool
	DaysLate  int
}

// Project works out a subject's pace from its history and extrapolates the
// remaining work from it.
//
// Pace is measured as completed points per day across the velocity window,
// end-to-end rather than as a per-day average, so a burst followed by a quiet
// week reads as the middling pace it was. A subject that has gone backwards
// (work reopened, or scope completed then un-completed) reports no pace rather
// than a negative one — an ETA in the past is worse than no ETA.
func Project(hist []store.ProgressSnapshot, cur Progress, target *time.Time, now time.Time) Forecast {
	f := Forecast{Remaining: cur.RemainingPoints()}
	if target != nil {
		f.Target, f.HasTarget = truncDay(*target), true
	}
	f.Done = cur.TotalPoints > 0 && f.Remaining == 0

	window := truncDay(now.Add(-VelocityWindow))
	var first, last *store.ProgressSnapshot
	for i := range hist {
		if hist[i].Day.Before(window) {
			continue
		}
		if first == nil {
			first = &hist[i]
		}
		last = &hist[i]
	}
	if first != nil && last != nil {
		if days := int(last.Day.Sub(first.Day).Hours() / 24); days > 0 {
			if gained := last.DonePoints - first.DonePoints; gained > 0 {
				f.Days = days
				f.PointsPerDay = float64(gained) / float64(days)
				f.HasPace = true
			}
		}
	}
	if f.HasPace && !f.Done && f.Remaining > 0 {
		eta := truncDay(now).AddDate(0, 0, int(math.Ceil(float64(f.Remaining)/f.PointsPerDay)))
		f.ETA = &eta
	}
	if f.HasTarget && f.ETA != nil {
		f.DaysLate = int(f.ETA.Sub(f.Target).Hours() / 24)
	}
	return f
}

// truncDay floors a time to UTC midnight — snapshots and target dates are days,
// not moments.
func truncDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
