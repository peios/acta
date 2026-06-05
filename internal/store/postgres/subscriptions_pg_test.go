package postgres

import (
	"context"
	"slices"
	"testing"

	"github.com/peios/acta/internal/store"
)

// TestPGSubscriptions exercises the subscription SQL against real Postgres: the
// sticky ON CONFLICT no-op of EnsureSubscription, the events comma-join, the
// three-way fanout OR-query, and the notification verb/summary columns — none of
// which the memstore can validate.
func TestPGSubscriptions(t *testing.T) {
	pg := openTestDB(t)
	ctx := context.Background()

	ada, err := pg.CreateUser(ctx, store.NewUser{Username: "pgsub_ada", Display: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteUser(ctx, ada.ID) }) // cascades the subscriptions

	// EnsureSubscription inserts with the given filter.
	s1, err := pg.EnsureSubscription(ctx, store.Subscription{
		SubscriberID: ada.ID, SubjectType: store.SubjectItem, SubjectID: "item-x",
		Events: []string{"comments", "status"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s1.Events, []string{"comments", "status"}) {
		t.Fatalf("ensure events = %v", s1.Events)
	}

	// Ensure again with a different filter — sticky: the existing row (and its
	// filter) is left untouched, and the same id comes back.
	s2, err := pg.EnsureSubscription(ctx, store.Subscription{
		SubscriberID: ada.ID, SubjectType: store.SubjectItem, SubjectID: "item-x",
		Events: []string{"status"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s2.ID != s1.ID || !slices.Equal(s2.Events, []string{"comments", "status"}) {
		t.Fatalf("ensure must be sticky: id=%s events=%v", s2.ID, s2.Events)
	}

	// SetSubscriptionEvents replaces the filter.
	if _, err := pg.SetSubscriptionEvents(ctx, ada.ID, store.SubjectItem, "item-x", []string{"status"}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := pg.SubscriptionFor(ctx, ada.ID, store.SubjectItem, "item-x")
	if err != nil || !ok || !slices.Equal(got.Events, []string{"status"}) {
		t.Fatalf("after set: ok=%v events=%v err=%v", ok, got.Events, err)
	}

	// Add a project and a principal subscription to exercise the fanout OR-query.
	for _, s := range []store.Subscription{
		{SubscriberID: ada.ID, SubjectType: store.SubjectProject, SubjectID: "proj-1", Events: []string{"status"}},
		{SubscriberID: ada.ID, SubjectType: store.SubjectPrincipal, SubjectID: "actor-1", Events: []string{"status"}},
	} {
		if _, err := pg.EnsureSubscription(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	// An event on item-x, in proj-1, by actor-1 matches all three subscriptions.
	all, err := pg.SubscribersForEvent(ctx, "item-x", "proj-1", "actor-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("SubscribersForEvent matched %d, want 3", len(all))
	}
	// Empty project/actor ids match nothing extra — just the item subscription.
	only, err := pg.SubscribersForEvent(ctx, "item-x", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0].SubjectType != store.SubjectItem {
		t.Fatalf("item-only fanout = %+v, want just the item subscription", only)
	}

	// Delete is idempotent.
	if err := pg.DeleteSubscription(ctx, ada.ID, store.SubjectItem, "item-x"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := pg.SubscriptionFor(ctx, ada.ID, store.SubjectItem, "item-x"); ok {
		t.Fatal("subscription should be gone after delete")
	}
	if err := pg.DeleteSubscription(ctx, ada.ID, store.SubjectItem, "item-x"); err != nil {
		t.Fatalf("deleting a missing subscription must be a no-op: %v", err)
	}

	// The notification verb/summary columns round-trip for an activity row.
	n, err := pg.CreateNotification(ctx, store.Notification{
		RecipientID: ada.ID, Kind: store.NotificationActivity,
		Verb: store.EventItemStatusChange, Summary: "moved to Done",
	})
	if err != nil {
		t.Fatal(err)
	}
	if n.Verb != store.EventItemStatusChange || n.Summary != "moved to Done" {
		t.Fatalf("notification verb/summary not persisted: %+v", n)
	}
}
