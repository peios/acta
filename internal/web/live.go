package web

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"time"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/live"
	"github.com/peios/acta/internal/store"
)

// --- topics ---
//
// A browser opens one SSE stream per page. It always subscribes to its own user
// topic (the notification bell, which follows you across pages) and, on a board
// page, to that workspace's topic (board cards + the open item modal).

func wsTopic(workspaceID string) string   { return "ws:" + workspaceID }
func userTopic(principalID string) string { return "user:" + principalID }

// clientID is the originating tab's id, sent on every mutating request as
// X-Acta-Client. It rides each published event as "origin" so the tab that made
// the change can ignore its own echo (it already applied the change locally).
// Empty for non-browser callers (MCP, the REST API), whose events therefore
// reach every browser.
func clientID(r *http.Request) string { return r.Header.Get("X-Acta-Client") }

// sseHeartbeat bounds how long the connection sits idle. A periodic comment
// frame keeps intermediaries from closing the stream and surfaces a dead client
// as a write error, which returns from the handler and tears the subscription
// down.
const sseHeartbeat = 25 * time.Second

// events is the live-update stream. It is GET /events, optionally
// ?workspace=<slug>; the body is text/event-stream and stays open until the
// client disconnects.
func (h *handlers) events(w http.ResponseWriter, r *http.Request) {
	me := principalFrom(r.Context())
	if me == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.live == nil {
		http.Error(w, "live updates unavailable", http.StatusServiceUnavailable)
		return
	}

	topics := []string{userTopic(me.ID)}
	if slug := r.URL.Query().Get("workspace"); slug != "" {
		if ws, err := h.workspaces.BySlug(r.Context(), slug); err == nil {
			topics = append(topics, wsTopic(ws.ID))
		}
	}

	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // tell nginx not to buffer the stream
	w.WriteHeader(http.StatusOK)

	ch := h.live.Subscribe(r.Context(), topics...)

	// An opening comment lets the client's onopen fire and confirms the headers
	// flushed; if this fails the connection is already gone.
	if _, err := w.Write([]byte(": connected\n\n")); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}

	tick := time.NewTicker(sseHeartbeat)
	defer tick.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case data := <-ch:
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !writeSSE(w, data) {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

// writeSSE frames one payload (a single-line JSON object) as an SSE data event.
// It reports success so the caller can return on a broken connection.
func writeSSE(w http.ResponseWriter, data []byte) bool {
	if _, err := w.Write([]byte("data: ")); err != nil {
		return false
	}
	if _, err := w.Write(data); err != nil {
		return false
	}
	_, err := w.Write([]byte("\n\n"))
	return err == nil
}

// --- publishing ---

// publishLive marshals one event envelope onto a topic. kind is the client-side
// event name; origin is the originating tab's client id; fields carries the
// kind-specific payload. A nil broker (live updates disabled) is a no-op.
func (h *handlers) publishLive(topic, kind, origin string, fields map[string]any) {
	publishLiveTo(h.live, topic, kind, origin, fields)
}

// publishLiveTo is publishLive against an explicit broker, for callers that
// aren't handler methods (the live bell notifier, wired behind the board).
func publishLiveTo(hub live.Broker, topic, kind, origin string, fields map[string]any) {
	if hub == nil {
		return
	}
	env := make(map[string]any, len(fields)+2)
	maps.Copy(env, fields)
	env["kind"] = kind
	env["origin"] = origin
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	hub.Publish(topic, data)
}

// cardFields builds the payload the client needs to create-or-update a board
// card in place: identity, lane (status + colour), title, milestone marker and
// the assignee avatar. parent_id is included so the client can tell a root card
// (placed on the board) from a subtask (which only lives in a modal).
func (h *handlers) cardFields(ctx context.Context, it store.Item) map[string]any {
	f := map[string]any{
		"id":        it.ID,
		"ref":       refID(h.prefixFor(ctx, it.WorkspaceID), it.RefNum),
		"status_id": it.StatusID,
		"title":     it.Title,
		"position":  it.Position,
		"parent_id": it.ParentID,
		"milestone": it.IsMilestone,
		"color":     "",
	}
	done := false
	if st, err := h.board.StatusByID(ctx, it.StatusID); err == nil {
		f["color"] = board.ColorFor(st)
		// "done" = the item's status is the last lane of its board, so a past-due
		// done item isn't flagged overdue.
		if lanes, err := h.board.BoardStatuses(ctx, st.BoardID); err == nil && len(lanes) > 0 {
			done = lanes[len(lanes)-1].ID == it.StatusID
		}
	}
	f["priority"] = attrField(board.Priorities, it.Priority)
	f["type"] = attrField(board.ItemTypes, it.Type)
	f["size"] = attrField(board.Sizes, it.Size)
	// Due chip. Absent key = no due date, so the client clears any existing chip.
	if it.DueDate != nil {
		f["due"] = map[string]any{
			"date":    board.DueString(it.DueDate),
			"label":   shortDueLabel(it.DueDate),
			"overdue": board.Overdue(it.DueDate, done),
		}
	}
	if it.ProjectID != "" {
		if p, err := h.board.Project(ctx, it.ProjectID); err == nil {
			f["project"] = map[string]any{
				"id":    p.ID,
				"name":  p.Name,
				"color": board.ProjectColorFor(p),
			}
		}
	}
	// Release chip (one per item in the UI). Absent key = no release, so the
	// client clears any existing chip on receipt.
	if rels, err := h.board.ReleasesForItem(ctx, it.ID); err == nil && len(rels) > 0 {
		rel := rels[0]
		f["release"] = map[string]any{
			"id":      rel.ID,
			"name":    rel.Name,
			"color":   board.ReleaseColorFor(rel),
			"shipped": rel.Status == "shipped",
		}
	}
	if it.AssigneeID != "" {
		if u, err := h.board.User(ctx, it.AssigneeID); err == nil {
			name := displayName(u)
			f["assignee"] = map[string]any{
				"id":       u.ID,
				"name":     name,
				"initials": initials(name),
				"agent":    u.AgentOfID != "",
			}
		}
	}
	return f
}

// attrField is the live-payload shape for an enum attribute: the value plus the
// slug and label the client needs to redraw the glyph without knowing the vocab.
func attrField(v board.AttrVocab, value int) map[string]any {
	o := v.Option(value)
	return map[string]any{"value": o.Value, "slug": o.Slug, "label": o.Label}
}

// publishItemUpsert pushes a card create/update to a workspace's viewers.
func (h *handlers) publishItemUpsert(ctx context.Context, origin, workspaceID string, it store.Item) {
	h.publishLive(wsTopic(workspaceID), "item.upsert", origin, h.cardFields(ctx, it))
}

// publishItemRemove pushes a card removal (archive or delete).
func (h *handlers) publishItemRemove(origin, workspaceID, itemID string) {
	h.publishLive(wsTopic(workspaceID), "item.remove", origin, map[string]any{"id": itemID})
}

// liveUpsert re-reads an item's post-mutation state and publishes its card to
// the workspace. Used by the field mutations (rename, move, status, assignee,
// milestone, reparent, unarchive) that answer 204 and so don't already hold the
// fresh item.
func (h *handlers) liveUpsert(r *http.Request, itemID string) {
	h.liveUpsertOrigin(r.Context(), clientID(r), itemID)
}

// liveUpsertOrigin is liveUpsert with an explicit origin, for callers without a
// request (the MCP tools, whose origin is always empty).
func (h *handlers) liveUpsertOrigin(ctx context.Context, origin, itemID string) {
	if it, err := h.board.Item(ctx, itemID); err == nil {
		h.publishItemUpsert(ctx, origin, it.WorkspaceID, it)
	}
}

// publishSubtaskAdd tells viewers with the parent's modal open to append a new
// subtask row.
func (h *handlers) publishSubtaskAdd(origin, workspaceID string, it store.Item) {
	h.publishLive(wsTopic(workspaceID), "subtask.add", origin, map[string]any{
		"parent": it.ParentID,
		"id":     it.ID,
		"title":  it.Title,
	})
}

// publishNotifications pushes a bell update to each freshly-notified recipient:
// the row to prepend plus their new unread count. origin is left empty so a
// self-mention still lights up the author's own bell.
func (h *handlers) publishNotifications(ctx context.Context, notes []store.Notification) {
	for _, n := range notes {
		row := buildNotifViews([]store.Notification{n})
		if len(row) == 0 {
			continue
		}
		v := row[0]
		count, err := h.board.UnreadCount(ctx, n.RecipientID)
		if err != nil {
			count = 0
		}
		h.publishLive(userTopic(n.RecipientID), "notif.add", "", map[string]any{
			"count":   count,
			"id":      v.ID,
			"url":     v.URL,
			"actor":   v.Actor,
			"title":   v.Title,
			"excerpt": v.Excerpt,
			"when":    v.When,
		})
	}
}
