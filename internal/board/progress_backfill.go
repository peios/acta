package board

import (
	"context"
	"time"

	"github.com/peios/acta/internal/store"
)

// backfillEventLimit is how much activity the reconstruction reads per
// workspace. Deep enough to cover the life of a release, bounded so a busy
// workspace can't turn startup into a table scan.
const backfillEventLimit = 5000

// backfillDays bounds how far back a reconstruction runs. A year-old release
// doesn't need a daily row for every day of its life to make its shape legible.
const backfillDays = 180

// BackfillAllProgress runs BackfillProgress across every workspace. One
// workspace's failure doesn't stop the others; the first error is returned once
// all have been attempted.
func (s *Service) BackfillAllProgress(ctx context.Context, now time.Time) error {
	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, ws := range workspaces {
		if err := s.BackfillProgress(ctx, ws.ID, now); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// BackfillProgress reconstructs a best-effort progress history for every
// release and project in a workspace that has none yet, from the activity log,
// and writes it as synthetic snapshots. It exists so a chart has something to
// show on the day the feature lands rather than starting from a blank page; from
// then on the daily sweep records the real thing, and a measured row is never
// overwritten by this (see UpsertProgressSnapshots).
//
// What it can and can't recover, since a reader deserves to know what they're
// looking at:
//
//   - When an item joined a subject comes from its item.release / item.project
//     events, falling back to the item's creation for memberships older than the
//     event log.
//   - Whether an item was done on a given day comes from its status changes,
//     matched against the lanes that are "done" *today*, because events record
//     lane names rather than ids. A renamed or reordered lane misreads.
//   - Item sizes are today's sizes: the log records size changes, but weighting
//     each day by the size the item had at the time is more precision than a
//     reconstruction deserves.
//   - Items that have since left a subject (or were archived) are invisible, so
//     the reconstructed total line understates past scope churn.
//
// Subjects that already have snapshots are skipped entirely, so this is safe to
// re-run.
func (s *Service) BackfillProgress(ctx context.Context, workspaceID string, now time.Time) error {
	releases, err := s.store.ReleasesByWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	projects, err := s.store.ProjectsByWorkspace(ctx, workspaceID, false)
	if err != nil {
		return err
	}
	if len(releases) == 0 && len(projects) == 0 {
		return nil
	}

	// Which subjects are missing history, keyed by the name the activity log
	// refers to them by ("to" on an item.release / item.project event).
	pending := map[string]map[string]string{} // subject type -> name -> id
	relIDs := make([]string, 0, len(releases))
	for _, r := range releases {
		relIDs = append(relIDs, r.ID)
	}
	projIDs := make([]string, 0, len(projects))
	for _, p := range projects {
		projIDs = append(projIDs, p.ID)
	}
	haveRel, err := s.store.ProgressSnapshotsBySubjects(ctx, SubjectRelease, relIDs, time.Time{})
	if err != nil {
		return err
	}
	haveProj, err := s.store.ProgressSnapshotsBySubjects(ctx, SubjectProject, projIDs, time.Time{})
	if err != nil {
		return err
	}
	pending[SubjectRelease] = map[string]string{}
	for _, r := range releases {
		if len(haveRel[r.ID]) == 0 {
			pending[SubjectRelease][r.Name] = r.ID
		}
	}
	pending[SubjectProject] = map[string]string{}
	for _, p := range projects {
		if len(haveProj[p.ID]) == 0 {
			pending[SubjectProject][p.Name] = p.ID
		}
	}
	if len(pending[SubjectRelease]) == 0 && len(pending[SubjectProject]) == 0 {
		return nil
	}

	// Current membership and the "done" lanes, so we know which items to trace.
	doneIDs, err := s.doneStatusIDs(ctx, workspaceID)
	if err != nil {
		return err
	}
	doneLanes, err := s.doneLaneNames(ctx, workspaceID, doneIDs)
	if err != nil {
		return err
	}
	doneSet := make(map[string]bool, len(doneIDs))
	for _, id := range doneIDs {
		doneSet[id] = true
	}

	members := map[string]map[string][]store.Item{} // subject type -> subject id -> items
	members[SubjectRelease] = map[string][]store.Item{}
	for _, id := range pending[SubjectRelease] {
		items, err := s.store.ItemsByRelease(ctx, id)
		if err != nil {
			return err
		}
		members[SubjectRelease][id] = items
	}
	members[SubjectProject] = map[string][]store.Item{}
	for _, id := range pending[SubjectProject] {
		items, err := s.store.ItemsByProject(ctx, id)
		if err != nil {
			return err
		}
		members[SubjectProject][id] = items
	}

	events, err := s.store.EventsByWorkspace(ctx, workspaceID, backfillEventLimit)
	if err != nil {
		return err
	}
	joined, finished := replayItemDays(events, pending, doneLanes)

	today := truncDay(now)
	var snaps []store.ProgressSnapshot
	for _, subjectType := range []string{SubjectRelease, SubjectProject} {
		for subjectID, items := range members[subjectType] {
			snaps = append(snaps, synthesize(subjectType, subjectID, items,
				joined[subjectID], finished, doneSet, today)...)
		}
	}
	return s.store.UpsertProgressSnapshots(ctx, snaps)
}

// doneLaneNames is the set of lane names that currently mean "done" — the
// bridge between status events (which record names) and the positional
// last-lane rule.
func (s *Service) doneLaneNames(ctx context.Context, workspaceID string, doneIDs []string) (map[string]bool, error) {
	statuses, err := s.Statuses(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(doneIDs))
	for _, id := range doneIDs {
		want[id] = true
	}
	names := map[string]bool{}
	for _, st := range statuses {
		if want[st.ID] {
			names[st.Name] = true
		}
	}
	return names, nil
}

// replayItemDays walks the activity log oldest-first and works out, per item,
// the day it joined each pending subject and the day it last became done.
//
// joined is keyed subject id -> item id -> day; finished is item id -> day, and
// tracks the *latest* move into a done lane, so an item that was reopened and
// finished again counts from the second time.
func replayItemDays(events []store.Event, pending map[string]map[string]string, doneLanes map[string]bool) (map[string]map[string]time.Time, map[string]time.Time) {
	joined := map[string]map[string]time.Time{}
	finished := map[string]time.Time{}
	// EventsByWorkspace returns newest-first; membership wants the earliest join,
	// so walk it backwards.
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		day := truncDay(e.CreatedAt)
		switch e.Verb {
		case store.EventItemRelease, store.EventItemProject:
			subjectType := SubjectRelease
			if e.Verb == store.EventItemProject {
				subjectType = SubjectProject
			}
			id, ok := pending[subjectType][e.Data["to"]]
			if !ok {
				continue
			}
			if joined[id] == nil {
				joined[id] = map[string]time.Time{}
			}
			if _, seen := joined[id][e.ItemID]; !seen {
				joined[id][e.ItemID] = day
			}
		case store.EventItemStatusChange, store.EventItemStatusForced:
			if doneLanes[e.Data["to"]] {
				finished[e.ItemID] = day
			} else {
				delete(finished, e.ItemID)
			}
		}
	}
	return joined, finished
}

// synthesize turns one subject's per-item join and finish days into a daily
// series, from the day the first item joined up to today.
func synthesize(subjectType, subjectID string, items []store.Item, joined map[string]time.Time, finished map[string]time.Time, doneSet map[string]bool, today time.Time) []store.ProgressSnapshot {
	if len(items) == 0 {
		return nil
	}
	type member struct {
		join   time.Time
		finish *time.Time
		points int
	}
	members := make([]member, 0, len(items))
	first := today
	for _, it := range items {
		m := member{join: truncDay(it.CreatedAt), points: SizePoints(it.Size)}
		if d, ok := joined[it.ID]; ok {
			m.join = d
		}
		// Only an item that is done *now* gets a finish day: the log tells us when
		// it last crossed into a done lane, and its current lane says whether it
		// stayed there.
		if doneSet[it.StatusID] {
			d, ok := finished[it.ID]
			if !ok || d.Before(m.join) {
				d = m.join
			}
			m.finish = &d
		}
		if m.join.Before(first) {
			first = m.join
		}
		members = append(members, m)
	}

	if earliest := today.AddDate(0, 0, -backfillDays); first.Before(earliest) {
		first = earliest
	}
	var out []store.ProgressSnapshot
	for day := first; !day.After(today); day = day.AddDate(0, 0, 1) {
		var p Progress
		for _, m := range members {
			if m.join.After(day) {
				continue
			}
			p.TotalItems++
			p.TotalPoints += m.points
			if m.finish != nil && !m.finish.After(day) {
				p.DoneItems++
				p.DonePoints += m.points
			}
		}
		out = append(out, snapshotOf(subjectType, subjectID, day, p, true))
	}
	return out
}
