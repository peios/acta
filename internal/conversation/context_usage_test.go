package conversation

import (
	"acta2/internal/threads"
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func TestContextMeasurementRetainsSourceTime(t *testing.T) {
	store := memoryItems{}
	current := Current{}
	r := Reducer{store, &current}
	base := time.Date(2026, 9, 8, 19, 0, 0, 0, time.UTC)
	seq := int64(0)
	apply := func(run string, context Object) threads.Frame {
		t.Helper()
		seq++
		f := threads.NewFrame(threads.ProviderFrame{ThreadID: "thread", RunID: run, Sequence: seq, ReceivedAt: base.Add(time.Duration(seq) * time.Minute)}, "usage/context", Object{"turn_id": "turn", "context": context, "last_request": Object{"output_tokens": seq}})
		if err := r.Apply(f); err != nil {
			t.Fatal(err)
		}
		// Round-trip the current state as the database does between inputs.
		raw, _ := json.Marshal(current)
		if err := decode(raw, &current); err != nil {
			t.Fatal(err)
		}
		return f
	}
	known := apply("run", Object{"used_tokens": 500, "capacity_tokens": 1000, "estimated": true})
	for _, unknown := range []Object{
		{"used_tokens": nil, "capacity_tokens": nil},
		{"used_tokens": nil, "capacity_tokens": 2000},
		{"used_tokens": 600, "capacity_tokens": nil},
		{"used_tokens": 600, "capacity_tokens": 0},
		{},
	} {
		f := apply("run", unknown)
		kept := current.Frames["usage/context"]
		if kept.Sequence != known.Sequence || !kept.ReceivedAt.Equal(known.ReceivedAt) || string(kept.Data) != string(known.Data) {
			t.Fatalf("measurement changed: %+v", kept)
		}
		turn := store[key("turn", "run", "turn")]
		if got := obj(list(turn.Payload["tokens"])[0])["value"]; got != json.Number(strconv.FormatInt(seq, 10)) {
			t.Fatalf("latest call tokens were lost: %v", got)
		}
		if store[frameKey(f)] == nil {
			t.Fatal("usage history was lost")
		}
	}
	fresh := apply("run", Object{"used_tokens": 0, "capacity_tokens": 2000, "estimated": true})
	if !current.Frames["usage/context"].ReceivedAt.Equal(fresh.ReceivedAt) {
		t.Fatal("fresh zero measurement not accepted")
	}
	newRun := apply("new-run", Object{"used_tokens": nil, "capacity_tokens": nil})
	if current.Frames["usage/context"].Sequence != newRun.Sequence {
		t.Fatal("previous run leaked into new run")
	}
}
