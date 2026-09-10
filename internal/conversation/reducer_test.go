package conversation

import (
	"acta2/internal/threads"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"
)

type memoryItems map[string]*Item

func (m memoryItems) Get(id string) (*Item, error) { return m[id], nil }
func (m memoryItems) Save(i *Item) error           { m[i.ID] = i; return nil }
func (m memoryItems) InTurn(run, turn string) ([]*Item, error) {
	out := []*Item{}
	for _, i := range m {
		if i.RunID == run && i.TurnID == turn {
			out = append(out, i)
		}
	}
	return out, nil
}
func TestPersistedLifecycleAndSingleton(t *testing.T) {
	store := memoryItems{}
	c := Current{}
	r := Reducer{store, &c}
	var seq int64
	apply := func(kind string, d Object) {
		seq++
		f := threads.NewFrame(threads.ProviderFrame{ThreadID: "thread", RunID: "run", Sequence: seq, ReceivedAt: time.Now()}, kind, d)
		if e := r.Apply(f); e != nil {
			t.Fatal(e)
		}
	}
	apply("thread/configuration", Object{"model": "model-a"})
	apply("thread/configuration", Object{"model": "model-b"})
	apply("tool/call", Object{"tool_id": "tool", "turn_id": "turn", "status": "running", "arguments": Object{}, "output": ""})
	tool := store[key("tool", "run", "tool")]
	position := tool.Sequence
	apply("tool/output/delta", Object{"tool_id": "tool", "turn_id": "turn", "text": "result"})
	apply("tool/call", Object{"tool_id": "tool", "turn_id": "turn", "status": "completed", "output": "authoritative result"})
	if tool.Sequence != position || tool.Payload["status"] != "completed" || tool.Payload["output"] != "authoritative result" || tool.Payload["completed_at"] == nil {
		t.Fatal(tool)
	}
	if Data(c.Frames["thread/configuration"])["model"] != "model-b" {
		t.Fatal(c)
	}
	configs := 0
	for _, i := range store {
		if obj(i.Payload["frame"])["kind"] == "thread/configuration" {
			configs++
		}
	}
	if configs != 2 {
		t.Fatal(configs)
	}
	apply("approval/request", Object{"approval_id": "a", "turn_id": "turn"})
	if len(c.Pending) != 1 {
		t.Fatal(c)
	}
	apply("approval/resolved", Object{"approval_id": "a"})
	if len(c.Pending) != 0 || !yes(store[key("approval", "run", "a")].Payload["resolved"]) {
		t.Fatal(c)
	}
	apply("turn/completed", Object{"turn_id": "turn", "outcome": "completed"})
	apply("usage/context", Object{"turn_id": "turn", "last_request": Object{"output_tokens": 12}})
	ending := store[key("turn", "run", "turn")]
	if len(list(ending.Payload["tokens"])) != 1 {
		t.Fatal(ending)
	}
}
func TestMCPFailureMovesOutOfStack(t *testing.T) {
	store := memoryItems{}
	c := Current{}
	r := Reducer{store, &c}
	for idx, d := range []Object{{"server_name": "a", "status": "starting"}, {"server_name": "b", "status": "starting"}, {"server_name": "a", "status": "failed", "error": Object{"message": "failed"}}, {"server_name": "b", "status": "ready"}, {"server_name": "c", "status": "starting"}} {
		f := threads.NewFrame(threads.ProviderFrame{ThreadID: "thread", RunID: "run", Sequence: int64(idx + 1)}, "mcp/server/status", d)
		if e := r.Apply(f); e != nil {
			t.Fatal(e)
		}
	}
	if len(store) != 3 {
		t.Fatal(store)
	}
	var sealed, active, failed int
	for _, i := range store {
		switch i.Payload["kind"] {
		case "tools":
			if yes(i.Payload["sealed"]) {
				sealed++
				if len(list(i.Payload["servers"])) != 1 {
					t.Fatal(i)
				}
			} else {
				active++
			}
		case "tool-error":
			failed++
			if i.Sequence != 3 {
				t.Fatal(i)
			}
		}
	}
	if sealed != 1 || active != 1 || failed != 1 {
		t.Fatal(sealed, active, failed)
	}
}

// Optional local evidence replay: this uses captured normalized frames, never
// starts a provider or sends a prompt. Output can be compared with the old UI.
func TestCapturedConversationReplay(t *testing.T) {
	path := os.Getenv("ACTA_CONVERSATION_REPLAY")
	if path == "" {
		t.Skip("no local capture fixture")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var frames []threads.Frame
	if e = json.Unmarshal(raw, &frames); e != nil {
		t.Fatal(e)
	}
	stores := map[string]memoryItems{}
	states := map[string]*Current{}
	for _, f := range frames {
		if stores[f.ThreadID] == nil {
			stores[f.ThreadID] = memoryItems{}
			states[f.ThreadID] = &Current{}
		}
		r := Reducer{stores[f.ThreadID], states[f.ThreadID]}
		if e = r.Apply(f); e != nil {
			t.Fatal(e)
		}
	}
	out := map[string][]*Item{}
	for id, m := range stores {
		for _, i := range m {
			out[id] = append(out[id], i)
		}
		sort.Slice(out[id], func(a, b int) bool {
			x, y := out[id][a], out[id][b]
			if x.Sequence != y.Sequence {
				return x.Sequence < y.Sequence
			}
			return x.OutputIndex < y.OutputIndex
		})
	}
	raw, e = json.Marshal(out)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path+".items.json", raw, 0600); e != nil {
		t.Fatal(e)
	}
}

