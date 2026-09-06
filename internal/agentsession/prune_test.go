package agentsession

import (
	"context"
	"strings"
	"testing"

	"github.com/peios/acta/internal/agentsession/model"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

// TestPruneEstimatesThenPrune checks the estimate for a category matches
// what pruning it saves, that frames are rewritten in place (same seqs, same
// count), and that unrelated categories report nothing.
func TestPruneEstimatesThenPrune(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	u, _ := ms.CreateUser(ctx, store.NewUser{Username: "a", Display: "A", PasswordHash: "x"})
	svc := New(ms)
	as, err := svc.CreateWithID(ctx, "s1", u.ID, "claude-code", "/tmp", "t", nil)
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("z", 2000)
	frames := []string{
		`{"type":"user","uuid":"u1","message":{"content":"hello"}}`,
		`{"type":"assistant","uuid":"a1","message":{"content":[{"type":"thinking","thinking":"` + big + `"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","uuid":"u2","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"` + big + `"}]}}`,
	}
	for _, f := range frames {
		if _, err := svc.Append(ctx, as.ID, "transcript", []byte(f)); err != nil {
			t.Fatal(err)
		}
	}
	ests, total, err := svc.PruneEstimates(ctx, as.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]PruneEstimate{}
	for _, e := range ests {
		byID[e.ID] = e
	}
	if total < 4000 || byID[model.PruneToolOutput].Frames != 1 || byID[model.PruneThinking].Frames != 1 || byID[model.PruneToolInput].Frames != 0 || byID[model.PruneImages].Frames != 0 {
		t.Fatalf("estimates: total=%d %+v", total, byID)
	}
	want := byID[model.PruneToolOutput].Bytes
	res, err := svc.Prune(ctx, as.ID, u.ID, map[string]bool{model.PruneToolOutput: true})
	if err != nil || res.Frames != 1 || res.Saved != want {
		t.Fatalf("prune: %+v %v (want saved %d)", res, err, want)
	}
	evs, _ := svc.Events(ctx, as.ID, 0, 0)
	if len(evs) != 3 || evs[2].Seq != 3 || !strings.Contains(string(evs[2].Payload), "[output pruned") || strings.Contains(string(evs[2].Payload), big) {
		t.Errorf("frames after prune: %d, third = %s", len(evs), evs[2].Payload)
	}
	if strings.Count(string(evs[1].Payload), big) != 1 {
		t.Error("thinking should be untouched when only tool output was chosen")
	}
	if _, err := svc.Prune(ctx, as.ID, u.ID, nil); err != ErrNothingToPrune {
		t.Errorf("empty choice: %v", err)
	}
}
