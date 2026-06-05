package board_test

import (
	"context"
	"slices"
	"testing"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

// activityNotes returns a recipient's activity (subscription) notifications.
func activityNotes(t *testing.T, ms *memstore.Store, recipient string) []store.Notification {
	t.Helper()
	all, err := ms.NotificationsByRecipient(context.Background(), recipient, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Notification
	for _, n := range all {
		if n.Kind == store.NotificationActivity {
			out = append(out, n)
		}
	}
	return out
}

func subscribed(t *testing.T, svc *board.Service, sub, subjectType, subjectID string) bool {
	t.Helper()
	_, ok, err := svc.SubscriptionFor(context.Background(), sub, subjectType, subjectID)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// TestSubscriptionFanout covers the core: a subscriber is notified of a matching
// event by another actor (with a rendered summary), and the actor is not
// notified of their own action.
func TestSubscriptionFanout(t *testing.T) {
	ms, svc, wsID, statusID, ada, ben, _ := notifySetup(t)
	it, err := svc.CreateRootItemAs(actorCtx(ada, "Ada"), wsID, statusID, "Watch me", ada)
	if err != nil {
		t.Fatal(err)
	}
	if !subscribed(t, svc, ada, store.SubjectItem, it.ID) {
		t.Fatal("creator should be auto-subscribed to the item")
	}
	statuses, _ := svc.Statuses(context.Background(), wsID)

	// Ben moves it → Ada (item watcher, status in her default filter) is notified.
	if err := svc.SetStatus(actorCtx(ben, "Ben"), it.ID, statuses[1].ID); err != nil {
		t.Fatal(err)
	}
	an := activityNotes(t, ms, ada)
	if len(an) != 1 {
		t.Fatalf("ada: want 1 activity notification, got %d", len(an))
	}
	switch n := an[0]; {
	case n.Verb != store.EventItemStatusChange:
		t.Errorf("verb = %q, want status_changed", n.Verb)
	case n.Summary == "":
		t.Error("summary not snapshotted")
	case n.ActorName != "Ben":
		t.Errorf("actor = %q, want Ben", n.ActorName)
	case n.ItemID != it.ID:
		t.Errorf("item = %q, want %q", n.ItemID, it.ID)
	}
	// The actor never self-notifies.
	if bn := activityNotes(t, ms, ben); len(bn) != 0 {
		t.Errorf("ben (actor) should not be notified, got %d", len(bn))
	}
}

// TestSubscriptionDedup: one event matching several of a recipient's
// subscriptions yields a single notification.
func TestSubscriptionDedup(t *testing.T) {
	ms, svc, wsID, statusID, ada, ben, _ := notifySetup(t)
	it, err := svc.CreateRootItemAs(actorCtx(ada, "Ada"), wsID, statusID, "Dedup", ada)
	if err != nil {
		t.Fatal(err)
	}
	// Ada also follows Ben (the principal) — so Ben's move matches both her item
	// subscription and her principal subscription.
	if _, err := svc.Subscribe(context.Background(), ada, store.SubjectPrincipal, ben); err != nil {
		t.Fatal(err)
	}
	statuses, _ := svc.Statuses(context.Background(), wsID)
	if err := svc.SetStatus(actorCtx(ben, "Ben"), it.ID, statuses[1].ID); err != nil {
		t.Fatal(err)
	}
	if an := activityNotes(t, ms, ada); len(an) != 1 {
		t.Fatalf("want 1 notification after dedup, got %d", len(an))
	}
}

// TestSubscriptionCategoryFilter: an event outside the filter is not delivered.
func TestSubscriptionCategoryFilter(t *testing.T) {
	ms, svc, wsID, statusID, ada, ben, _ := notifySetup(t)
	it, err := svc.CreateRootItemAs(actorCtx(ada, "Ada"), wsID, statusID, "Filter", ada)
	if err != nil {
		t.Fatal(err)
	}
	// A rename is the "other" category — not in the item default (comments+status).
	if err := svc.RenameItem(actorCtx(ben, "Ben"), it.ID, "Filter renamed"); err != nil {
		t.Fatal(err)
	}
	if an := activityNotes(t, ms, ada); len(an) != 0 {
		t.Fatalf("rename is outside the default filter: want 0, got %d", len(an))
	}
	// Widen Ada's filter to include "other", then a fresh "other" event (a
	// milestone flag — a distinct, non-coalescing verb) — now it delivers.
	if _, err := svc.SetSubscription(context.Background(), ada, store.SubjectItem, it.ID, board.AllCategories); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetMilestone(actorCtx(ben, "Ben"), it.ID, true); err != nil {
		t.Fatal(err)
	}
	if an := activityNotes(t, ms, ada); len(an) != 1 {
		t.Fatalf("after widening filter: want 1, got %d", len(an))
	}
}

// TestAutoSubscribeAssignee: assigning an agent subscribes both the agent and its
// human owner to the item.
func TestAutoSubscribeAssignee(t *testing.T) {
	_, svc, wsID, statusID, ada, ben, agent := notifySetup(t)
	it, err := svc.CreateRootItemAs(actorCtx(ben, "Ben"), wsID, statusID, "Assign", ben)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetAssignee(actorCtx(ben, "Ben"), it.ID, agent); err != nil {
		t.Fatal(err)
	}
	if !subscribed(t, svc, agent, store.SubjectItem, it.ID) {
		t.Error("assigned agent should be auto-subscribed to the item")
	}
	if !subscribed(t, svc, ada, store.SubjectItem, it.ID) {
		t.Error("the agent's owner should be auto-subscribed to the item")
	}
}

// TestPrincipalFollow: following a principal notifies you of what they do
// (matched on the actor), self-excluded as ever.
func TestPrincipalFollow(t *testing.T) {
	ms, svc, wsID, statusID, ada, _, agent := notifySetup(t)
	// Ada follows her agent (the auto-subscribe at agent creation is wired in the
	// web layer; here we subscribe explicitly).
	if _, err := svc.Subscribe(context.Background(), ada, store.SubjectPrincipal, agent); err != nil {
		t.Fatal(err)
	}
	// The agent creates an item, then moves it.
	it, err := svc.CreateRootItemAs(actorCtx(agent, "Ada's Bot"), wsID, statusID, "Agent work", agent)
	if err != nil {
		t.Fatal(err)
	}
	statuses, _ := svc.Statuses(context.Background(), wsID)
	if err := svc.SetStatus(actorCtx(agent, "Ada's Bot"), it.ID, statuses[1].ID); err != nil {
		t.Fatal(err)
	}
	// Principal default = status only, so Ada hears the status change but not the
	// creation.
	an := activityNotes(t, ms, ada)
	if len(an) != 1 || an[0].Verb != store.EventItemStatusChange {
		t.Fatalf("want 1 status-change notification from following the agent, got %d: %+v", len(an), an)
	}
}

// TestProjectFollow: following a project notifies you of activity on its items.
func TestProjectFollow(t *testing.T) {
	ms, svc, wsID, statusID, ada, ben, _ := notifySetup(t)
	pr, err := svc.CreateProject(actorCtx(ada, "Ada"), wsID, "Peinit", "", "", "active", "", ada)
	if err != nil {
		t.Fatal(err)
	}
	if !subscribed(t, svc, ada, store.SubjectProject, pr.ID) {
		t.Fatal("project creator should be auto-subscribed to the project")
	}
	it, err := svc.CreateRootItemAs(actorCtx(ben, "Ben"), wsID, statusID, "Project task", ben)
	if err != nil {
		t.Fatal(err)
	}
	// Ada files it into her project (she's the actor, so excluded from this event).
	if err := svc.SetItemProject(actorCtx(ada, "Ada"), it.ID, pr.ID); err != nil {
		t.Fatal(err)
	}
	// Ben moves it → Ada (project watcher, status in the project default) hears it.
	statuses, _ := svc.Statuses(context.Background(), wsID)
	if err := svc.SetStatus(actorCtx(ben, "Ben"), it.ID, statuses[1].ID); err != nil {
		t.Fatal(err)
	}
	an := activityNotes(t, ms, ada)
	if len(an) != 1 || an[0].Verb != store.EventItemStatusChange {
		t.Fatalf("want 1 status notification from following the project, got %d: %+v", len(an), an)
	}
}

// TestMentionTakesPriorityOverActivity: a watcher who is @mentioned in a comment
// gets just the mention, not also a comment-activity notification — but an
// unmentioned watcher still gets the activity.
func TestMentionTakesPriorityOverActivity(t *testing.T) {
	ms, svc, wsID, statusID, ada, ben, agent := notifySetup(t)
	it, err := svc.CreateRootItemAs(actorCtx(ben, "Ben"), wsID, statusID, "Thread", ben)
	if err != nil {
		t.Fatal(err)
	}
	// The agent also watches the item, but won't be mentioned.
	if _, err := svc.Subscribe(context.Background(), agent, store.SubjectItem, it.ID); err != nil {
		t.Fatal(err)
	}

	// Ada comments, @mentioning only Ben (who also watches the item).
	if _, _, err := svc.AddComment(actorCtx(ada, "Ada"), it.ID, ada, "@ben take a look"); err != nil {
		t.Fatal(err)
	}

	// Ben (mentioned watcher) gets exactly one notification — the mention wins.
	bn, _ := ms.NotificationsByRecipient(context.Background(), ben, 50)
	if len(bn) != 1 || bn[0].Kind != store.NotificationMention {
		t.Fatalf("ben: want exactly 1 mention, got %d: %+v", len(bn), bn)
	}
	// The agent (unmentioned watcher) still gets the comment activity.
	an := activityNotes(t, ms, agent)
	if len(an) != 1 || an[0].Verb != store.EventCommentAdded {
		t.Fatalf("agent: want 1 comment-activity notification, got %d: %+v", len(an), an)
	}
}

// TestDefaultAndCleanEvents checks the category defaults and sanitisation.
func TestDefaultAndCleanEvents(t *testing.T) {
	if got := board.DefaultEvents(store.SubjectPrincipal); !slices.Equal(got, []string{board.CatStatus}) {
		t.Errorf("principal default = %v, want [status]", got)
	}
	if got := board.DefaultEvents(store.SubjectItem); !slices.Equal(got, []string{board.CatComments, board.CatStatus}) {
		t.Errorf("item default = %v", got)
	}
	// SetSubscription sanitises: unknown keys dropped, canonical order restored.
	_, svc, wsID, statusID, ada, _, _ := notifySetup(t)
	it, err := svc.CreateRootItemAs(actorCtx(ada, "Ada"), wsID, statusID, "Cats", ada)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := svc.SetSubscription(context.Background(), ada, store.SubjectItem, it.ID,
		[]string{"bogus", board.CatStatus, board.CatComments})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sub.Events, []string{board.CatComments, board.CatStatus}) {
		t.Errorf("events = %v, want canonical [comments status] with bogus dropped", sub.Events)
	}
}
