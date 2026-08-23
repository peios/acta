package board_test

import (
	"context"
	"errors"
	"testing"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

func TestReleaseCreateAndNameUniqueness(t *testing.T) {
	svc, wsID, _ := setup(t)
	ctx := context.Background()

	r1, err := svc.CreateRelease(ctx, wsID, "v0.27.0", "next cut", "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if r1.Status != "active" {
		t.Fatalf("default status = %q, want active", r1.Status)
	}
	if r1.ShippedAt != nil {
		t.Fatal("a fresh release must not be shipped")
	}
	// The name is unique within the workspace, case-insensitively.
	if _, err := svc.CreateRelease(ctx, wsID, "V0.27.0", "", "", nil, ""); !errors.Is(err, store.ErrReleaseNameTaken) {
		t.Fatalf("duplicate name: want ErrReleaseNameTaken, got %v", err)
	}
}

func TestReleaseValidation(t *testing.T) {
	svc, wsID, _ := setup(t)
	ctx := context.Background()

	if _, err := svc.CreateRelease(ctx, wsID, "   ", "", "", nil, ""); !errors.Is(err, board.ErrInvalidReleaseName) {
		t.Fatalf("blank name: want ErrInvalidReleaseName, got %v", err)
	}
}

func TestItemReleaseAssignmentAndProgress(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	todo, done := statuses[0].ID, statuses[len(statuses)-1].ID

	rel, err := svc.CreateRelease(ctx, wsID, "v0.27.0", "", "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := svc.CreateItem(ctx, wsID, todo, "A")
	b, _ := svc.CreateItem(ctx, wsID, todo, "B")
	svc.CreateItem(ctx, wsID, todo, "C") // left unfiled

	if err := svc.SetItemRelease(ctx, a.ID, rel.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetItemRelease(ctx, b.ID, rel.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetStatus(ctx, b.ID, done); err != nil { // B reaches the last lane
		t.Fatal(err)
	}

	items, err := svc.ReleaseItems(ctx, rel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("release items = %d, want 2 (unfiled C excluded)", len(items))
	}

	prog, err := svc.ReleaseProgress(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	if g := prog[rel.ID]; g.TotalItems != 2 || g.DoneItems != 1 {
		t.Fatalf("progress = %d/%d items, want 1/2", g.DoneItems, g.TotalItems)
	}

	// Clearing the release unfiles the item.
	if err := svc.SetItemRelease(ctx, a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if items, _ = svc.ReleaseItems(ctx, rel.ID); len(items) != 1 {
		t.Fatalf("after clear, release items = %d, want 1", len(items))
	}
}

func TestSetItemReleaseReplacesSingle(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()

	r1, _ := svc.CreateRelease(ctx, wsID, "v0.27.0", "", "", nil, "")
	r2, _ := svc.CreateRelease(ctx, wsID, "v0.28.0", "", "", nil, "")
	it, _ := svc.CreateItem(ctx, wsID, statuses[0].ID, "x")

	if err := svc.SetItemRelease(ctx, it.ID, r1.ID); err != nil {
		t.Fatal(err)
	}
	// Setting a second release replaces the first — one release per item in the UI.
	if err := svc.SetItemRelease(ctx, it.ID, r2.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ReleasesForItem(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != r2.ID {
		t.Fatalf("releases for item = %+v, want only %s", got, r2.ID)
	}
	if items, _ := svc.ReleaseItems(ctx, r1.ID); len(items) != 0 {
		t.Fatalf("old release still holds the item (%d)", len(items))
	}
}

func TestSetItemReleaseRejectsForeignWorkspace(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	svc := board.New(ms)
	ws1, _ := ms.CreateWorkspace(ctx, store.Workspace{Slug: "a", Name: "A"})
	ws2, _ := ms.CreateWorkspace(ctx, store.Workspace{Slug: "b", Name: "B"})
	if err := svc.SeedDefaults(ctx, ws1.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SeedDefaults(ctx, ws2.ID); err != nil {
		t.Fatal(err)
	}
	rel, _ := svc.CreateRelease(ctx, ws2.ID, "v1", "", "", nil, "")
	st1, _ := svc.Statuses(ctx, ws1.ID)
	it, _ := svc.CreateItem(ctx, ws1.ID, st1[0].ID, "x")

	if err := svc.SetItemRelease(ctx, it.ID, rel.ID); !errors.Is(err, board.ErrReleaseMismatch) {
		t.Fatalf("cross-workspace release: want ErrReleaseMismatch, got %v", err)
	}
}

func TestReleaseShipReopenAndDelete(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()

	rel, _ := svc.CreateRelease(ctx, wsID, "v0.27.0", "", "", nil, "")
	it, _ := svc.CreateItem(ctx, wsID, statuses[0].ID, "x")
	if err := svc.SetItemRelease(ctx, it.ID, rel.ID); err != nil {
		t.Fatal(err)
	}

	if err := svc.SetReleaseStatus(ctx, rel.ID, "shipped"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Release(ctx, rel.ID)
	if got.Status != "shipped" || got.ShippedAt == nil {
		t.Fatalf("after ship: status=%q shippedAt=%v, want shipped + stamp", got.Status, got.ShippedAt)
	}

	if err := svc.SetReleaseStatus(ctx, rel.ID, "active"); err != nil { // reopen
		t.Fatal(err)
	}
	got, _ = svc.Release(ctx, rel.ID)
	if got.Status != "active" || got.ShippedAt != nil {
		t.Fatalf("after reopen: status=%q shippedAt=%v, want active + no stamp", got.Status, got.ShippedAt)
	}

	// Deleting a release drops its membership but leaves the item.
	if err := svc.DeleteRelease(ctx, rel.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Release(ctx, rel.ID); !errors.Is(err, store.ErrReleaseNotFound) {
		t.Fatalf("deleted release still resolves: %v", err)
	}
	if r, _ := svc.ReleasesForItem(ctx, it.ID); len(r) != 0 {
		t.Fatalf("item still tagged with a deleted release (%d)", len(r))
	}
	if _, err := svc.Item(ctx, it.ID); err != nil {
		t.Fatalf("item should survive release deletion: %v", err)
	}
}

func TestReleasePlannedLifecycle(t *testing.T) {
	svc, wsID, _ := setup(t)
	ctx := context.Background()

	// Create directly as Planned.
	rel, err := svc.CreateRelease(ctx, wsID, "v1.0", "future", "planned", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Status != "planned" {
		t.Fatalf("status = %q, want planned", rel.Status)
	}

	// Activate it, then it can ship.
	if err := svc.SetReleaseStatus(ctx, rel.ID, "active"); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Release(ctx, rel.ID); got.Status != "active" {
		t.Fatalf("after activate: %q, want active", got.Status)
	}

	// A bogus status is rejected.
	if err := svc.SetReleaseStatus(ctx, rel.ID, "nonsense"); !errors.Is(err, board.ErrInvalidReleaseStatus) {
		t.Fatalf("bad status: want ErrInvalidReleaseStatus, got %v", err)
	}
	// Can't create as shipped.
	if _, err := svc.CreateRelease(ctx, wsID, "v2.0", "", "shipped", nil, ""); !errors.Is(err, board.ErrInvalidReleaseStatus) {
		t.Fatalf("create shipped: want ErrInvalidReleaseStatus, got %v", err)
	}
}

func TestConvertMilestoneToRelease(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()

	ms, _ := svc.CreateItem(ctx, wsID, statuses[0].ID, "v1.0 milestone")
	if err := svc.SetMilestone(ctx, ms.ID, true); err != nil {
		t.Fatal(err)
	}
	c1, _ := svc.CreateSubtask(ctx, ms.ID, "task one")
	c2, _ := svc.CreateSubtask(ctx, ms.ID, "task two")

	rel, err := svc.ConvertMilestoneToRelease(ctx, ms.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Name != "v1.0 milestone" || rel.Status != "active" {
		t.Fatalf("release = {name:%q status:%q}, want name from milestone + active", rel.Name, rel.Status)
	}

	// The sub-tasks are now top-level items in the release.
	items, _ := svc.ReleaseItems(ctx, rel.ID)
	if len(items) != 2 {
		t.Fatalf("release items = %d, want 2", len(items))
	}
	for _, c := range []store.Item{c1, c2} {
		it, _ := svc.Item(ctx, c.ID)
		if it.ParentID != "" {
			t.Errorf("sub-task %s should be promoted to root, parent=%q", c.ID, it.ParentID)
		}
	}

	// The milestone is archived.
	if got, _ := svc.Item(ctx, ms.ID); got.ArchivedAt == nil {
		t.Error("milestone should be archived after convert")
	}

	// Converting a non-milestone is rejected.
	normal, _ := svc.CreateItem(ctx, wsID, statuses[0].ID, "normal")
	if _, err := svc.ConvertMilestoneToRelease(ctx, normal.ID, ""); !errors.Is(err, board.ErrNotMilestone) {
		t.Fatalf("convert non-milestone: want ErrNotMilestone, got %v", err)
	}
}

func TestUpdateRelease(t *testing.T) {
	svc, wsID, _ := setup(t)
	ctx := context.Background()

	rel, _ := svc.CreateRelease(ctx, wsID, "v0.27.0", "old notes", "", nil, "")
	if err := svc.UpdateRelease(ctx, rel.ID, "v0.27.1", "new notes", nil); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Release(ctx, rel.ID)
	if got.Name != "v0.27.1" || got.Description != "new notes" {
		t.Fatalf("update not applied: %+v", got)
	}
}
