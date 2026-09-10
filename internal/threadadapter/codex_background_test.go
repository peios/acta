package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestCodexBackgroundLifecycle(t *testing.T) {
	b, err := os.ReadFile("testdata/codex-background.json")
	if err != nil {
		t.Fatal(err)
	}
	var captures []object
	if err := json.Unmarshal(b, &captures); err != nil {
		t.Fatal(err)
	}
	const id = "9d3ba50b-5142-480c-8533-81d9bb463b52"
	const native = "01a0823d-29fc-79c1-9b0c-d3c577ec57f3"
	const tool = "call_kdwaR1oo2qR7kJVnSZWbftCS"
	const turn = "01a082c5-2603-7833-97a0-5f56a841c62f"
	for _, process := range []bool{true, false} {
		for _, exitCode := range []int{0, 7} {
			s := State{RunID: id, NativeID: native}
			seq := int64(0)
			mapFrame := func(m object) []threads.Frame {
				t.Helper()
				checkpoint, _ := json.Marshal(s)
				var restored State
				if err := json.Unmarshal(checkpoint, &restored); err != nil {
					t.Fatal(err)
				}
				b, _ := json.Marshal(m)
				seq++
				next, frames, err := Map(restored, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "codex", Sequence: seq, ReceivedAt: time.Now()}, "stdout", string(b))
				if err != nil {
					t.Fatal(err)
				}
				for _, f := range frames {
					if f.Kind == "debug/unknown" || DataError(f) {
						t.Fatalf("unmapped: %s", f.Data)
					}
				}
				s = next
				return frames
			}
			item := obj(obj(captures[0]["params"])["item"])
			item["processId"] = nil
			if process {
				item["processId"] = "68601"
			}
			mapFrame(captures[0])
			end := mapFrame(captures[1])
			if process {
				if len(end) != 3 || end[1].Kind != "tool/call" || end[2].Kind != "turn/completed" || s.Tools[tool].Status != "background" || s.Tools[tool].Completed != nil {
					t.Fatalf("background must precede turn closure: %+v", end)
				}
				mapFrame(object{"method": "item/commandExecution/outputDelta", "params": object{"threadId": native, "turnId": turn, "itemId": tool, "delta": "late output"}})
			} else if len(end) != 2 || s.Tools[tool].BackgroundID != "" {
				t.Fatal("inferred background without a process")
			}
			if repeat := mapFrame(captures[1]); len(repeat) != 1 {
				t.Fatal("duplicate turn emitted another background snapshot")
			}
			obj(obj(captures[2]["params"])["item"])["exitCode"] = exitCode
			done := mapFrame(captures[2])
			want := "completed"
			if exitCode != 0 {
				want = "failed"
			}
			if s.Tools[tool].Status != want {
				t.Fatal("wrong completion", s.Tools[tool])
			}
			if process {
				if len(done) != 3 || done[2].Kind != "tool/notification" {
					t.Fatal("missing notice", done)
				}
				var d object
				_ = json.Unmarshal(done[2].Data, &d)
				if d["status"] != want || d["context_entry"] != false {
					t.Fatal(d)
				}
			} else if len(done) != 2 {
				t.Fatal("foreground completion gained a notice")
			}
			if repeat := mapFrame(captures[2]); len(repeat) != 1 {
				t.Fatal("duplicate completion notice")
			}
		}
	}
}