func TestTurnCancellationCorrectsDividerWithoutMovingOrLosingDetails(t *testing.T) {
	store := memoryItems{}
	current := Current{}
	r := Reducer{store, &current}
	apply := func(seq int64, d Object) {
		t.Helper()
		f := threads.NewFrame(threads.ProviderFrame{ThreadID: "thread", RunID: "run", Sequence: seq, ReceivedAt: time.Unix(seq, 0)}, "turn/completed", d)
		if e := r.Apply(f); e != nil {
			t.Fatal(e)
		}
	}
	apply(1, Object{"turn_id": "turn", "outcome": "completed", "duration_ms": 9087})
	item := store[key("turn", "run", "turn")]
	at := item.Payload["completed_at"]
	apply(2, Object{"turn_id": "turn", "outcome": "interrupted"})
	apply(3, Object{"turn_id": "turn", "outcome": "completed", "duration_ms": 9087})
	if item.Sequence != 1 || item.Payload["label"] != "Turn interrupted" || item.Payload["durationMs"] != json.Number("9087") || item.Payload["completed_at"] != at || item.Revision != 3 {
		t.Fatalf("%+v", item)
	}
	if len(store) != 1 {
		t.Fatal("duplicate divider", store)
	}
}

func TestBackgroundSurvivesTurnAndPagination(t *testing.T) {
	store := memoryItems{}
	c := Current{}
	r := Reducer{store, &c}
	var seq int64
	apply := func(kind string, d Object) {
		seq++
		f := threads.NewFrame(threads.ProviderFrame{ThreadID: "thread", RunID: "run", Sequence: seq, ReceivedAt: time.Now()}, kind, d)
		if e := r.Apply(f); e != nil {
			t.Fatal(e)
		}
	}
	apply("tool/call", Object{"tool_id": "tool", "turn_id": "turn", "status": "background", "background_task_id": "task"})
	position := store[key("tool", "run", "tool")].Sequence
	apply("turn/completed", Object{"turn_id": "turn", "outcome": "completed"})
	if len(c.Background) != 1 || store[key("tool", "run", "tool")].Payload["status"] != "background" {
		t.Fatal("turn ended background command")
	}
	// Current state restores independently of whichever history page is loaded.
	raw, _ := json.Marshal(c)
	c = Current{}
	if e := json.Unmarshal(raw, &c); e != nil {
		t.Fatal(e)
	}
	if len(c.Background) != 1 {
		t.Fatal("lost background count")
	}
	apply("tool/call", Object{"tool_id": "tool", "turn_id": "turn", "status": "completed", "background_task_id": "task"})
	if len(c.Background) != 0 || store[key("tool", "run", "tool")].Sequence != position {
		t.Fatal("lost completion or original position")
	}
	apply("tool/notification", Object{"tool_id": "tool", "turn_id": "turn", "task_id": "task", "status": "completed", "summary": "Finished"})
	notice := store[key("tool-notification", "run", "task")]
	if notice == nil || notice.Sequence <= position || notice.Visibility != "normal" {
		t.Fatal("missing chronological notice")
	}
}

func TestBackgroundNoticeMovesOnContextEcho(t *testing.T) {
	store := memoryItems{}
	c := Current{}
	r := Reducer{store, &c}
	apply := func(seq int64, context bool) {
		f := threads.NewFrame(threads.ProviderFrame{ThreadID: "thread", RunID: "run", Sequence: seq, ReceivedAt: time.Now()}, "tool/notification", Object{"task_id": "task", "tool_id": "tool", "turn_id": "turn", "summary": "Done", "context_entry": context})
		f.OutputIndex = 1
		if e := r.Apply(f); e != nil {
			t.Fatal(e)
		}
	}
	apply(1, false)
	id := key("tool-notification", "run", "task")
	if len(store) != 1 || store[id].Sequence != 1 {
		t.Fatal("missing immediate notice")
	}
	apply(50, true)
	if len(store) != 1 || store[id].Sequence != 50 || store[id].Revision != 50 {
		t.Fatal("echo did not move same item")
	}
	apply(51, false)
	apply(52, true)
	if len(store) != 1 || store[id].Sequence != 50 || store[id].Revision != 50 {
		t.Fatal("duplicate delivery moved notice")
	}
	// A context echo without an earlier notification still creates one item.
	store = memoryItems{}
	r.Store = store
	apply(60, true)
	if len(store) != 1 || store[id].Sequence != 60 {
		t.Fatal("lost first context echo")
	}
}

