package web_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// openEvents connects to the SSE stream and returns a channel of decoded event
// envelopes. The subscription is live by the time this returns (the handler
// subscribes before flushing headers), so a mutation fired next is not raced.
func openEvents(t *testing.T, client *http.Client, base, slug string) (<-chan map[string]any, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	url := base + "/events"
	if slug != "" {
		url += "?workspace=" + slug
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("GET /events: want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		cancel()
		t.Fatalf("GET /events: want text/event-stream, got %q", ct)
	}

	out := make(chan map[string]any, 32)
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			data, ok := strings.CutPrefix(line, "data: ")
			if !ok {
				continue // a heartbeat/comment line
			}
			var m map[string]any
			if json.Unmarshal([]byte(data), &m) != nil {
				continue
			}
			select {
			case out <- m:
			case <-ctx.Done():
				return
			}
		}
		_ = sc.Err() // stream ended (body closed on cancel); nothing to assert
	}()
	return out, cancel
}

// waitEvent drains the stream until an event of the given kind arrives, or the
// test's patience runs out.
func waitEvent(t *testing.T, ch <-chan map[string]any, kind string) map[string]any {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case m := <-ch:
			if m["kind"] == kind {
				return m
			}
		case <-timeout:
			t.Fatalf("timed out waiting for a %q event", kind)
			return nil
		}
	}
}

func TestLiveItemUpsertAndComment(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	events, cancel := openEvents(t, client, base, "general")
	defer cancel()

	todo := statusID(t, client, base, "To do")
	resp := postJSON(t, client, base+"/general/items", token, map[string]any{
		"status_id": todo, "title": "Live item",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create item: want 200, got %d", resp.StatusCode)
	}
	id := decodeID(t, resp)

	up := waitEvent(t, events, "item.upsert")
	if up["id"] != id {
		t.Fatalf("upsert id = %v, want %s", up["id"], id)
	}
	if up["title"] != "Live item" {
		t.Fatalf("upsert title = %v, want %q", up["title"], "Live item")
	}
	if up["status_id"] != todo {
		t.Fatalf("upsert status_id = %v, want %s", up["status_id"], todo)
	}

	cresp := postJSON(t, client, base+"/general/items/"+id+"/comment", token, map[string]any{"body": "hello there"})
	if cresp.StatusCode != http.StatusOK {
		t.Fatalf("comment: want 200, got %d", cresp.StatusCode)
	}
	cm := waitEvent(t, events, "comment.add")
	if cm["item"] != id {
		t.Fatalf("comment.add item = %v, want %s", cm["item"], id)
	}
	if cm["body"] != "hello there" {
		t.Fatalf("comment.add body = %v", cm["body"])
	}
}

func TestLiveItemRemoveOnArchive(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	events, cancel := openEvents(t, client, base, "general")
	defer cancel()

	todo := statusID(t, client, base, "To do")
	id := decodeID(t, postJSON(t, client, base+"/general/items", token, map[string]any{
		"status_id": todo, "title": "Doomed",
	}))
	waitEvent(t, events, "item.upsert") // the create

	if resp := postJSON(t, client, base+"/general/items/"+id+"/archive", token, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("archive: want 204, got %d", resp.StatusCode)
	}
	rm := waitEvent(t, events, "item.remove")
	if rm["id"] != id {
		t.Fatalf("item.remove id = %v, want %s", rm["id"], id)
	}
}

// TestLiveEchoOriginStamped proves the X-Acta-Client header rides the published
// event as "origin" (the hook the client uses to ignore its own echo).
func TestLiveEchoOriginStamped(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	events, cancel := openEvents(t, client, base, "general")
	defer cancel()

	todo := statusID(t, client, base, "To do")
	body, _ := json.Marshal(map[string]any{"status_id": todo, "title": "Tagged"})
	req, _ := http.NewRequest(http.MethodPost, base+"/general/items", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", token)
	req.Header.Set("X-Acta-Client", "tab-xyz")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create item: want 200, got %d", resp.StatusCode)
	}

	up := waitEvent(t, events, "item.upsert")
	if up["origin"] != "tab-xyz" {
		t.Fatalf("origin = %v, want tab-xyz", up["origin"])
	}
}

// TestLiveBellOnMention checks a mention pushes a notif.add to the recipient's
// user topic (which their stream is always subscribed to).
func TestLiveBellOnMention(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	events, cancel := openEvents(t, client, base, "general")
	defer cancel()

	todo := statusID(t, client, base, "To do")
	id := decodeID(t, postJSON(t, client, base+"/general/items", token, map[string]any{
		"status_id": todo, "title": "Mention me",
	}))
	waitEvent(t, events, "item.upsert")

	// jack mentions himself — a self-mention is a valid delivery.
	if resp := postJSON(t, client, base+"/general/items/"+id+"/comment", token, map[string]any{"body": "ping @jack"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("comment: want 200, got %d", resp.StatusCode)
	}
	n := waitEvent(t, events, "notif.add")
	if _, ok := n["count"].(float64); !ok {
		t.Fatalf("notif.add missing numeric count: %v", n["count"])
	}
	if n["title"] != "Mention me" {
		t.Fatalf("notif.add title = %v, want %q", n["title"], "Mention me")
	}
}
