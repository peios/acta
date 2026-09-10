package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"testing"
	"time"
)

func TestHookProvidersPreserveResponses(t *testing.T) {
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			start := object{"type": "system", "subtype": "hook_started", "session_id": id, "hook_id": "hook", "hook_name": "Load notes", "hook_event": "SessionStart"}
			done := object{"type": "system", "subtype": "hook_response", "session_id": id, "hook_id": "hook", "hook_name": "Load notes", "hook_event": "SessionStart", "outcome": "success", "stdout": "  exact\noutput\n", "stderr": "warning\n", "output": "combined", "exit_code": 0}
			if provider == "codex" {
				start = object{"method": "hook/started", "params": object{"threadId": id, "turnId": nil, "run": object{"id": "hook", "eventName": "sessionStart"}}}
				done = object{"method": "hook/completed", "params": object{"threadId": id, "turnId": nil, "run": object{"id": "hook", "eventName": "sessionStart", "status": "completed", "entries": []any{object{"kind": "context", "text": "  exact\noutput\n"}}, "durationMs": 12}}}
			}
			state := State{NativeID: id}
			for i, input := range []object{start, done} {
				raw, _ := json.Marshal(input)
				p := threads.ProviderFrame{ThreadID: id, RunID: id, Provider: provider, Sequence: int64(i + 1), ReceivedAt: time.Now()}
				next, frames, err := Map(state, p, "stdout", string(raw))
				state = next
				if err != nil || len(frames) != 2 || frames[0].Kind != "debug/resolved" {
					t.Fatalf("mapping: %v %+v", err, frames)
				}
				want := []string{"hook/started", "hook/completed"}[i]
				if frames[1].Kind != want {
					t.Fatalf("got %s", frames[1].Kind)
				}
				if i == 1 {
					var d object
					_ = json.Unmarshal(frames[1].Data, &d)
					if provider == "claude" && (d["stdout"] != done["stdout"] || d["stderr"] != done["stderr"] || d["output"] != done["output"]) {
						t.Fatal("response modified")
					}
					if provider == "codex" && obj(list(d["entries"])[0])["text"] != "  exact\noutput\n" {
						t.Fatal("entry modified")
					}
				}
			}
			// Missing identity must not become a hook UI item.
			invalid := `{"type":"system","subtype":"hook_response","session_id":"wrong"}`
			if provider == "codex" {
				invalid = `{"method":"hook/started","params":{"run":{"id":"x","eventName":"stop"}}}`
			}
			_, frames, err := Map(state, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: provider, Sequence: 3, ReceivedAt: time.Now()}, "stdout", invalid)
			if err != nil || len(frames) != 1 || frames[0].Kind != "debug/unknown" {
				t.Fatal("lost identity protection", err, frames)
			}
		})
	}
}
