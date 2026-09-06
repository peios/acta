package agentsession

import (
	"testing"

	"github.com/peios/acta/internal/agentsession/model"
)

func seqs(w Window) []int64 {
	var out []int64
	for _, e := range w.Events {
		out = append(out, e.Seq)
	}
	return out
}

func eq(a []int64, b ...int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWindowsCutAtTurns(t *testing.T) {
	// three turns (1-3, 4-5, 6-8) and one under way (9)
	evs := []model.Event{
		{Seq: 1, T: model.Input}, {Seq: 2, T: model.Assistant}, {Seq: 3, T: model.TurnIdle},
		{Seq: 4, T: model.Input}, {Seq: 5, T: model.TurnIdle},
		{Seq: 6, T: model.Input}, {Seq: 7, T: model.Assistant}, {Seq: 8, T: model.TurnIdle},
		{Seq: 9, T: model.Input},
	}
	if w := Tail(evs, 2, 0); !eq(seqs(w), 6, 7, 8, 9) || !w.More {
		t.Errorf("tail 2: %v more=%v", seqs(w), w.More)
	}
	if w := Tail(evs, 10, 0); !eq(seqs(w), 1, 2, 3, 4, 5, 6, 7, 8, 9) || w.More {
		t.Errorf("tail 10: %v more=%v", seqs(w), w.More)
	}
	if w := Before(evs, 6, 1, 0); !eq(seqs(w), 4, 5) || !w.More {
		t.Errorf("before 6: %v more=%v", seqs(w), w.More)
	}
	if w := Before(evs, 4, 5, 0); !eq(seqs(w), 1, 2, 3) || w.More {
		t.Errorf("before 4: %v more=%v", seqs(w), w.More)
	}
	if w := After(evs, 3, 1, 0); !eq(seqs(w), 4, 5) || !w.More {
		t.Errorf("after 3: %v more=%v", seqs(w), w.More)
	}
	if w := After(evs, 5, 5, 0); !eq(seqs(w), 6, 7, 8, 9) || w.More {
		t.Errorf("after 5: %v more=%v", seqs(w), w.More)
	}
	if w := After(evs, 9, 5, 0); len(w.Events) != 0 || w.More {
		t.Errorf("after the end: %v more=%v", seqs(w), w.More)
	}
}

// TestWindowsHonourFrameBudget checks whole turns are taken until the frame
// budget is spent, at least one turn is always taken, and a single turn far
// beyond the budget is cut inside itself.
func TestWindowsHonourFrameBudget(t *testing.T) {
	var evs []model.Event
	seq := int64(0)
	turn := func(frames int) {
		for i := 0; i < frames-1; i++ {
			seq++
			evs = append(evs, model.Event{Seq: seq, T: model.Assistant})
		}
		seq++
		evs = append(evs, model.Event{Seq: seq, T: model.TurnIdle})
	}
	turn(10) // 1-10
	turn(10) // 11-20
	turn(10) // 21-30
	turn(4)  // 31-34

	// budget of 15 frames from the tail: the last turn (4) fits, the one
	// before (10) fits, the next would not
	if w := Tail(evs, 40, 15); len(w.Events) != 14 || w.Events[0].Seq != 21 || !w.More {
		t.Errorf("Tail 15 frames: %d events from %d, more=%v", len(w.Events), w.Events[0].Seq, w.More)
	}
	// a budget smaller than the newest turn still takes that turn whole
	if w := Tail(evs, 40, 2); len(w.Events) != 4 || !w.More {
		t.Errorf("Tail 2 frames: %d events", len(w.Events))
	}
	// After from seq 20 with 15 frames: the 10-frame turn, then the 4 fits too
	if w := After(evs, 20, 40, 15); len(w.Events) != 14 || w.More {
		t.Errorf("After 15 frames: %d events, more=%v", len(w.Events), w.More)
	}
	// one enormous turn: cut inside at the budget
	var big []model.Event
	for i := 1; i <= 100; i++ {
		big = append(big, model.Event{Seq: int64(i), T: model.Assistant})
	}
	if w := Tail(big, 40, 30); len(w.Events) != 30 || w.Events[0].Seq != 71 || !w.More {
		t.Errorf("Tail of one big turn: %d events from %d", len(w.Events), w.Events[0].Seq)
	}
	if w := After(big, 0, 40, 30); len(w.Events) != 30 || w.Events[29].Seq != 30 || !w.More {
		t.Errorf("After of one big turn: %d events to %d", len(w.Events), w.Events[len(w.Events)-1].Seq)
	}
}

// TestWindowsFillFromALongTurn checks a window that would open nearly empty,
// because the next turn is over budget, is filled from inside that turn.
func TestWindowsFillFromALongTurn(t *testing.T) {
	var evs []model.Event
	for i := 1; i <= 50; i++ { // one long turn, 1-50
		evs = append(evs, model.Event{Seq: int64(i), T: model.Assistant})
	}
	evs[49].T = model.TurnIdle
	evs = append(evs, model.Event{Seq: 51, T: model.Input}, model.Event{Seq: 52, T: model.TurnIdle}) // a tiny one

	if w := Tail(evs, 40, 20); len(w.Events) != 20 || w.Events[0].Seq != 33 || !w.More {
		t.Errorf("Tail: %d events from %d, more=%v", len(w.Events), w.Events[0].Seq, w.More)
	}
	// the mirror: a tiny turn then a long one
	rev := append([]model.Event{{Seq: 0, T: model.TurnIdle}}, evs[:50]...)
	if w := After(rev, 0, 40, 20); len(w.Events) != 20 || w.Events[19].Seq != 20 || !w.More {
		t.Errorf("After: %d events to %d, more=%v", len(w.Events), w.Events[len(w.Events)-1].Seq, w.More)
	}
	// but a window already half full is not padded with a partial turn
	if w := Tail(evs, 40, 3); len(w.Events) != 2 || !w.More {
		t.Errorf("Tail 3: %d events", len(w.Events))
	}
}

// TestLanesSummariseFromOutsideTheWindow checks a window holding only a
// lane's later events still learns the lane's name, and its end.
func TestLanesSummariseFromOutsideTheWindow(t *testing.T) {
	all := []model.Event{
		{Seq: 1, T: model.AgentStart, At: "2026-09-06T10:00:00Z", Data: map[string]any{"id": "L1", "type": "codex", "description": "gcc_resume"}},
		{Seq: 2, T: model.ToolCall, Lane: "L1"},
		{Seq: 3, T: model.AgentProgress, Data: map[string]any{"id": "L1", "last": "built", "type": "codex"}, Lane: "L1"},
		{Seq: 4, T: model.ToolCall, Lane: "L1"},
		{Seq: 5, T: model.AgentEnd, At: "2026-09-06T10:05:00Z", Data: map[string]any{"id": "L1", "status": "completed"}},
		{Seq: 6, T: model.Input},
	}
	// a window of just the fourth event: the lane is running with a last word
	got := Lanes(all, all[3:4])
	if li := got["L1"]; li.Description != "gcc_resume" || li.Type != "codex" || li.Status != "running" || li.Last != "built" || li.StartedAt == "" {
		t.Errorf("mid-lane window: %+v", li)
	}
	// a window through the end: finished, last word cleared
	got = Lanes(all, all[3:6])
	if li := got["L1"]; li.Status != "completed" || li.EndedAt == "" || li.Last != "" {
		t.Errorf("window past the end: %+v", li)
	}
	if Lanes(all, all[5:6]) != nil {
		t.Error("a window with no lane events summarises nothing")
	}
}
