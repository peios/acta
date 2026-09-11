package integration

import (
	"acta/internal/conversation"
	"acta/internal/postgres"
	"acta/internal/threadadapter"
	"acta/internal/threads"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestConversationReversePagesAndOldItemChanges(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	owner, e := f.service.Current(ctx, f.token)
	must(t, e)
	id := uuid.NewString()
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{{ID: id, RunID: id, Provider: "codex", ProviderID: "native", CWD: "/tmp", State: "running", CreatedAt: time.Now(), Revision: 1}}))
	state := threadadapter.State{NativeID: "native"}
	var seq int64
	var last []threads.Frame
	appendFrame := func(m map[string]any) {
		seq++
		raw, _ := json.Marshal(m)
		p := threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "codex", Sequence: seq, ReceivedAt: time.Now().UTC()}
		next, bundle, e := threadadapter.Map(state, p, "stdout", string(raw))
		must(t, e)
		_, e = f.store.AppendThreadFrames(ctx, owner.ID, id, bundle)
		must(t, e)
		state = next
		last = bundle
	}
	appendFrame(map[string]any{"id": id + "/thread/start", "result": map[string]any{"thread": map[string]any{"id": "native"}, "model": "model-a", "cwd": "/tmp"}})
	for n := 0; n < 75; n++ {
		appendFrame(map[string]any{"method": "item/started", "params": map[string]any{"threadId": "native", "turnId": "turn", "item": map[string]any{"type": "userMessage", "id": uuid.NewString(), "content": []any{map[string]any{"type": "text", "text": "page message"}}}}})
	}
	page, e := f.store.Conversation(ctx, owner.ID, id, conversation.Query{})
	must(t, e)
	if len(page.Items) != 50 || !page.HasMore || page.Revision != seq {
		t.Fatal(len(page.Items), page.HasMore, page.Revision, seq)
	}
	if conversation.Data(page.Current.Frames["thread/configuration"])["model"] != "model-a" {
		t.Fatal(page.Current)
	}
	older, e := f.store.Conversation(ctx, owner.ID, id, conversation.Query{Before: page.Next})
	must(t, e)
	if len(older.Items) != 25 || older.HasMore {
		t.Fatal(len(older.Items), older.HasMore)
	}
	first := older.Items[0]
	frame := first.Payload["frame"].(map[string]any)
	d := frame["data"].(map[string]any)
	watermark := page.Revision
	appendFrame(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "native", "turnId": "turn", "item": map[string]any{"type": "userMessage", "id": d["message_id"], "content": []any{map[string]any{"type": "text", "text": "completed later"}}}}})
	changes, e := f.store.Conversation(ctx, owner.ID, id, conversation.Query{After: &watermark})
	must(t, e)
	found := false
	for _, i := range changes.Items {
		if i.ID == first.ID {
			found = true
			if i.Sequence != first.Sequence || i.Payload["completed"] != true || i.Payload["text"] != "completed later" {
				t.Fatal(i)
			}
		}
	}
	if !found {
		t.Fatal("old item update missing")
	}
	// Re-delivery cannot apply deltas twice or advance a projection revision.
	_, e = f.store.AppendThreadFrames(ctx, owner.ID, id, last)
	must(t, e)
	repeat, e := f.store.Conversation(ctx, owner.ID, id, conversation.Query{After: &seq})
	must(t, e)
	if len(repeat.Items) != 0 {
		t.Fatal(repeat.Items)
	}
	_, e = f.store.Conversation(ctx, uuid.NewString(), id, conversation.Query{})
	if e == nil {
		t.Fatal("foreign owner read conversation")
	}
	debug, e := f.store.Conversation(ctx, owner.ID, id, conversation.Query{Debug: true})
	must(t, e)
	if len(debug.Items) != 50 {
		t.Fatal(len(debug.Items))
	}
	// A projection write failure must roll back the raw journal and watermark too.
	_, e = f.conn.Exec(ctx, fmt.Sprintf(`ALTER TABLE thread_conversation_items ADD CONSTRAINT reject_projection CHECK(revision<=%d)`, seq))
	must(t, e)
	raw, _ := json.Marshal(map[string]any{"method": "thread/status/changed", "params": map[string]any{"threadId": "native", "status": map[string]any{"type": "active", "activeFlags": []any{}}}})
	_, bundle, e := threadadapter.Map(state, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "codex", Sequence: seq + 1, ReceivedAt: time.Now().UTC()}, "stdout", string(raw))
	must(t, e)
	_, e = f.store.AppendThreadFrames(ctx, owner.ID, id, bundle)
	if e == nil {
		t.Fatal("projection failure accepted")
	}
	rollback, e := f.store.Conversation(ctx, owner.ID, id, conversation.Query{})
	must(t, e)
	if rollback.Revision != seq {
		t.Fatal("failed projection advanced journal", rollback.Revision)
	}
	_, e = f.conn.Exec(ctx, `ALTER TABLE thread_conversation_items DROP CONSTRAINT reject_projection`)
	must(t, e)
	// Simulate an old installation and prove startup rebuild reproduces current state.
	expected, _ := json.Marshal(rollback)
	_, e = f.conn.Exec(ctx, `UPDATE provider_threads SET conversation_version=0,conversation_state='{}' WHERE id=$1`, id)
	must(t, e)
	_, e = f.conn.Exec(ctx, `DELETE FROM thread_conversation_items WHERE thread_id=$1`, id)
	must(t, e)
	reopened, e := postgres.Open(ctx, f.url)
	must(t, e)
	defer reopened.Close()
	rebuilt, e := reopened.Conversation(ctx, owner.ID, id, conversation.Query{})
	must(t, e)
	actual, _ := json.Marshal(rebuilt)
	if string(actual) != string(expected) {
		t.Fatal("backfill differs from live ingestion")
	}

}

