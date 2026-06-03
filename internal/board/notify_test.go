package board_test

import (
	"context"
	"strings"
	"testing"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

// notifySetup builds a board over a fresh store with a workspace, the default
// lanes, a human author (Ada), a second human (Ben) and an agent of Ada's
// (ada/bot). It returns the store, service, workspace id, first status id, and
// the three principals' ids.
func notifySetup(t *testing.T) (ms *memstore.Store, svc *board.Service, wsID, statusID, ada, ben, agent string) {
	t.Helper()
	ms = memstore.New()
	ctx := context.Background()
	ws, err := ms.CreateWorkspace(ctx, store.Workspace{Slug: "general", Name: "General"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := ms.CreateUser(ctx, store.NewUser{Username: "ada", Display: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ms.CreateUser(ctx, store.NewUser{Username: "ben", Display: "Ben"})
	if err != nil {
		t.Fatal(err)
	}
	bot, err := ms.CreateUser(ctx, store.NewUser{Username: "ada/bot", Display: "Ada's Bot", AgentOfID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	svc = board.New(ms)
	if err := svc.SeedDefaults(ctx, ws.ID); err != nil {
		t.Fatal(err)
	}
	statuses, err := svc.Statuses(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	return ms, svc, ws.ID, statuses[0].ID, a.ID, b.ID, bot.ID
}

func TestMentionCreatesNotification(t *testing.T) {
	ms, svc, wsID, statusID, ada, ben, agent := notifySetup(t)
	ctx := actorCtx(ada, "Ada")

	it, err := svc.CreateRootItem(ctx, wsID, statusID, "Wire the bell")
	if err != nil {
		t.Fatal(err)
	}
	// Ada mentions Ben, the agent, herself, and a non-existent handle.
	if _, err := svc.AddComment(ctx, it.ID, ada, "hey @ben and @ada/bot — @ada @nobody"); err != nil {
		t.Fatal(err)
	}

	// Ben got exactly one mention, fully snapshotted.
	bn, err := ms.NotificationsByRecipient(ctx, ben, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(bn) != 1 {
		t.Fatalf("ben: want 1 notification, got %d: %+v", len(bn), bn)
	}
	n := bn[0]
	switch {
	case n.Kind != store.NotificationMention:
		t.Errorf("kind = %q, want %q", n.Kind, store.NotificationMention)
	case n.ActorName != "Ada":
		t.Errorf("actor = %q, want Ada", n.ActorName)
	case n.ItemID != it.ID || n.ItemTitle != "Wire the bell":
		t.Errorf("item snapshot = %q/%q", n.ItemID, n.ItemTitle)
	case n.WorkspaceSlug != "general":
		t.Errorf("slug = %q, want general", n.WorkspaceSlug)
	case n.CommentID == "":
		t.Error("comment id not captured")
	case !strings.Contains(n.Excerpt, "@ben"):
		t.Errorf("excerpt = %q, want it to carry the comment text", n.Excerpt)
	case n.ReadAt != nil:
		t.Error("fresh notification should be unread")
	}

	// The agent (mentioned by owner/name) also got one.
	an, err := ms.NotificationsByRecipient(ctx, agent, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(an) != 1 {
		t.Fatalf("agent: want 1 notification, got %d", len(an))
	}

	// Ada mentioned herself too — self-mentions are allowed (a way to bookmark
	// a thread), so she gets exactly one.
	if c, _ := ms.UnreadNotificationCount(ctx, ada); c != 1 {
		t.Errorf("ada self-mention: want 1 unread, got %d", c)
	}
}

func TestMentionMarkRead(t *testing.T) {
	ms, svc, wsID, statusID, ada, ben, _ := notifySetup(t)
	ctx := actorCtx(ada, "Ada")

	it, err := svc.CreateRootItem(ctx, wsID, statusID, "Read me")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddComment(ctx, it.ID, ada, "@ben look"); err != nil {
		t.Fatal(err)
	}
	bn, _ := ms.NotificationsByRecipient(ctx, ben, 50)
	if len(bn) != 1 {
		t.Fatalf("want 1 notification, got %d", len(bn))
	}
	id := bn[0].ID

	// A different recipient can't clear Ben's notification.
	if err := svc.MarkNotificationRead(ctx, id, ada); err != nil {
		t.Fatal(err)
	}
	if c, _ := svc.UnreadCount(ctx, ben); c != 1 {
		t.Fatalf("cross-recipient mark must not apply: want 1 unread, got %d", c)
	}

	// The owner can.
	if err := svc.MarkNotificationRead(ctx, id, ben); err != nil {
		t.Fatal(err)
	}
	if c, _ := svc.UnreadCount(ctx, ben); c != 0 {
		t.Fatalf("want 0 unread after mark, got %d", c)
	}
}
