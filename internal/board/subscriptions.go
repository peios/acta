package board

import (
	"context"
	"log/slog"
	"slices"

	"github.com/peios/acta/internal/store"
)

// Subscriptions turn the activity log into delivered notifications. A principal
// subscribes to a subject — an item, a project, or another principal (their
// agents) — and picks which categories of event they want delivered. When an
// event fires, recordEvent fans it out (see notifySubscribers): it matches the
// event's item, the item's project, and the event's actor against the
// subscription table, and files an "activity" notification for each subscriber
// whose filter includes the event's category, the actor excluded and deduped to
// one per recipient.

// Event categories — the configurable unit of a subscription's filter. Each
// groups one or more raw verbs into a checkbox the subscriber can toggle. Stored
// as the comma-joined events column on a subscription.
const (
	CatComments    = "comments"    // a comment was added
	CatStatus      = "status"      // an item's status changed
	CatAssignments = "assignments" // an item was (re/un)assigned
	CatItemsAdded  = "items_added" // an item was created, or filed into a project
	CatOther       = "other"       // rename, description, archive, milestone, reparent
)

// AllCategories is the canonical category set and order, used to render the
// config UI and to normalise a stored or submitted filter.
var AllCategories = []string{CatComments, CatStatus, CatAssignments, CatItemsAdded, CatOther}

// CategoryLabel is the human label for a category key, for the config UI.
func CategoryLabel(cat string) string {
	switch cat {
	case CatComments:
		return "Comments"
	case CatStatus:
		return "Status changes"
	case CatAssignments:
		return "Assignments"
	case CatItemsAdded:
		return "Items added"
	case CatOther:
		return "Other edits"
	default:
		return cat
	}
}

// categoryForVerb maps a stored event verb to the subscription category it falls
// under, or "" for a verb no subscription can match.
func categoryForVerb(verb string) string {
	switch verb {
	case store.EventCommentAdded:
		return CatComments
	case store.EventItemStatusChange:
		return CatStatus
	case store.EventItemAssigned:
		return CatAssignments
	case store.EventItemCreated, store.EventItemProject:
		return CatItemsAdded
	case store.EventItemRenamed, store.EventItemDescribed, store.EventItemArchived,
		store.EventItemUnarchived, store.EventItemMilestone, store.EventItemReparented,
		// A forced move already emits a status-change event (which status watchers
		// get); this audit note rides "other" so they aren't notified twice.
		store.EventItemStatusForced:
		return CatOther
	default:
		return ""
	}
}

// DefaultEvents is the starting filter for a new subscription to subjectType —
// the conservative default each can be tuned up or down from. Item: comments +
// status. Project: items added + status. Principal: status only (it auto-applies
// to your agents, so it must stay calm; crank it to the firehose per agent).
func DefaultEvents(subjectType string) []string {
	switch subjectType {
	case store.SubjectItem:
		return []string{CatComments, CatStatus}
	case store.SubjectProject:
		return []string{CatItemsAdded, CatStatus}
	case store.SubjectPrincipal:
		return []string{CatStatus}
	default:
		return nil
	}
}

// cleanEvents normalises a submitted/stored filter: drops unknown keys, dedupes,
// and returns them in canonical order. An empty result is valid (muted).
func cleanEvents(events []string) []string {
	out := make([]string, 0, len(AllCategories))
	for _, c := range AllCategories {
		if slices.Contains(events, c) {
			out = append(out, c)
		}
	}
	return out
}

// --- subscription management ---

// Subscribe ensures subscriberID follows the subject with the subject-type
// default filter. Sticky and idempotent: if a subscription already exists its
// (possibly configured) filter is left untouched. This is the one path both the
// manual toggle and every auto-subscribe rule go through.
func (s *Service) Subscribe(ctx context.Context, subscriberID, subjectType, subjectID string) (store.Subscription, error) {
	return s.store.EnsureSubscription(ctx, store.Subscription{
		SubscriberID: subscriberID,
		SubjectType:  subjectType,
		SubjectID:    subjectID,
		Events:       DefaultEvents(subjectType),
	})
}

