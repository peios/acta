package threadadapter

import (
	"acta/internal/threads"
	"encoding/json"
	"testing"
	"time"
)

func TestCodexFileChangesAndTurnDiff(t *testing.T) {
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	s := State{RunID: id, NativeID: "native"}
	seq := int64(0)
	apply := func(method string, params object) []threads.Frame {
		t.Helper()
		params["threadId"] = "native"
		params["turnId"] = "turn"
		raw, _ := json.Marshal(object{"method": method, "params": params})
		seq++
		next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "codex", Sequence: seq, ReceivedAt: time.Now()}, "stdout", string(raw))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range frames {
			var d object
			json.Unmarshal(f.Data, &d)
			if f.Kind == "debug/unknown" || d["processing_error"] != nil {
				t.Fatalf("bad mapping: %s %s", raw, f.Data)
			}
		}
		saved, _ := json.Marshal(next)
		s, err = DecodeState(saved)
		if err != nil {
			t.Fatal(err)
		}
		return frames
	}
	for _, action := range []string{"add", "update", "delete"} {
		patch := "@@ -1 +1 @@\n-old\n+new\n"
		if action == "add" {
			patch = "new\n"
		}
		if action == "delete" {
			patch = "old\n"
		}
		changes := []any{object{"path": "/tmp/check.txt", "kind": object{"type": action}, "diff": patch}}
		apply("item/started", object{"item": object{"type": "fileChange", "id": action, "changes": changes, "status": "inProgress"}, "startedAtMs": 1000})
		done := apply("item/completed", object{"item": object{"type": "fileChange", "id": action, "changes": changes, "status": "completed"}, "completedAtMs": 1200})
		if len(done) != 2 || done[1].Kind != "tool/call" {
			t.Fatalf("missing tool snapshot: %+v", done)
		}
		call := s.Tools[action]
		if call.Status != "completed" || len(call.Changes) != 1 || call.Changes[0]["diff"] != patch {
			t.Fatalf("lost operation diff: %+v", call)
		}
		apply("item/started", object{"item": object{"type": "fileChange", "id": action, "changes": changes, "status": "inProgress"}})
		if s.Tools[action].Status != "completed" {
			t.Fatal("stale start reopened completed edit")
		}
	}
	for _, diff := range []string{"diff --git a/check.txt b/check.txt\n-old\n+new\n", ""} {
		frames := apply("turn/diff/updated", object{"diff": diff})
		if len(frames) != 2 || frames[1].Kind != "turn/diff" {
			t.Fatalf("bad turn diff: %+v", frames)
		}
		var d object
		json.Unmarshal(frames[1].Data, &d)
		if d["diff"] != diff || d["turn_id"] != "turn" {
			t.Fatal(d)
		}
	}
}

func TestCodexInvalidFileChangesRemainUnknown(t *testing.T) {
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	for _, raw := range []string{
		`{"method":"turn/diff/updated","params":{"threadId":"other","turnId":"turn","diff":"patch"}}`,
		`{"method":"turn/diff/updated","params":{"threadId":"native","diff":"patch"}}`,
		`{"method":"turn/diff/updated","params":{"threadId":"native","turnId":"turn","diff":null}}`,
		`{"method":"item/started","params":{"threadId":"native","turnId":"turn","item":{"type":"fileChange","id":"x","status":"inProgress","changes":[{"path":"/tmp/a","kind":{"type":"future"},"diff":"text"}]}}}`,
	} {
		_, frames, err := Map(State{RunID: id, NativeID: "native"}, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "codex", Sequence: 1, ReceivedAt: time.Now()}, "stdout", raw)
		if err != nil || len(frames) != 1 || frames[0].Kind != "debug/unknown" {
			t.Fatalf("expected unknown: %v %+v", err, frames)
		}
	}
}
