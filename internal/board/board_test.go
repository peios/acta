package board_test

import (
	"context"
	"errors"
	"testing"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

// setup makes a workspace with the default lanes seeded and returns the board
// service plus the three status ids (To do, Doing, Done in order).
func setup(t *testing.T) (*board.Service, string, []store.Status) {
	t.Helper()
	ms := memstore.New()
	ctx := context.Background()
	ws, err := ms.CreateWorkspace(ctx, store.Workspace{Slug: "general", Name: "General"})
	if err != nil {
		t.Fatal(err)
	}
	svc := board.New(ms)
	if err := svc.SeedDefaults(ctx, ws.ID); err != nil {
		t.Fatal(err)
	}
	statuses, err := svc.Statuses(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != len(board.DefaultStatuses) {
		t.Fatalf("want %d seeded statuses, got %d", len(board.DefaultStatuses), len(statuses))
	}
	return svc, ws.ID, statuses
}

// laneTitles returns the titles in a lane, in position order.
func laneTitles(t *testing.T, svc *board.Service, wsID, statusID string) []string {
	t.Helper()
	items, err := svc.Items(context.Background(), wsID)
	if err != nil {
		t.Fatal(err)
	}
	// Items() returns the whole workspace; filter + rely on position order
	// being preserved per status by the store's ORDER BY position.
	var out []string
	for _, it := range items {
		if it.StatusID == statusID {
			out = append(out, it.Title)
		}
	}
	return out
}

func TestSeedDefaults(t *testing.T) {
	_, _, statuses := setup(t)
	for i, want := range board.DefaultStatuses {
		if statuses[i].Name != want || statuses[i].Position != i {
			t.Fatalf("status %d: want %q@%d, got %q@%d", i, want, i, statuses[i].Name, statuses[i].Position)
		}
	}
}

func TestCreateItemAppendsToLane(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	todo := st[0].ID

	a, _ := svc.CreateItem(ctx, wsID, todo, "A")
	b, _ := svc.CreateItem(ctx, wsID, todo, "B")
	if a.Position != 0 || b.Position != 1 {
		t.Fatalf("append positions: want 0,1 got %d,%d", a.Position, b.Position)
	}
	if got := laneTitles(t, svc, wsID, todo); len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("lane order: want [A B], got %v", got)
	}
}

func TestMoveItemWithinLane(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	todo := st[0].ID

	svc.CreateItem(ctx, wsID, todo, "A")
	svc.CreateItem(ctx, wsID, todo, "B")
	c, _ := svc.CreateItem(ctx, wsID, todo, "C")

	if err := svc.MoveItem(ctx, c.ID, todo, 0); err != nil {
		t.Fatal(err)
	}
	if got := laneTitles(t, svc, wsID, todo); len(got) != 3 || got[0] != "C" || got[1] != "A" || got[2] != "B" {
		t.Fatalf("after move-to-front: want [C A B], got %v", got)
	}
}

func TestMoveItemAcrossLanes(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	todo, doing := st[0].ID, st[1].ID

	a, _ := svc.CreateItem(ctx, wsID, todo, "A")
	svc.CreateItem(ctx, wsID, todo, "B")

	if err := svc.MoveItem(ctx, a.ID, doing, 0); err != nil {
		t.Fatal(err)
	}
	// Source lane re-densified to just [B] at position 0.
	src := laneTitles(t, svc, wsID, todo)
	if len(src) != 1 || src[0] != "B" {
		t.Fatalf("source lane: want [B], got %v", src)
	}
	// Destination lane now holds [A].
	dst := laneTitles(t, svc, wsID, doing)
	if len(dst) != 1 || dst[0] != "A" {
		t.Fatalf("dest lane: want [A], got %v", dst)
	}
	// And B's position is dense (0), not a stale 1.
	for _, it := range mustItems(t, svc, wsID) {
		if it.Title == "B" && it.Position != 0 {
			t.Fatalf("B should be re-densified to position 0, got %d", it.Position)
		}
	}
}

func TestDeleteStatusBlockedWhenNonEmpty(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	todo := st[0].ID

	svc.CreateItem(ctx, wsID, todo, "A")
	if err := svc.DeleteStatus(ctx, todo); !errors.Is(err, board.ErrStatusNotEmpty) {
		t.Fatalf("delete non-empty lane: want ErrStatusNotEmpty, got %v", err)
	}
	// Empty it, then deletion succeeds.
	items := mustItems(t, svc, wsID)
	if err := svc.DeleteItem(ctx, items[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteStatus(ctx, todo); err != nil {
		t.Fatalf("delete emptied lane: %v", err)
	}
}

func TestCreateItemRejectsForeignStatus(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()

	// A status id from a *different* workspace must be rejected.
	ms2 := memstore.New()
	other, _ := ms2.CreateWorkspace(ctx, store.Workspace{Slug: "other", Name: "Other"})
	otherSvc := board.New(ms2)
	otherSvc.SeedDefaults(ctx, other.ID)
	otherStatuses, _ := otherSvc.Statuses(ctx, other.ID)

	_ = st
	if _, err := svc.CreateItem(ctx, wsID, otherStatuses[0].ID, "X"); !errors.Is(err, store.ErrStatusNotFound) {
		t.Fatalf("foreign status (different store): want ErrStatusNotFound, got %v", err)
	}
}

func TestInvalidInput(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()

	if _, err := svc.CreateStatus(ctx, wsID, "   "); !errors.Is(err, board.ErrInvalidName) {
		t.Fatalf("blank status name: want ErrInvalidName, got %v", err)
	}
	if _, err := svc.CreateItem(ctx, wsID, st[0].ID, ""); !errors.Is(err, board.ErrInvalidTitle) {
		t.Fatalf("blank item title: want ErrInvalidTitle, got %v", err)
	}
}

func TestSubtaskBasics(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	parent, _ := svc.CreateItem(ctx, wsID, st[0].ID, "Parent")
	sub, err := svc.CreateSubtask(ctx, parent.ID, "Child")
	if err != nil {
		t.Fatal(err)
	}
	if sub.ParentID != parent.ID {
		t.Fatalf("subtask parent not set: %q", sub.ParentID)
	}
	if sub.StatusID != st[0].ID {
		t.Fatalf("subtask should start in the first status")
	}
	// A subtask never shows on the board.
	for _, it := range mustItems(t, svc, wsID) {
		if it.ID == sub.ID {
			t.Fatal("subtask leaked onto the board")
		}
	}
	kids, _ := svc.Children(ctx, parent.ID)
	if len(kids) != 1 || kids[0].ID != sub.ID {
		t.Fatalf("child missing from parent")
	}
}

func TestArbitraryDepth(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	p, _ := svc.CreateItem(ctx, wsID, st[0].ID, "P")
	c, _ := svc.CreateSubtask(ctx, p.ID, "C")
	g, err := svc.CreateSubtask(ctx, c.ID, "G")
	if err != nil {
		t.Fatalf("a subtask should be able to have its own subtask: %v", err)
	}
	kids, _ := svc.Children(ctx, c.ID)
	if len(kids) != 1 || kids[0].ID != g.ID {
		t.Fatal("grandchild not nested under child")
	}
}

func TestSubtaskCounts(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	last := st[len(st)-1].ID
	p, _ := svc.CreateItem(ctx, wsID, st[0].ID, "P")
	svc.CreateSubtask(ctx, p.ID, "a")
	svc.CreateSubtask(ctx, p.ID, "b")
	c, _ := svc.CreateSubtask(ctx, p.ID, "c")
	if err := svc.SetStatus(ctx, c.ID, last); err != nil {
		t.Fatal(err)
	}
	counts, _ := svc.SubtaskCounts(ctx, wsID, last)
	if counts[p.ID].Total != 3 || counts[p.ID].Done != 1 {
		t.Fatalf("counts: want 1/3, got %d/%d", counts[p.ID].Done, counts[p.ID].Total)
	}
}

func TestArchiveCascadesToSubtree(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	p, _ := svc.CreateItem(ctx, wsID, st[0].ID, "P")
	svc.CreateSubtask(ctx, p.ID, "c1")
	svc.CreateSubtask(ctx, p.ID, "c2")

	if err := svc.Archive(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if kids, _ := svc.Children(ctx, p.ID); len(kids) != 0 {
		t.Fatalf("children not archived with parent: %d remain active", len(kids))
	}
	arch, _ := svc.ArchivedItems(ctx, wsID)
	if len(arch) != 1 || arch[0].ID != p.ID {
		t.Fatalf("archive should show only the subtree root, got %d items", len(arch))
	}
	if err := svc.Unarchive(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if kids, _ := svc.Children(ctx, p.ID); len(kids) != 2 {
		t.Fatalf("children not restored with parent: got %d", len(kids))
	}
}

func TestReparentPromote(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	p, _ := svc.CreateItem(ctx, wsID, st[0].ID, "Parent")
	sub, _ := svc.CreateSubtask(ctx, p.ID, "Sub")

	if err := svc.Reparent(ctx, sub.ID, ""); err != nil {
		t.Fatal(err)
	}
	onBoard := false
	for _, it := range mustItems(t, svc, wsID) {
		if it.ID == sub.ID {
			onBoard = true
		}
	}
	if !onBoard {
		t.Fatal("promoted item is not on the board")
	}
	if kids, _ := svc.Children(ctx, p.ID); len(kids) != 0 {
		t.Fatalf("promoted item still a child: %d remain", len(kids))
	}
}

func TestReparentDemote(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	a, _ := svc.CreateItem(ctx, wsID, st[0].ID, "A")
	b, _ := svc.CreateItem(ctx, wsID, st[0].ID, "B")

	if err := svc.Reparent(ctx, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	for _, it := range mustItems(t, svc, wsID) {
		if it.ID == a.ID {
			t.Fatal("demoted item still on the board")
		}
	}
	if kids, _ := svc.Children(ctx, b.ID); len(kids) != 1 || kids[0].ID != a.ID {
		t.Fatal("A is not a child of B after demote")
	}
}

func TestReparentCycleRejected(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	a, _ := svc.CreateItem(ctx, wsID, st[0].ID, "A")
	b, _ := svc.CreateSubtask(ctx, a.ID, "B") // B under A

	if err := svc.Reparent(ctx, a.ID, b.ID); !errors.Is(err, board.ErrCycle) {
		t.Fatalf("A under its descendant B: want ErrCycle, got %v", err)
	}
	if err := svc.Reparent(ctx, a.ID, a.ID); !errors.Is(err, board.ErrCycle) {
		t.Fatalf("A under itself: want ErrCycle, got %v", err)
	}
}

func TestCandidateParentsExcludeSubtree(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	a, _ := svc.CreateItem(ctx, wsID, st[0].ID, "A")
	b, _ := svc.CreateSubtask(ctx, a.ID, "B")
	svc.CreateSubtask(ctx, b.ID, "C") // A > B > C
	other, _ := svc.CreateItem(ctx, wsID, st[0].ID, "Other")

	cands, _ := svc.CandidateParents(ctx, wsID, a.ID)
	if len(cands) != 1 || cands[0].ID != other.ID {
		t.Fatalf("candidates for A should be just [Other], got %d", len(cands))
	}
}

func mustItems(t *testing.T, svc *board.Service, wsID string) []store.Item {
	t.Helper()
	items, err := svc.Items(context.Background(), wsID)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestArchiveHidesAndReDensifies(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	todo := st[0].ID

	svc.CreateItem(ctx, wsID, todo, "A")
	b, _ := svc.CreateItem(ctx, wsID, todo, "B")
	svc.CreateItem(ctx, wsID, todo, "C")

	if err := svc.Archive(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	// B is gone from the lane, and A/C are renumbered to 0,1 (no gap at 1).
	if got := laneTitles(t, svc, wsID, todo); len(got) != 2 || got[0] != "A" || got[1] != "C" {
		t.Fatalf("after archive: want [A C], got %v", got)
	}
	for _, it := range mustItems(t, svc, wsID) {
		if it.Position > 1 {
			t.Fatalf("lane not re-densified: %q at %d", it.Title, it.Position)
		}
	}
	arch, _ := svc.ArchivedItems(ctx, wsID)
	if len(arch) != 1 || arch[0].Title != "B" {
		t.Fatalf("archived list: want [B], got %v", arch)
	}

	// Unarchiving puts B back at the end of its lane.
	if err := svc.Unarchive(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if got := laneTitles(t, svc, wsID, todo); len(got) != 3 || got[2] != "B" {
		t.Fatalf("after unarchive: want B last in [A C B], got %v", got)
	}
}

func TestSetAssigneeValidatesUser(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	it, _ := svc.CreateItem(ctx, wsID, st[0].ID, "A")

	if err := svc.SetAssignee(ctx, it.ID, "nope"); !errors.Is(err, store.ErrUserNotFound) {
		t.Fatalf("unknown assignee: want ErrUserNotFound, got %v", err)
	}
	// Clearing (empty) is always allowed.
	if err := svc.SetAssignee(ctx, it.ID, ""); err != nil {
		t.Fatalf("clearing assignee: %v", err)
	}
}

func TestSetStatusMovesToLaneEnd(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	todo, doing := st[0].ID, st[1].ID
	svc.CreateItem(ctx, wsID, doing, "existing")
	a, _ := svc.CreateItem(ctx, wsID, todo, "A")

	if err := svc.SetStatus(ctx, a.ID, doing); err != nil {
		t.Fatal(err)
	}
	if got := laneTitles(t, svc, wsID, doing); len(got) != 2 || got[1] != "A" {
		t.Fatalf("set status: want A appended -> [existing A], got %v", got)
	}
	if got := laneTitles(t, svc, wsID, todo); len(got) != 0 {
		t.Fatalf("source lane should be empty, got %v", got)
	}
}

func TestComments(t *testing.T) {
	svc, wsID, st := setup(t)
	ctx := context.Background()
	it, _ := svc.CreateItem(ctx, wsID, st[0].ID, "A")

	if _, err := svc.AddComment(ctx, it.ID, "author1", "   "); !errors.Is(err, board.ErrInvalidComment) {
		t.Fatalf("blank comment: want ErrInvalidComment, got %v", err)
	}
	if _, err := svc.AddComment(ctx, it.ID, "author1", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddComment(ctx, it.ID, "author2", "second"); err != nil {
		t.Fatal(err)
	}
	cs, _ := svc.Comments(ctx, it.ID)
	if len(cs) != 2 || cs[0].Body != "first" || cs[1].Body != "second" {
		t.Fatalf("comments out of order: %v", cs)
	}
}
