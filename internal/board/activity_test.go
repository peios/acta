package board_test

import (
	"context"
	"testing"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

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
