package threadadapter

import (
	"acta/internal/threads"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"testing"
	"time"
)

func TestAutomaticReviewCapturedLifecycle(t *testing.T) {
	raw, err := os.ReadFile("testdata/approval-review.json")
	if err != nil {
		t.Fatal(err)
	}
	var captures []json.RawMessage
	if err = json.Unmarshal(raw, &captures); err != nil {
		t.Fatal(err)
	}
	run := uuid.NewString()
	state := State{NativeID: "native"}
	for i, capture := range captures {
		p := threads.ProviderFrame{Provider: "codex", ThreadID: run, RunID: run, Sequence: int64(i + 1), ReceivedAt: time.Now()}
		next, bundle, err := Map(state, p, "stdout", string(capture))
		if err != nil || len(bundle) != 2 || bundle[0].Kind != "debug/resolved" || bundle[1].Kind != "approval/review" {
			t.Fatal(bundle, err)
		}
		state = next
		var data object
		_ = json.Unmarshal(bundle[1].Data, &data)
		want := "in_progress"
		if i == 1 {
			want = "approved"
		}
		if data["status"] != want || data["tool_id"] != "tool" || data["review_id"] != "review" {
			t.Fatal(data)
		}
	}
	// Reviewed terminal states retain their distinction; unknown states, sources
	// and foreign thread identities must remain visible as unknown evidence.
	var end object
	_ = json.Unmarshal(captures[1], &end)
	for _, status := range []string{"denied", "timedOut", "aborted", "future"} {
		obj(obj(end["params"])["review"])["status"] = status
		b, _ := json.Marshal(end)
		_, bundle, err := Map(state, threads.ProviderFrame{Provider: "codex", ThreadID: run, RunID: run, Sequence: 3, ReceivedAt: time.Now()}, "stdout", string(b))
		if err != nil {
			t.Fatal(err)
		}
		if status == "future" {
			if len(bundle) != 1 || bundle[0].Kind != "debug/unknown" {
				t.Fatal(bundle)
			}
		} else if len(bundle) != 2 {
			t.Fatal(bundle)
		}
	}
	for _, field := range []string{"threadId", "decisionSource"} {
		var m object
		_ = json.Unmarshal(captures[1], &m)
		obj(m["params"])[field] = "foreign"
		b, _ := json.Marshal(m)
		_, bundle, err := Map(state, threads.ProviderFrame{Provider: "codex", ThreadID: run, RunID: run, Sequence: 4, ReceivedAt: time.Now()}, "stdout", string(b))
		if err != nil || len(bundle) != 1 || bundle[0].Kind != "debug/unknown" {
			t.Fatal(bundle, err)
		}
	}
}
