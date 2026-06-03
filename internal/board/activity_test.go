package board_test

import (
	"context"
	"testing"
	"time"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

// fakeClock is a hand-advanced time source for exercising the activity log's
// coalescing window deterministically.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// latestVerb returns the newest activity event of verb on itemID, failing if none.
func latestVerb(t *testing.T, svc *board.Service, itemID, verb string) store.Event {
	t.Helper()
	hist, err := svc.ItemHistory(context.Background(), itemID, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range hist {
		if e.Verb == verb {
			return e
		}
	}
	t.Fatalf("no %s event on %s", verb, itemID)
	return store.Event{}
}

// actorCtx returns a context carrying a principal, the way the auth middleware
// does — so the board service can attribute the activity it records.
func actorCtx(id, display string) context.Context {
	return identity.NewContext(context.Background(), &identity.Principal{ID: id, Display: display})
}

// activitySetup builds a board over a fresh store with one user and the default
// lanes, returning the service, workspace id, statuses, and the user's id.
func activitySetup(t *testing.T) (*board.Service, string, []store.Status, string) {
	t.Helper()
	ms := memstore.New()
	ctx := context.Background()
	ws, err := ms.CreateWorkspace(ctx, store.Workspace{Slug: "general", Name: "General"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := ms.CreateUser(ctx, store.NewUser{Username: "ada", Display: "Ada"})
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
	return svc, ws.ID, statuses, u.ID
}

func TestActivityRecordsLifecycle(t *testing.T) {
	svc, wsID, statuses, uid := activitySetup(t)
	ctx := actorCtx(uid, "Ada")

	it, err := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Wire the log")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetStatus(ctx, it.ID, statuses[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetAssignee(ctx, it.ID, uid); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AddComment(ctx, it.ID, uid, "on it"); err != nil {
		t.Fatal(err)
	}

	hist, err := svc.ItemHistory(ctx, it.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	// Newest-first: comment, assign, status, create.
	wantVerbs := []string{
		store.EventCommentAdded,
		store.EventItemAssigned,
		store.EventItemStatusChange,
		store.EventItemCreated,
	}
	if len(hist) != len(wantVerbs) {
		t.Fatalf("want %d events, got %d: %+v", len(wantVerbs), len(hist), hist)
	}
	for i, want := range wantVerbs {
		if hist[i].Verb != want {
			t.Fatalf("event %d: want verb %q, got %q", i, want, hist[i].Verb)
		}
		if hist[i].ActorName != "Ada" {
			t.Fatalf("event %d: want actor Ada, got %q", i, hist[i].ActorName)
		}
		if hist[i].ItemID != it.ID {
			t.Fatalf("event %d: wrong item id %q", i, hist[i].ItemID)
		}
	}

	// The status change carries resolved from/to lane names.
	if sc := hist[2]; sc.Data["from"] != "To do" || sc.Data["to"] != "Doing" {
		t.Fatalf("status event data = %+v, want from=To do to=Doing", sc.Data)
	}
	// The assignment resolved the assignee's display name.
	if as := hist[1]; as.Data["to"] != "Ada" {
		t.Fatalf("assign event data = %+v, want to=Ada", as.Data)
	}

	// The workspace feed sees the same four entries.
	feed, err := svc.WorkspaceActivity(ctx, wsID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != len(wantVerbs) {
		t.Fatalf("workspace feed: want %d, got %d", len(wantVerbs), len(feed))
	}
}

// A no-op mutation (same value) records nothing.
func TestActivityNoopsAreSilent(t *testing.T) {
	svc, wsID, statuses, uid := activitySetup(t)
	ctx := actorCtx(uid, "Ada")

	it, err := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "Quiet")
	if err != nil {
		t.Fatal(err)
	}
	// Renaming to the same title and moving to the same lane are no-ops.
	if err := svc.RenameItem(ctx, it.ID, "Quiet"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetStatus(ctx, it.ID, statuses[0].ID); err != nil {
		t.Fatal(err)
	}
	hist, err := svc.ItemHistory(ctx, it.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].Verb != store.EventItemCreated {
		t.Fatalf("want only the create event, got %+v", hist)
	}
}

// A burst of description autosaves logs once, then stays quiet for the window —
// scoped per item per actor, so a different item or person still logs at once.
func TestActivityCoalescesDescriptionEdits(t *testing.T) {
	ms := memstore.New()
	bg := context.Background()
	ws, err := ms.CreateWorkspace(bg, store.Workspace{Slug: "general", Name: "General"})
	if err != nil {
		t.Fatal(err)
	}
	ada, err := ms.CreateUser(bg, store.NewUser{Username: "ada", Display: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := ms.CreateUser(bg, store.NewUser{Username: "bob", Display: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	clk := &fakeClock{t: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)}
	svc := board.New(ms, board.WithClock(clk.now))
	if err := svc.SeedDefaults(bg, ws.ID); err != nil {
		t.Fatal(err)
	}
	statuses, err := svc.Statuses(bg, ws.ID)
	if err != nil {
		t.Fatal(err)
	}

	asAda := actorCtx(ada.ID, "Ada")
	asBob := actorCtx(bob.ID, "Bob")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	describedOn := func(itemID string) int {
		hist, err := svc.ItemHistory(bg, itemID, 100)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range hist {
			if e.Verb == store.EventItemDescribed {
				n++
			}
		}
		return n
	}

	it, err := svc.CreateRootItem(asAda, ws.ID, statuses[0].ID, "Doc")
	if err != nil {
		t.Fatal(err)
	}

	// First edit logs; rapid follow-ups inside the window are suppressed.
	must(svc.UpdateDescription(asAda, it.ID, "v1"))
	clk.advance(2 * time.Second)
	must(svc.UpdateDescription(asAda, it.ID, "v2"))
	clk.advance(3 * time.Second)
	must(svc.UpdateDescription(asAda, it.ID, "v3"))
	if got := describedOn(it.ID); got != 1 {
		t.Fatalf("within window: want 1 described event, got %d", got)
	}
	// Trailing edge: the surviving entry carries the last edit's time, not the first.
	if got, want := latestVerb(t, svc, it.ID, store.EventItemDescribed).CreatedAt, clk.now(); !got.Equal(want) {
		t.Fatalf("coalesced entry time = %s, want the latest edit %s", got, want)
	}

	// Past the 5-minute window, the next edit logs a fresh entry.
	clk.advance(6 * time.Minute)
	must(svc.UpdateDescription(asAda, it.ID, "v4"))
	if got := describedOn(it.ID); got != 2 {
		t.Fatalf("after window: want 2 described events, got %d", got)
	}

	// Per item: a different item logs its own first edit even within the window.
	it2, err := svc.CreateRootItem(asAda, ws.ID, statuses[0].ID, "Doc2")
	if err != nil {
		t.Fatal(err)
	}
	must(svc.UpdateDescription(asAda, it2.ID, "other-item"))
	if got := describedOn(it2.ID); got != 1 {
		t.Fatalf("second item: want 1 described event, got %d", got)
	}

	// Per actor: a different person editing the same item within the window logs.
	must(svc.UpdateDescription(asBob, it.ID, "bob-edit"))
	if got := describedOn(it.ID); got != 3 {
		t.Fatalf("second actor: want 3 described events on the item, got %d", got)
	}
}

// A burst of title autosaves folds to one rename entry that reads from the
// burst's original title to the final one — not frozen mid-keystroke.
func TestActivityCoalescesRenames(t *testing.T) {
	ms := memstore.New()
	bg := context.Background()
	ws, err := ms.CreateWorkspace(bg, store.Workspace{Slug: "general", Name: "General"})
	if err != nil {
		t.Fatal(err)
	}
	ada, err := ms.CreateUser(bg, store.NewUser{Username: "ada", Display: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	clk := &fakeClock{t: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)}
	svc := board.New(ms, board.WithClock(clk.now))
	if err := svc.SeedDefaults(bg, ws.ID); err != nil {
		t.Fatal(err)
	}
	statuses, err := svc.Statuses(bg, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	asAda := actorCtx(ada.ID, "Ada")

	it, err := svc.CreateRootItem(asAda, ws.ID, statuses[0].ID, "Draft")
	if err != nil {
		t.Fatal(err)
	}
	for i, title := range []string{"Draft v", "Draft ve", "Draft version"} {
		clk.advance(time.Duration(i+1) * time.Second)
		if err := svc.RenameItem(asAda, it.ID, title); err != nil {
			t.Fatal(err)
		}
	}

	hist, err := svc.ItemHistory(bg, it.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	renames := 0
	for _, e := range hist {
		if e.Verb == store.EventItemRenamed {
			renames++
		}
	}
	if renames != 1 {
		t.Fatalf("want 1 coalesced rename event, got %d", renames)
	}
	r := latestVerb(t, svc, it.ID, store.EventItemRenamed)
	if r.Data["from"] != "Draft" || r.Data["to"] != "Draft version" {
		t.Fatalf("rename entry = %+v, want from=Draft to=\"Draft version\"", r.Data)
	}
}

// With no principal in context, activity is still recorded but attributed to the
// system rather than dropped.
func TestActivitySystemActor(t *testing.T) {
	svc, wsID, statuses, _ := activitySetup(t)
	ctx := context.Background() // no principal

	it, err := svc.CreateRootItem(ctx, wsID, statuses[0].ID, "no actor")
	if err != nil {
		t.Fatal(err)
	}
	hist, err := svc.ItemHistory(ctx, it.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].ActorID != "" {
		t.Fatalf("want one system-attributed event, got %+v", hist)
	}
}
