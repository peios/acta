package live

import (
	"context"
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan []byte) []byte {
	t.Helper()
	select {
	case b := <-ch:
		return b
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a payload")
		return nil
	}
}

func assertQuiet(t *testing.T, ch <-chan []byte) {
	t.Helper()
	select {
	case b := <-ch:
		t.Fatalf("unexpected payload: %q", b)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestPublishReachesTopicSubscribers(t *testing.T) {
	h := NewHub()
	ctx := t.Context()

	a := h.Subscribe(ctx, "ws:1")
	b := h.Subscribe(ctx, "ws:1")
	other := h.Subscribe(ctx, "ws:2")

	h.Publish("ws:1", []byte("hello"))

	if got := string(recv(t, a)); got != "hello" {
		t.Fatalf("a got %q, want hello", got)
	}
	if got := string(recv(t, b)); got != "hello" {
		t.Fatalf("b got %q, want hello", got)
	}
	assertQuiet(t, other) // a different topic must not receive it
}

func TestSubscribeManyTopics(t *testing.T) {
	h := NewHub()
	ctx := t.Context()

	ch := h.Subscribe(ctx, "user:7", "ws:9")

	h.Publish("ws:9", []byte("board"))
	if got := string(recv(t, ch)); got != "board" {
		t.Fatalf("got %q, want board", got)
	}
	h.Publish("user:7", []byte("bell"))
	if got := string(recv(t, ch)); got != "bell" {
		t.Fatalf("got %q, want bell", got)
	}
}

func TestCancelUnsubscribes(t *testing.T) {
	h := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	ch := h.Subscribe(ctx, "ws:1")
	cancel()

	// Give the teardown goroutine a moment to remove the subscriber.
	deadline := time.Now().Add(time.Second)
	for {
		h.mu.Lock()
		n := len(h.subs["ws:1"])
		h.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subscriber was not removed after cancel")
		}
		time.Sleep(time.Millisecond)
	}

	// Publishing after teardown must not panic or deliver.
	h.Publish("ws:1", []byte("late"))
	assertQuiet(t, ch)
}

func TestPublishNoSubscribersIsNoop(t *testing.T) {
	h := NewHub()
	h.Publish("ws:nobody", []byte("x")) // must not panic
}

func TestSlowSubscriberDropsRatherThanBlock(t *testing.T) {
	h := NewHub()
	h.Subscribe(t.Context(), "ws:1") // never drained

	done := make(chan struct{})
	go func() {
		for range queueDepth * 4 {
			h.Publish("ws:1", []byte("x"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
}
