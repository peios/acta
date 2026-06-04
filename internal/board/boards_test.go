package board_test

import (
	"context"
	"errors"
	"testing"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

// TestSeedDefaultsCreatesBoards pins the seed: a fresh workspace gets a Tasks
// board (To do / Doing / Done) and a Backlog board (a single Backlog lane), in
// that order, each board's first lane flagged as its entry.
func TestSeedDefaultsCreatesBoards(t *testing.T) {
	svc, wsID, statuses := setup(t)
	ctx := context.Background()

	boards, err := svc.Boards(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 2 {
		t.Fatalf("want 2 boards, got %d", len(boards))
	}
	tasks, backlog := boards[0], boards[1]
	if tasks.Name != "Tasks" || tasks.Slug != "tasks" || tasks.Position != 0 {
		t.Fatalf("unexpected Tasks board: %+v", tasks)
	}
	if backlog.Name != "Backlog" || backlog.Slug != "backlog" || backlog.Position != 1 {
		t.Fatalf("unexpected Backlog board: %+v", backlog)
	}

	def, err := svc.DefaultBoard(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	if def.ID != tasks.ID {
		t.Fatalf("DefaultBoard = %s, want Tasks %s", def.ID, tasks.ID)
	}

	// setup() returns the Tasks lanes; every one belongs to Tasks, first is entry.
	for i, st := range statuses {
		if st.BoardID != tasks.ID {
			t.Errorf("status %q board = %q, want %q", st.Name, st.BoardID, tasks.ID)
		}
		if wantEntry := i == 0; st.IsEntry != wantEntry {
			t.Errorf("status %q IsEntry = %v, want %v", st.Name, st.IsEntry, wantEntry)
		}
	}

	taskEntry, err := svc.EntryStatus(ctx, tasks.ID)
	if err != nil {
		t.Fatal(err)
	}
	if taskEntry.Name != "To do" {
		t.Fatalf("Tasks entry lane = %q, want \"To do\"", taskEntry.Name)
	}
	backEntry, err := svc.EntryStatus(ctx, backlog.ID)
	if err != nil {
		t.Fatal(err)
	}
	if backEntry.Name != "Backlog" || !backEntry.IsEntry {
		t.Fatalf("Backlog entry lane = %+v, want \"Backlog\" entry", backEntry)
	}
}

// TestCreateStatusOnBoard confirms a new lane joins the board it targets, takes
// the next position on that board, and isn't accidentally crowned the entry lane.
func TestCreateStatusOnBoard(t *testing.T) {
	svc, wsID, _ := setup(t)
	ctx := context.Background()

	boards, err := svc.Boards(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	tasks, backlog := boards[0], boards[1]

	st, err := svc.CreateStatus(ctx, tasks.ID, "Review")
	if err != nil {
		t.Fatal(err)
	}
	if st.BoardID != tasks.ID {
		t.Errorf("new lane board = %q, want Tasks %q", st.BoardID, tasks.ID)
	}
	if st.IsEntry {
		t.Error("new lane should not be the entry lane")
	}
	if st.Position != len(board.DefaultStatuses) {
		t.Errorf("new Tasks lane position = %d, want %d", st.Position, len(board.DefaultStatuses))
	}

	// A lane added to Backlog joins Backlog at its next position, not Tasks.
	bk, err := svc.CreateStatus(ctx, backlog.ID, "Later")
	if err != nil {
		t.Fatal(err)
	}
	if bk.BoardID != backlog.ID {
		t.Errorf("new lane board = %q, want Backlog %q", bk.BoardID, backlog.ID)
	}
	if bk.Position != len(board.DefaultBacklogStatuses) {
		t.Errorf("new Backlog lane position = %d, want %d", bk.Position, len(board.DefaultBacklogStatuses))
	}
}

// TestEventCarriesBoardID checks that activity is attributed to the board its
// item lives on — derived from the item's status — so each board has its own
// feed.
func TestEventCarriesBoardID(t *testing.T) {
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
	def, err := svc.DefaultBoard(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := svc.EntryStatus(ctx, def.ID)
	if err != nil {
		t.Fatal(err)
	}
	it, err := svc.CreateItem(ctx, ws.ID, entry.ID, "First task")
	if err != nil {
		t.Fatal(err)
	}

	events, err := svc.ItemHistory(ctx, it.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("no events recorded for created item")
	}
	for _, e := range events {
		if e.BoardID != def.ID {
			t.Errorf("event %q board = %q, want %q", e.Verb, e.BoardID, def.ID)
		}
	}

	feed, err := ms.EventsByBoard(ctx, def.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) == 0 {
		t.Fatal("EventsByBoard returned nothing")
	}
}

// TestCrossBoardStatusMove confirms the core of the model: giving an item a
// status on another board moves it there (board derived from status), and the
// activity line records the destination board.
func TestCrossBoardStatusMove(t *testing.T) {
	svc, wsID, tasks := setup(t)
	ctx := context.Background()

	boards, err := svc.Boards(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	backlog := boards[1]
	backStatuses, err := svc.BoardStatuses(ctx, backlog.ID)
	if err != nil {
		t.Fatal(err)
	}
	backLane := backStatuses[0] // "Backlog"

	it, err := svc.CreateItem(ctx, wsID, tasks[0].ID, "Idea") // starts on Tasks "To do"
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetStatus(ctx, it.ID, backLane.ID); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Item(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StatusID != backLane.ID {
		t.Fatalf("status = %q, want Backlog lane %q", got.StatusID, backLane.ID)
	}
	st, err := svc.StatusByID(ctx, got.StatusID)
	if err != nil {
		t.Fatal(err)
	}
	if st.BoardID != backlog.ID {
		t.Errorf("item board = %q, want Backlog %q", st.BoardID, backlog.ID)
	}

	events, err := svc.ItemHistory(ctx, it.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Verb == store.EventItemStatusChange && e.Data["toBoard"] == "Backlog" {
			found = true
			// The event is attributed to the destination board's feed.
			if e.BoardID != backlog.ID {
				t.Errorf("cross-board event board = %q, want Backlog %q", e.BoardID, backlog.ID)
			}
		}
	}
	if !found {
		t.Error("no cross-board status event with toBoard=Backlog")
	}
}

// TestBoardLookups exercises the slug/id resolvers and the not-found sentinel.
func TestBoardLookups(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	ws, err := ms.CreateWorkspace(ctx, store.Workspace{Slug: "general", Name: "General"})
	if err != nil {
		t.Fatal(err)
	}
	if err := board.New(ms).SeedDefaults(ctx, ws.ID); err != nil {
		t.Fatal(err)
	}
	bySlug, err := ms.BoardBySlug(ctx, ws.ID, "tasks")
	if err != nil {
		t.Fatal(err)
	}
	byID, err := ms.BoardByID(ctx, bySlug.ID)
	if err != nil {
		t.Fatal(err)
	}
	if byID.Slug != "tasks" {
		t.Fatalf("BoardByID slug = %q, want \"tasks\"", byID.Slug)
	}
	if _, err := ms.BoardBySlug(ctx, ws.ID, "nope"); !errors.Is(err, store.ErrBoardNotFound) {
		t.Fatalf("want ErrBoardNotFound, got %v", err)
	}
}
