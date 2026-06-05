package web

import (
	"context"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/live"
	"github.com/peios/acta/internal/store"
)

var _ board.Notifier = (*liveNotifier)(nil)

// liveNotifier is the in-page half of notification delivery: a board.Notifier
// that pushes a bell update over SSE to the recipient — the row to prepend plus
// their fresh unread count. Web Push (internal/push) is the away half; the two
// are attached side by side as the board's notifiers. Wiring it behind the board
// is what lets subscription notifications — filed deep in recordEvent — light
// the bell live, exactly as a mention does.
//
// Mentions are skipped here: the handler that files a mention already pushes its
// own bell row right after responding (it holds the notifications to hand), so
// echoing it again would double the row. This notifier therefore covers the
// activity (subscription) notifications, which have no such handler-side push.
type liveNotifier struct {
	hub   live.Broker
	board *board.Service
}

func newLiveNotifier(hub live.Broker, b *board.Service) *liveNotifier {
	return &liveNotifier{hub: hub, board: b}
}

func (ln *liveNotifier) NotifyUser(ctx context.Context, userID string, n store.Notification) {
	if ln.hub == nil || n.Kind == store.NotificationMention {
		return
	}
	rows := buildNotifViews([]store.Notification{n})
	if len(rows) == 0 {
		return
	}
	v := rows[0]
	count, err := ln.board.UnreadCount(ctx, userID)
	if err != nil {
		count = 0
	}
	publishLiveTo(ln.hub, userTopic(userID), "notif.add", "", map[string]any{
		"count":   count,
		"id":      v.ID,
		"url":     v.URL,
		"nkind":   v.Kind,
		"actor":   v.Actor,
		"title":   v.Title,
		"summary": v.Summary,
		"excerpt": v.Excerpt,
		"when":    v.When,
	})
}