func TestQuestionCurrentAndResolution(t *testing.T) {
	for _, blocking := range []bool{true, false} {
		store := memoryItems{}
		current := Current{}
		r := Reducer{store, &current}
		apply := func(sequence int64, kind string, data Object) {
			t.Helper()
			if err := r.Apply(threads.NewFrame(threads.ProviderFrame{ThreadID: "thread", RunID: "run", Sequence: sequence, ReceivedAt: time.Now()}, kind, data)); err != nil {
				t.Fatal(err)
			}
		}
		apply(1, "question/request", Object{"question_id": "request", "turn_id": "turn", "blocking": blocking})
		if len(current.Pending) != 1 {
			t.Fatal("lost pending question")
		}
		apply(2, "turn/completed", Object{"turn_id": "turn", "outcome": "completed"})
		if blocking && len(current.Pending) != 0 || !blocking && len(current.Pending) != 1 {
			t.Fatal("wrong question lifetime", current.Pending)
		}
		apply(3, "question/resolved", Object{"question_id": "request"})
		if len(current.Pending) != 0 || !yes(store[key("approval", "run", "request")].Payload["resolved"]) {
			t.Fatal("resolution lost")
		}
	}
}

func TestCompactionProjection(t *testing.T) {
	store := memoryItems{}
	c := Current{}
	r := Reducer{store, &c}
	seq := int64(0)
	apply := func(id, state, summary string) {
		seq++
		d := Object{"compaction_id": id, "turn_id": "turn", "state": state, "summary": summary}
		if e := r.Apply(threads.NewFrame(threads.ProviderFrame{RunID: "run", Sequence: seq}, "context/compaction", d)); e != nil {
			t.Fatal(e)
		}
	}
	apply("one", "in_progress", "")
	i := store[key("compaction", "run", "one")]
	if i.Visibility != "hidden" || Data(c.Frames["context/compaction"])["state"] != "in_progress" {
		t.Fatal(i, c)
	}
	apply("one", "completed", "")
	position := i.Sequence
	apply("one", "completed", "Summary")
	if len(store) != 1 || i.Visibility != "normal" || i.Sequence != position || obj(obj(i.Payload["frame"])["data"])["summary"] != "Summary" {
		t.Fatal(store)
	}
	apply("one", "in_progress", "")
	if Data(c.Frames["context/compaction"])["state"] != "completed" {
		t.Fatal("reopened")
	}
	apply("two", "in_progress", "")
	apply("one", "completed", "Late summary")
	if Data(c.Frames["context/compaction"])["compaction_id"] != "two" {
		t.Fatal("late summary cleared current operation")
	}
	if e := r.Apply(threads.NewFrame(threads.ProviderFrame{RunID: "run", Sequence: seq + 1}, "turn/completed", Object{"turn_id": "turn", "outcome": "interrupted"})); e != nil {
		t.Fatal(e)
	}
	if _, ok := c.Frames["context/compaction"]; ok {
		t.Fatal("stale compacting status")
	}
}

func TestBackgroundNoticeRepeatsAcrossRunsWithoutChangingTool(t *testing.T) {
	store := memoryItems{}
	c := Current{}
	r := Reducer{store, &c}
	apply := func(run, kind string, seq int64, d Object) {
		if e := r.Apply(threads.NewFrame(threads.ProviderFrame{RunID: run, Sequence: seq}, kind, d)); e != nil {
			t.Fatal(e)
		}
	}
	apply("old", "tool/call", 1, Object{"tool_id": "tool", "turn_id": "turn", "status": "interrupted"})
	tool := store[key("tool", "old", "tool")]
	revision := tool.Revision
	notice := Object{"tool_id": "tool", "task_id": "task", "turn_id": "turn", "status": "interrupted", "summary": "Stopped", "context_entry": false}
	apply("old", "tool/notification", 2, notice)
	apply("new", "tool/notification", 3, notice)
	if len(store) != 3 || tool.Revision != revision {
		t.Fatal("changed old tool or lost a notice", store)
	}
	notice["context_entry"] = true
	apply("new", "tool/notification", 4, notice)
	if store[key("tool-notification", "old", "task")].Sequence != 2 || store[key("tool-notification", "new", "task")].Sequence != 4 || len(c.Background) != 0 {
		t.Fatal(store, c)
	}
}

func TestUserImagePersistsAsOneCollapsedMessage(t *testing.T) {
	store := memoryItems{}
	current := Current{}
	r := Reducer{store, &current}
	for n, state := range []string{"in_progress", "completed"} {
		f := threads.NewFrame(threads.ProviderFrame{ThreadID: "thread", RunID: "run", Sequence: int64(n + 1), ReceivedAt: time.Now()}, "message/user", Object{"message_id": "image-message", "turn_id": "turn", "submission_id": "command", "state": state, "content": []any{Object{"type": "text", "text": "look"}, Object{"type": "image", "media_type": "image/png", "base64": "YWJj"}}})
		if e := r.Apply(f); e != nil {
			t.Fatal(e)
		}
	}
	if len(store) != 1 {
		t.Fatal("duplicate message", len(store))
	}
	for _, i := range store {
		if i.Payload["text"] != "look" || !yes(i.Payload["completed"]) || len(list(i.Payload["images"])) != 1 {
			t.Fatal(i.Payload)
		}
	}
}
