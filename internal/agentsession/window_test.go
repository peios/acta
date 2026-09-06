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
	if w := Tail(evs, 2); !eq(seqs(w), 6, 7, 8, 9) || !w.More {
		t.Errorf("tail 2: %v more=%v", seqs(w), w.More)
	}
	if w := Tail(evs, 10); !eq(seqs(w), 1, 2, 3, 4, 5, 6, 7, 8, 9) || w.More {
		t.Errorf("tail 10: %v more=%v", seqs(w), w.More)
	}
	if w := Before(evs, 6, 1); !eq(seqs(w), 4, 5) || !w.More {
		t.Errorf("before 6: %v more=%v", seqs(w), w.More)
	}
	if w := Before(evs, 4, 5); !eq(seqs(w), 1, 2, 3) || w.More {
		t.Errorf("before 4: %v more=%v", seqs(w), w.More)
	}
	if w := After(evs, 3, 1); !eq(seqs(w), 4, 5) || !w.More {
		t.Errorf("after 3: %v more=%v", seqs(w), w.More)
	}
	if w := After(evs, 5, 5); !eq(seqs(w), 6, 7, 8, 9) || w.More {
		t.Errorf("after 5: %v more=%v", seqs(w), w.More)
	}
	if w := After(evs, 9, 5); len(w.Events) != 0 || w.More {
		t.Errorf("after the end: %v more=%v", seqs(w), w.More)
	}
}