// SetSubscription upserts subscriberID's filter for the subject to exactly the
// given categories (sanitised). An empty set mutes the subscription without
// removing it; Unsubscribe removes it entirely.
func (s *Service) SetSubscription(ctx context.Context, subscriberID, subjectType, subjectID string, events []string) (store.Subscription, error) {
	return s.store.SetSubscriptionEvents(ctx, subscriberID, subjectType, subjectID, cleanEvents(events))
}

// Unsubscribe removes subscriberID's subscription to the subject (idempotent).
func (s *Service) Unsubscribe(ctx context.Context, subscriberID, subjectType, subjectID string) error {
	return s.store.DeleteSubscription(ctx, subscriberID, subjectType, subjectID)
}

// SubscriptionFor returns subscriberID's subscription to the subject and whether
// one exists — backs the toggle's on/off state.
func (s *Service) SubscriptionFor(ctx context.Context, subscriberID, subjectType, subjectID string) (store.Subscription, bool, error) {
	return s.store.SubscriptionFor(ctx, subscriberID, subjectType, subjectID)
}

// Subscriptions lists subscriberID's subscriptions, optionally filtered to a
// subject type ("" for all), newest-first.
func (s *Service) Subscriptions(ctx context.Context, subscriberID, subjectType string) ([]store.Subscription, error) {
	return s.store.SubscriptionsBySubscriber(ctx, subscriberID, subjectType)
}

// --- auto-subscribe (sticky, best-effort) ---

// autoSubscribe ensures a sticky subscription, swallowing errors — a missed
// auto-subscribe is observability noise, never a reason to fail the mutation
// that triggered it (the same stance as the activity log).
func (s *Service) autoSubscribe(ctx context.Context, subscriberID, subjectType, subjectID string) {
	if subscriberID == "" || subjectID == "" {
		return
	}
	if _, err := s.Subscribe(ctx, subscriberID, subjectType, subjectID); err != nil {
		slog.Error("auto-subscribe", "subscriber", subscriberID, "type", subjectType, "subject", subjectID, "err", err)
	}
}

// autoSubscribeAssignee subscribes a freshly-assigned principal to the item and,
// when the assignee is an agent, its human owner too — so you watch what your
// agents are put on. Sticky: ever-assigned means an interested party.
func (s *Service) autoSubscribeAssignee(ctx context.Context, assigneeID, itemID string) {
	if assigneeID == "" {
		return
	}
	s.autoSubscribe(ctx, assigneeID, store.SubjectItem, itemID)
	if u, err := s.store.UserByID(ctx, assigneeID); err == nil && u.AgentOfID != "" {
		s.autoSubscribe(ctx, u.AgentOfID, store.SubjectItem, itemID)
	}
}

// --- fanout ---

