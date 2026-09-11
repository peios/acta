package threadadapter

import (
	"acta/internal/threads"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestCompactionCaptureCheckpointAndDuplicates(t *testing.T) {
	raw, e := os.ReadFile("testdata/claude-compaction.json")
	if e != nil {
		t.Fatal(e)
	}
	var capture []json.RawMessage
	if e = json.Unmarshal(raw, &capture); e != nil {
		t.Fatal(e)
	}
	s := State{RunID: "659f0af7-6a65-4b34-8b24-d295d9feb4ea", NativeID: "db799357-e3f3-4f43-9960-df8e8e3bd330", Claude: ClaudeState{Turn: "turn"}}
	apply := func(raw []byte, seq int64) []threads.Frame {
		var f []threads.Frame
		s, f, e = Map(s, threads.ProviderFrame{ThreadID: "db799357-e3f3-4f43-9960-df8e8e3bd330", RunID: "659f0af7-6a65-4b34-8b24-d295d9feb4ea", Provider: "claude", Sequence: seq, ReceivedAt: time.Date(2026, 9, 8, 21, 26, int(seq), 0, time.UTC)}, "stdout", string(raw))
		if e != nil {
			t.Fatal(e)
		}
		checkpoint, _ := json.Marshal(s)
		s, e = DecodeState(checkpoint)
		if e != nil {
			t.Fatal(e)
		}
		return f
	}
	var id string
	for i, raw := range capture {
		f := apply(raw, int64(i+1))
		if len(f) != 2 || f[1].Kind != "context/compaction" {
			t.Fatalf("%d: %+v", i, f)
		}
		var d object
		_ = json.Unmarshal(f[1].Data, &d)
		if i == 0 {
			id = str(d["compaction_id"])
		} else if d["compaction_id"] != id {
			t.Fatal("changed logical identity")
		}
		if i == 1 && (d["before_tokens"] != float64(42638) || d["after_tokens"] != float64(6648) || d["duration_ms"] != float64(21680)) {
			t.Fatal(d)
		}
		if i == 2 && len(str(d["summary"])) < 100 {
			t.Fatal("missing summary")
		}
		if dup := apply(raw, int64(i+10)); len(dup) != 1 || dup[0].Kind == "debug/unknown" {
			t.Fatalf("duplicate: %+v", dup)
		}
	}
	// A synthetic user message is not a summary merely because of its prose.
	if f := apply([]byte(`{"type":"user","session_id":"db799357-e3f3-4f43-9960-df8e8e3bd330","uuid":"unrelated","isSynthetic":true,"message":{"role":"user","content":"Summary"}}`), 20); f[0].Kind != "debug/unknown" {
		t.Fatal(f)
	}
}
func TestCodexCompaction(t *testing.T) {
	for _, withStart := range []bool{true, false} {
		s := State{RunID: "659f0af7-6a65-4b34-8b24-d295d9feb4ea", NativeID: "native"}
		seq := int64(0)
		apply := func(method string) []threads.Frame {
			seq++
			var f []threads.Frame
			var e error
			s, f, e = Map(s, threads.ProviderFrame{ThreadID: "db799357-e3f3-4f43-9960-df8e8e3bd330", RunID: "659f0af7-6a65-4b34-8b24-d295d9feb4ea", Provider: "codex", Sequence: seq, ReceivedAt: time.Unix(seq, 0)}, "stdout", `{"method":"`+method+`","params":{"threadId":"native","turnId":"turn","item":{"type":"contextCompaction","id":"compact"}}}`)
			if e != nil {
				t.Fatal(e)
			}
			return f
		}
		if withStart {
			if f := apply("item/started"); len(f) != 2 {
				t.Fatal(f)
			}
		}
		f := apply("item/completed")
		if len(f) != 2 {
			t.Fatal(f)
		}
		var d object
		_ = json.Unmarshal(f[1].Data, &d)
		if d["summary"] != nil || d["before_tokens"] != nil || d["state"] != "completed" {
			t.Fatal(d)
		}
		if !withStart && d["duration_ms"] != nil {
			t.Fatal("invented duration")
		}
		if f = apply("item/completed"); len(f) != 1 {
			t.Fatal("duplicate completion", f)
		}
		if f = apply("item/started"); len(f) != 1 {
			t.Fatal("reopened completed compaction", f)
		}
	}
}
