package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/peios/acta/internal/store"
)

// TestPGProgressSnapshots exercises the snapshot SQL against real Postgres: the
// per-day upsert, the rule that a measured row is never displaced by a synthetic
// one, the since filter, and the manual cascade for a polymorphic subject —
// none of which the memstore's map can prove about the actual constraint.
func TestPGProgressSnapshots(t *testing.T) {
	pg := openTestDB(t)
	ctx := context.Background()

	ws, err := pg.CreateWorkspace(ctx, store.Workspace{Slug: "prog", Name: "Progress"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteWorkspace(ctx, ws.ID) })
	rel, err := pg.CreateRelease(ctx, store.Release{WorkspaceID: ws.ID, Name: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteProgressSnapshots(ctx, "release", rel.ID) })

	day := func(n int) time.Time {
		return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
	}
	snap := func(n, done, total int, synthetic bool) store.ProgressSnapshot {
		return store.ProgressSnapshot{
			SubjectType: "release", SubjectID: rel.ID, Day: day(n),
			DoneItems: done, TotalItems: total,
			DonePoints: done * 3, TotalPoints: total * 3,
			Synthetic: synthetic,
		}
	}

	if err := pg.UpsertProgressSnapshots(ctx, []store.ProgressSnapshot{
		snap(0, 0, 2, true), snap(1, 1, 2, true), snap(2, 1, 3, false),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := pg.ProgressSnapshotsBySubjects(ctx, "release", []string{rel.ID}, day(0))
	if err != nil {
		t.Fatal(err)
	}
	rows := got[rel.ID]
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if !rows[0].Day.Equal(day(0)) || rows[2].DonePoints != 3 || rows[2].TotalPoints != 9 {
		t.Fatalf("rows came back wrong: %+v", rows)
	}

	// Re-measuring a day replaces it rather than colliding on the primary key.
	if err := pg.UpsertProgressSnapshots(ctx, []store.ProgressSnapshot{snap(2, 3, 3, false)}); err != nil {
		t.Fatal(err)
	}
	got, _ = pg.ProgressSnapshotsBySubjects(ctx, "release", []string{rel.ID}, day(0))
	if rows = got[rel.ID]; len(rows) != 3 || rows[2].DoneItems != 3 {
		t.Fatalf("same-day upsert didn't replace: %+v", rows)
	}

	// A synthetic write must not overwrite a measured row...
	if err := pg.UpsertProgressSnapshots(ctx, []store.ProgressSnapshot{snap(2, 0, 9, true)}); err != nil {
		t.Fatal(err)
	}
	got, _ = pg.ProgressSnapshotsBySubjects(ctx, "release", []string{rel.ID}, day(0))
	if rows = got[rel.ID]; rows[2].DoneItems != 3 || rows[2].Synthetic {
		t.Fatalf("synthetic write clobbered a measured row: %+v", rows[2])
	}
	// ...but a measured write over a synthetic one is exactly what should happen.
	if err := pg.UpsertProgressSnapshots(ctx, []store.ProgressSnapshot{snap(1, 2, 2, false)}); err != nil {
		t.Fatal(err)
	}
	got, _ = pg.ProgressSnapshotsBySubjects(ctx, "release", []string{rel.ID}, day(0))
	if rows = got[rel.ID]; rows[1].DoneItems != 2 || rows[1].Synthetic {
		t.Fatalf("measured write didn't replace a synthetic row: %+v", rows[1])
	}

	// since filters, and an unknown subject is simply absent.
	got, _ = pg.ProgressSnapshotsBySubjects(ctx, "release", []string{rel.ID, "nope"}, day(2))
	if len(got[rel.ID]) != 1 {
		t.Fatalf("since filter returned %d rows, want 1", len(got[rel.ID]))
	}
	if _, ok := got["nope"]; ok {
		t.Error("an unknown subject should be absent, not empty")
	}

	// The cascade is manual: subject_id carries no FK.
	if err := pg.DeleteProgressSnapshots(ctx, "release", rel.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = pg.ProgressSnapshotsBySubjects(ctx, "release", []string{rel.ID}, day(0))
	if len(got[rel.ID]) != 0 {
		t.Fatalf("delete left %d rows", len(got[rel.ID]))
	}
}

// TestPGReleaseTargetDate checks the target date survives a round trip and can
// be cleared, since it's the one release column that's a date rather than a
// timestamp.
func TestPGReleaseTargetDate(t *testing.T) {
	pg := openTestDB(t)
	ctx := context.Background()

	ws, err := pg.CreateWorkspace(ctx, store.Workspace{Slug: "target", Name: "Target"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteWorkspace(ctx, ws.ID) })

	want := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	rel, err := pg.CreateRelease(ctx, store.Release{WorkspaceID: ws.ID, Name: "v1", TargetDate: &want})
	if err != nil {
		t.Fatal(err)
	}
	if rel.TargetDate == nil || !rel.TargetDate.UTC().Equal(want) {
		t.Fatalf("target on create = %v, want %v", rel.TargetDate, want)
	}
	got, err := pg.ReleaseByID(ctx, rel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetDate == nil || !got.TargetDate.UTC().Equal(want) {
		t.Fatalf("target on read = %v, want %v", got.TargetDate, want)
	}

	moved := want.AddDate(0, 0, 7)
	if err := pg.UpdateRelease(ctx, store.Release{ID: rel.ID, Name: "v1", TargetDate: &moved}); err != nil {
		t.Fatal(err)
	}
	if got, _ = pg.ReleaseByID(ctx, rel.ID); got.TargetDate == nil || !got.TargetDate.UTC().Equal(moved) {
		t.Fatalf("target after move = %v, want %v", got.TargetDate, moved)
	}
	if err := pg.UpdateRelease(ctx, store.Release{ID: rel.ID, Name: "v1"}); err != nil {
		t.Fatal(err)
	}
	if got, _ = pg.ReleaseByID(ctx, rel.ID); got.TargetDate != nil {
		t.Fatalf("target after clear = %v, want nil", got.TargetDate)
	}
}
