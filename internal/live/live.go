// Package live is a tiny in-process publish/subscribe hub that fans server-side
// mutations out to connected browsers over Server-Sent Events.
//
// It is deliberately the simplest thing that works for a single-instance
// deployment: topics are opaque strings (a workspace id, a user id), each
// payload is one pre-serialized SSE data frame, and delivery is best-effort — a
// subscriber that can't keep up has events dropped rather than stalling the
// publisher (a mutation request must never block on a wedged socket). The
// Broker interface is the seam to swap in a cross-instance transport (e.g.
// Postgres LISTEN/NOTIFY) later without touching callers.
package live

import (
	"context"
	"sync"
)

// Broker fans published payloads out to the subscribers of a topic. Publish
// never blocks on a slow subscriber; Subscribe streams a topic's payloads on a
// channel until the context is cancelled.
type Broker interface {
	Publish(topic string, data []byte)
	Subscribe(ctx context.Context, topics ...string) <-chan []byte
}

// queueDepth is the per-subscriber buffer. SSE consumers drain promptly (the
// handler reads and writes to the socket in a tight loop), so a modest buffer
// only ever absorbs short bursts; if it fills, the subscriber's socket is
// almost certainly stalled and about to be reaped by its heartbeat.
const queueDepth = 64

// Hub is an in-process Broker. The zero value is not usable; call NewHub.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[*subscriber]struct{} // topic -> set of subscribers
}

type subscriber struct {
	ch chan []byte
}

// NewHub returns a ready in-process hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[*subscriber]struct{})}
}

// Subscribe registers interest in one or more topics and returns a channel of
// their payloads. The subscription is torn down — and the channel stops
// receiving — when ctx is cancelled. The channel is intentionally never closed:
// a cancelled reader leaves via ctx.Done(), and leaving it open means a Publish
// racing with teardown can't panic on a send to a closed channel.
func (h *Hub) Subscribe(ctx context.Context, topics ...string) <-chan []byte {
	s := &subscriber{ch: make(chan []byte, queueDepth)}
	h.mu.Lock()
	for _, t := range topics {
		m := h.subs[t]
		if m == nil {
			m = make(map[*subscriber]struct{})
			h.subs[t] = m
		}
		m[s] = struct{}{}
	}
	h.mu.Unlock()

	go func() {
		<-ctx.Done()
		h.mu.Lock()
		for _, t := range topics {
			if m := h.subs[t]; m != nil {
				delete(m, s)
				if len(m) == 0 {
					delete(h.subs, t)
				}
			}
		}
		h.mu.Unlock()
	}()
	return s.ch
}

// Publish delivers data to every current subscriber of topic. It never blocks:
// a subscriber whose buffer is full (a stalled socket) has the event dropped
// rather than holding up the publishing request.
func (h *Hub) Publish(topic string, data []byte) {
	h.mu.Lock()
	subs := make([]*subscriber, 0, len(h.subs[topic]))
	for s := range h.subs[topic] {
		subs = append(subs, s)
	}
	h.mu.Unlock()

	for _, s := range subs {
		select {
		case s.ch <- data:
		default: // subscriber is behind — drop rather than block.
		}
	}
}

// compile-time assertion that Hub satisfies Broker.
var _ Broker = (*Hub)(nil)