// notifySubscribers files an activity notification for every subscription that
// matches the event (by its item, the item's project, or its actor) and whose
// filter includes the event's category. The actor is never notified of their
// own action, and a recipient who matches on several axes at once gets a single
// notification. Recipients in exclude are skipped — used to let a more specific
// delivery (a comment's @mention) take priority over the generic activity one.
// Best-effort: a failed delivery is logged, never surfaced — a missed
// notification must not fail the mutation it describes. Called from recordEvent
// after the event is durably logged.
func (s *Service) notifySubscribers(ctx context.Context, ev store.Event, item store.Item, exclude map[string]bool) {
	cat := categoryForVerb(ev.Verb)
	if cat == "" {
		return
	}
	subs, err := s.store.SubscribersForEvent(ctx, ev.ItemID, item.ProjectID, ev.ActorID)
	if err != nil {
		slog.Error("subscription fanout lookup", "item", ev.ItemID, "err", err)
		return
	}
	if len(subs) == 0 {
		return
	}
	summary := HumanizeEvent(ev)
	seen := make(map[string]bool, len(subs))
	slug, slugDone := "", false
	for _, sub := range subs {
		if !slices.Contains(sub.Events, cat) {
			continue // this subscription's filter excludes the category
		}
		if sub.SubscriberID == ev.ActorID || seen[sub.SubscriberID] || exclude[sub.SubscriberID] {
			continue // never self-notify; one per recipient; yield to a mention
		}
		seen[sub.SubscriberID] = true
		if !slugDone {
			if ws, err := s.store.WorkspaceByID(ctx, item.WorkspaceID); err == nil {
				slug = ws.Slug
			}
			slugDone = true
		}
		n, err := s.store.CreateNotification(ctx, store.Notification{
			RecipientID:   sub.SubscriberID,
			Kind:          store.NotificationActivity,
			WorkspaceID:   item.WorkspaceID,
			WorkspaceSlug: slug,
			ItemID:        ev.ItemID,
			ItemTitle:     ev.ItemTitle,
			ActorID:       ev.ActorID,
			ActorName:     ev.ActorName,
			Verb:          ev.Verb,
			Summary:       summary,
		})
		if err != nil {
			slog.Error("create activity notification", "recipient", sub.SubscriberID, "item", ev.ItemID, "err", err)
			continue
		}
		s.notify(ctx, sub.SubscriberID, n) // best-effort push + live bell; must not block
	}
}

// recipientSet collects the recipient ids of a batch of notifications — the
// @mention recipients AddComment hands the comment fanout to exclude.
func recipientSet(notes []store.Notification) map[string]bool {
	if len(notes) == 0 {
		return nil
	}
	m := make(map[string]bool, len(notes))
	for _, n := range notes {
		m[n.RecipientID] = true
	}
	return m
}

// HumanizeEvent renders a stored event as the verb phrase shown to people ("moved
// from To do to Doing"). It reads the same denormalised Data the board wrote, so
// it never resolves ids and stays correct after referenced rows change. Lives
// here (the domain) so both the web activity feed and the subscription fanout —
// which snapshots the phrase onto each notification — share one source.
func HumanizeEvent(e store.Event) string {
	d := e.Data
	switch e.Verb {
	case store.EventItemCreated:
		if s := d["status"]; s != "" {
			return "created this in " + s
		}
		return "created this"
	case store.EventItemRenamed:
		return "renamed “" + d["from"] + "” → “" + d["to"] + "”"
	case store.EventItemStatusChange:
		if b := d["toBoard"]; b != "" {
			return "moved to the " + b + " board"
		}
		return "moved from " + d["from"] + " to " + d["to"]
	case store.EventItemStatusForced:
		if u := d["unmet"]; u != "" {
			return "forced into " + d["to"] + " past unmet: " + u
		}
		return "forced into " + d["to"]
	case store.EventItemAssigned:
		switch {
		case d["to"] == "":
			return "unassigned this"
		case d["from"] == "":
			return "assigned this to " + d["to"]
		default:
			return "reassigned this from " + d["from"] + " to " + d["to"]
		}
	case store.EventItemDescribed:
		return "updated the description"
	case store.EventItemArchived:
		return "archived this"
	case store.EventItemUnarchived:
		return "restored this"
	case store.EventItemMilestone:
		if d["on"] == "true" {
			return "marked this as a milestone"
		}
		return "removed the milestone mark"
	case store.EventItemReparented:
		if d["to"] == "" {
			return "moved this to the top level"
		}
		return "moved this under " + d["to"]
	case store.EventItemProject:
		if d["to"] == "" {
			return "removed this from its project"
		}
		return "filed this under " + d["to"]
	case store.EventCommentAdded:
		if x := d["excerpt"]; x != "" {
			return "commented: “" + x + "”"
		}
		return "added a comment"
	default:
		return e.Verb
	}
}