func TestConversationContextMeasurementBackfill(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	owner, err := f.service.Current(ctx, f.token)
	must(t, err)
	id := uuid.NewString()
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{{ID: id, RunID: id, Provider: "claude", ProviderID: "native", CWD: "/tmp", State: "running", CreatedAt: time.Now(), Revision: 1}}))
	measured := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	for n, counts := range []map[string]any{{"used_tokens": 500, "capacity_tokens": 1000, "estimated": true}, {"used_tokens": nil, "capacity_tokens": nil}} {
		p := threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: int64(n + 1), ReceivedAt: measured.Add(time.Duration(n) * time.Minute)}
		debug := threads.NewFrame(p, "debug/resolved", map[string]any{"raw": "{}", "stream": "stdout", "reason": "Test capture", "outputs": []threads.OutputReference{{OutputIndex: 1, Kind: "usage/context"}}})
		usage := threads.NewFrame(p, "usage/context", map[string]any{"turn_id": "turn", "context": counts, "cumulative": nil, "last_request": nil})
		usage.OutputIndex = 1
		_, err = f.store.AppendThreadFrames(ctx, owner.ID, id, []threads.Frame{debug, usage})
		must(t, err)
	}
	assertMeasurement := func(store *postgres.Store) {
		t.Helper()
		page, err := store.Conversation(ctx, owner.ID, id, conversation.Query{})
		must(t, err)
		measurement := page.Current.Frames["usage/context"]
		if measurement.Sequence != 1 || !measurement.ReceivedAt.Equal(measured) || page.Revision != 2 {
			t.Fatal("lost measurement timestamp", measurement, page.Revision)
		}
	}
	assertMeasurement(f.store)
	// Version 1 lost the previous measurement. Rebuild it from stored frames.
	_, err = f.conn.Exec(ctx, `UPDATE provider_threads SET conversation_version=1,conversation_state='{}' WHERE id=$1`, id)
	must(t, err)
	reopened, err := postgres.Open(ctx, f.url)
	must(t, err)
	defer reopened.Close()
	assertMeasurement(reopened)
}

func TestConversationLanesPersistAndPageIndependently(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	owner, e := f.service.Current(ctx, f.token)
	must(t, e)
	id := uuid.NewString()
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{{ID: id, RunID: id, Provider: "codex", ProviderID: "native", CWD: "/tmp", State: "running", CreatedAt: time.Now(), Revision: 1}}))
	state := threadadapter.State{NativeID: "native"}
	seq := int64(0)
	appendRaw := func(m map[string]any) {
		t.Helper()
		seq++
		raw, _ := json.Marshal(m)
		next, b, e := threadadapter.Map(state, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "codex", Sequence: seq, ReceivedAt: time.Now().UTC()}, "stdout", string(raw))
		must(t, e)
		for _, v := range b {
			if v.Kind == "debug/unknown" {
				t.Fatalf("unknown %s", v.Data)
			}
		}
		_, e = f.store.AppendThreadFrames(ctx, owner.ID, id, b)
		must(t, e)
		state = next
	}
	appendRaw(map[string]any{"method": "item/started", "params": map[string]any{"threadId": "native", "turnId": "t", "item": map[string]any{"type": "subAgentActivity", "id": "agent", "kind": "started", "agentThreadId": "child", "agentPath": "/root/child"}}})
	for n := 0; n < 8; n++ {
		for _, native := range []string{"native", "child"} {
			appendRaw(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": native, "turnId": "t", "item": map[string]any{"type": "userMessage", "id": fmt.Sprint(n), "content": []any{map[string]any{"type": "text", "text": native}}}}})
		}
	}
	parent, e := f.store.Conversation(ctx, owner.ID, id, conversation.Query{})
	must(t, e)
	child, e := f.store.Conversation(ctx, owner.ID, id, conversation.Query{LaneID: "child", Limit: 3})
	must(t, e)
	if child.LaneID != "child" || len(child.Items) != 3 || !child.HasMore || len(parent.Items) != 9 || len(parent.Current.Agents) != 1 {
		t.Fatal("lane counts", len(parent.Items), len(child.Items), child.HasMore)
	}
	for _, i := range parent.Items {
		if i.LaneID != "" {
			t.Fatal("child leaked into parent")
		}
	}
	for _, i := range child.Items {
		if i.LaneID != "child" {
			t.Fatal("parent leaked into child")
		}
	}
	older, e := f.store.Conversation(ctx, owner.ID, id, conversation.Query{LaneID: "child", Before: child.Next, Limit: 100})
	must(t, e)
	if len(older.Items) != 5 {
		t.Fatal(len(older.Items))
	}
	expected, _ := json.Marshal(child)
	_, e = f.conn.Exec(ctx, `UPDATE provider_threads SET conversation_version=0,conversation_state='{}' WHERE id=$1`, id)
	must(t, e)
	reopened, e := postgres.Open(ctx, f.url)
	must(t, e)
	defer reopened.Close()
	rebuilt, e := reopened.Conversation(ctx, owner.ID, id, conversation.Query{LaneID: "child", Limit: 3})
	must(t, e)
	actual, _ := json.Marshal(rebuilt)
	if string(expected) != string(actual) {
		t.Fatal("rebuild changed lane projection")
	}
}
