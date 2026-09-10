package conversation

import (
	"acta2/internal/threads"
	"encoding/json"
	"testing"
	"time"
)

func TestLaneIsolationAndParentCompletion(t *testing.T) {
	store := memoryItems{}
	c := Current{}
	r := Reducer{store, &c}
	var seq int64
	apply := func(lane, kind string, d Object) {
		t.Helper()
		seq++
		f := threads.NewFrame(threads.ProviderFrame{ThreadID: "thread", RunID: "run", Sequence: seq, ReceivedAt: time.Now()}, kind, d)
		f.LaneID = lane
		if e := r.Apply(f); e != nil {
			t.Fatal(e)
		}
	}
	for _, lane := range []string{"", "child", "nested"} {
		apply(lane, "thread/configuration", Object{"model": "model-" + lane})
		apply(lane, "tool/call", Object{"tool_id": "same", "turn_id": "same", "status": "running", "arguments": Object{}})
		apply(lane, "approval/request", Object{"approval_id": "same", "turn_id": "same"})
	}
	apply("", "turn/completed", Object{"turn_id": "same", "outcome": "completed"})
	if len(c.Pending) != 0 || len(c.Lanes["child"].Pending) != 1 || len(c.Lanes["nested"].Pending) != 1 {
		t.Fatal("parent completion cleared child requests", c)
	}
	if Data(c.Frames["thread/configuration"])["model"] != "model-" || Data(c.Lanes["child"].Frames["thread/configuration"])["model"] != "model-child" {
		t.Fatal("configuration leaked")
	}
	child := store[key("lane", "child")+":"+key("tool", "run", "same")]
	if child == nil || child.Payload["status"] != "running" {
		t.Fatal("parent completed child tool", child)
	}
	// Restore persisted singleton before completing one child.
	raw, _ := json.Marshal(c)
	json.Unmarshal(raw, &c)
	apply("child", "approval/resolved", Object{"approval_id": "same"})
	apply("child", "tool/call", Object{"tool_id": "same", "turn_id": "same", "status": "completed"})
	if len(c.Lanes["child"].Pending) != 0 || len(c.Lanes["nested"].Pending) != 1 {
		t.Fatal("resolution crossed lanes")
	}
	if len(store) != 11 {
		t.Fatalf("unexpected identities or duplicate update: %d", len(store))
	}
}
func TestSubagentReplacesToolAndMovesOneNotice(t *testing.T) {
	store := memoryItems{}
	c := Current{}
	r := Reducer{store, &c}
	var seq int64
	apply := func(kind string, d Object) {
		t.Helper()
		seq++
		f := threads.NewFrame(threads.ProviderFrame{ThreadID: "thread", RunID: "run", Sequence: seq}, kind, d)
		if e := r.Apply(f); e != nil {
			t.Fatal(e)
		}
	}
	apply("tool/call", Object{"tool_id": "agent", "turn_id": "t", "status": "running"})
	apply("subagent/status", Object{"lane_id": "child", "tool_id": "agent", "status": "running"})
	apply("tool/call", Object{"tool_id": "agent", "turn_id": "t", "status": "completed"})
	card := store[key("tool", "run", "agent")]
	if card.Sequence != 1 || card.Payload["kind"] != "subagent" {
		t.Fatal(card)
	}
	apply("subagent/notification", Object{"lane_id": "child", "tool_id": "agent", "context_entry": false})
	apply("subagent/notification", Object{"lane_id": "child", "tool_id": "agent", "context_entry": true})
	apply("subagent/notification", Object{"lane_id": "child", "tool_id": "agent", "context_entry": false})
	notice := store[key("agent-notice", "run", "child", "agent")]
	if notice.Sequence != 5 || len(store) != 2 {
		t.Fatal("duplicate or regressed notification", store)
	}
}

func TestCodexDiscoveryBatchKeepsSeparateSiblingCards(t *testing.T) {
	store := memoryItems{}
	c := Current{}
	r := Reducer{store, &c}
	for index, lane := range []string{"first", "second"} {
		f := threads.NewFrame(threads.ProviderFrame{ThreadID: "thread", RunID: "run", Provider: "codex", Sequence: int64(index + 1)}, "subagent/status", Object{"lane_id": lane, "tool_id": "shared-wait-call", "status": "running"})
		if e := r.Apply(f); e != nil {
			t.Fatal(e)
		}
	}
	if len(store) != 2 {
		t.Fatalf("sibling cards collapsed into one coordination call: %d", len(store))
	}
}
