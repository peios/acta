package conversation

import (
	"acta2/internal/threads"
	"testing"
	"time"
)

func TestTurnDiffSnapshotReplacesAndPreservesFallback(t *testing.T) {
	store := memoryItems{}
	r := Reducer{store, &Current{}}
	seq := int64(0)
	apply := func(kind string, data Object) {
		t.Helper()
		seq++
		f := threads.NewFrame(threads.ProviderFrame{ThreadID: "thread", RunID: "run", Sequence: seq, ReceivedAt: time.Now()}, kind, data)
		if err := r.Apply(f); err != nil {
			t.Fatal(err)
		}
	}
	apply("turn/diff", Object{"turn_id": "turn", "diff": "first patch", "mode": "combined", "incomplete": false})
	apply("turn/diff", Object{"turn_id": "turn", "diff": "final patch", "mode": "sequential", "incomplete": true})
	apply("turn/completed", Object{"turn_id": "turn", "outcome": "completed"})
	i := store[key("turn", "run", "turn")]
	if i.Visibility != "normal" || i.Payload["diff"] != "final patch" || i.Payload["diffMode"] != "sequential" || i.Payload["diffIncomplete"] != true {
		t.Fatal(i)
	}
	position := i.Sequence
	apply("turn/diff", Object{"turn_id": "turn", "diff": "", "mode": "combined", "incomplete": false})
	if i.Sequence != position || i.Payload["diff"] != "" || i.Payload["diffIncomplete"] != false {
		t.Fatal(i)
	}
}
