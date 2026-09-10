package hyperharness

import (
	"acta2/internal/threadadapter"
	"acta2/internal/threads"
	"testing"
)

func TestLaneControlCannotRetargetParentOrOtherRun(t *testing.T) {
	r := &localThread{Thread: threads.Descriptor{ID: "acta", RunID: "run", ProviderID: "parent", Provider: "codex"}, Adapter: threadadapter.State{Lanes: map[string]*threadadapter.Lane{"child": {ID: "child", Native: "native-child", State: threadadapter.State{RunID: "run"}}}}}
	q := threads.Control{LaneID: "child", RunID: "run", Action: "interrupt"}
	target, e := laneTarget(r, q)
	if e != nil || target.Thread.ProviderID != "native-child" || r.Thread.ProviderID != "parent" || target.Thread.ID != "acta" {
		t.Fatal(target, e)
	}
	q.RunID = "old"
	if _, e = laneTarget(r, q); e == nil {
		t.Fatal("old run accepted")
	}
	q.RunID = "run"
	q.LaneID = "other"
	if _, e = laneTarget(r, q); e == nil {
		t.Fatal("arbitrary native id accepted")
	}
	q.LaneID = "child"
	q.Action = "send"
	if _, e = laneTarget(r, q); e == nil {
		t.Fatal("unsupported native send advertised")
	}
}
