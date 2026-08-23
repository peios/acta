package board_test

import (
	"context"
	"errors"
	"testing"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

func TestProjectCreateDerivesUniqueSlug(t *testing.T) {
	svc, wsID, _ := setup(t)
	ctx := context.Background()

	p1, err := svc.CreateProject(ctx, wsID, "Peinit", "boot stuff", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if p1.Slug != "peinit" {
		t.Fatalf("slug = %q, want peinit", p1.Slug)
	}
	if p1.Status != "active" {
		t.Fatalf("default status = %q, want active", p1.Status)
	}
	// A second project with the same name gets a disambiguated slug.
	p2, err := svc.CreateProject(ctx, wsID, "Peinit", "", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if p2.Slug != "peinit-2" {
		t.Fatalf("collision slug = %q, want peinit-2", p2.Slug)
	}
}

func TestProjectValidation(t *testing.T) {
	svc, wsID, _ := setup(t)
	ctx := context.Background()

	if _, err := svc.CreateProject(ctx, wsID, "   ", "", "", "", "", ""); !errors.Is(err, board.ErrInvalidProjectName) {
		t.Fatalf("blank name: want ErrInvalidProjectName, got %v", err)
	}
	if _, err := svc.CreateProject(ctx, wsID, "X", "", "", "nonsense", "", ""); !errors.Is(err, board.ErrInvalidProjectStatus) {
		t.Fatalf("bad status: want ErrInvalidProjectStatus, got %v", err)
	}
	if _, err := svc.CreateProject(ctx, wsID, "X", "", "", "", "#zzzzzz", ""); !errors.Is(err, board.ErrInvalidColor) {
		t.Fatalf("bad colour: want ErrInvalidColor, got %v", err)
	}
}

func TestItemProjectAssignmentAndProgress(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()
	todo, done := statuses[0].ID, statuses[len(statuses)-1].ID

	pr, err := svc.CreateProject(ctx, wsID, "Peinit", "", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := svc.CreateItem(ctx, wsID, todo, "A")
	b, _ := svc.CreateItem(ctx, wsID, todo, "B")
	c, _ := svc.CreateItem(ctx, wsID, todo, "C") // left unfiled

	if err := svc.SetItemProject(ctx, a.ID, pr.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetItemProject(ctx, b.ID, pr.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetStatus(ctx, b.ID, done); err != nil { // B reaches the last lane
		t.Fatal(err)
	}

	items, err := svc.ProjectItems(ctx, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("project items = %d, want 2 (unfiled C excluded)", len(items))
	}

	prog, err := svc.ProjectProgress(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	if g := prog[pr.ID]; g.TotalItems != 2 || g.DoneItems != 1 {
		t.Fatalf("progress = %d/%d items, want 1/2", g.DoneItems, g.TotalItems)
	}
	_ = c

	// Clearing the project unfiles the item.
	if err := svc.SetItemProject(ctx, a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if items, _ = svc.ProjectItems(ctx, pr.ID); len(items) != 1 {
		t.Fatalf("after clear, project items = %d, want 1", len(items))
	}
}

func TestSubtaskInheritsParentProject(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()

	pr, _ := svc.CreateProject(ctx, wsID, "Peinit", "", "", "", "", "")
	parent, _ := svc.CreateItem(ctx, wsID, statuses[0].ID, "parent")
	if err := svc.SetItemProject(ctx, parent.ID, pr.ID); err != nil {
		t.Fatal(err)
	}
	sub, err := svc.CreateSubtask(ctx, parent.ID, "child")
	if err != nil {
		t.Fatal(err)
	}
	if sub.ProjectID != pr.ID {
		t.Fatalf("subtask project = %q, want inherited %q", sub.ProjectID, pr.ID)
	}
}

func TestSetItemProjectRejectsForeignWorkspace(t *testing.T) {
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
	pr, _ := svc.CreateProject(ctx, ws2.ID, "Other", "", "", "", "", "")
	st1, _ := svc.Statuses(ctx, ws1.ID)
	it, _ := svc.CreateItem(ctx, ws1.ID, st1[0].ID, "x")

	if err := svc.SetItemProject(ctx, it.ID, pr.ID); !errors.Is(err, board.ErrProjectMismatch) {
		t.Fatalf("cross-workspace project: want ErrProjectMismatch, got %v", err)
	}
}

func TestUpdateAndArchiveProject(t *testing.T) {
	svc, wsID, _ := setup(t)
	ctx := context.Background()

	pr, _ := svc.CreateProject(ctx, wsID, "Peinit", "", "", "", "", "")
	if err := svc.UpdateProject(ctx, pr.ID, "Peinit v2", "new brief", "", "paused", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Project(ctx, pr.ID)
	if got.Name != "Peinit v2" || got.Status != "paused" || got.Brief != "new brief" {
		t.Fatalf("update not applied: %+v", got)
	}
	if got.Slug != "peinit" {
		t.Fatalf("slug changed on rename: %q (should stay stable)", got.Slug)
	}

	if err := svc.ArchiveProject(ctx, pr.ID); err != nil {
		t.Fatal(err)
	}
	if active, _ := svc.Projects(ctx, wsID, false); len(active) != 0 {
		t.Fatalf("archived project still listed in active set (%d)", len(active))
	}
	if all, _ := svc.Projects(ctx, wsID, true); len(all) != 1 {
		t.Fatalf("archived project missing from include-archived set (%d)", len(all))
	}
}
