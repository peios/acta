package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestClaudeLocalCommandSubmission(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-local-command.json")
	if err != nil {
		t.Fatal(err)
	}
	var inputs []object
	if err = json.Unmarshal(raw, &inputs); err != nil {
		t.Fatal(err)
	}
	const id = "db799357-e3f3-4f43-9960-df8e8e3bd330"
	const command = "0e33f0fc-455b-4cba-bcb7-14dff21e3f1b"
	for _, scenario := range []string{"normal", "old-run", "internal-command", "result-only"} {
		t.Run(scenario, func(t *testing.T) {
			s := State{NativeID: id, Submissions: map[string]string{command: id}, SubmissionTexts: map[string]string{command: "/status"}}
			if scenario == "old-run" {
				s.Submissions[command] = "old"
			}
			if scenario == "internal-command" {
				s.SubmissionTexts = nil
			}
			users := 0
			for i, m := range inputs {
				if scenario == "result-only" && m["type"] != "result" {
					continue
				}
				checkpoint, _ := json.Marshal(s)
				s, err = DecodeState(checkpoint)
				if err != nil {
					t.Fatal(err)
				}
				text, _ := json.Marshal(m)
				next, frames, e := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: int64(i + 1), ReceivedAt: time.Now()}, "stdout", string(text))
				if e != nil {
					t.Fatal(e)
				}
				for _, f := range frames {
					if DataError(f) {
						t.Fatalf("mapping failure: %s", f.Data)
					}
					if f.Kind == "message/user" {
						users++
						var d object
						_ = json.Unmarshal(f.Data, &d)
						if d["submission_id"] != command || d["state"] != "completed" || obj(list(d["content"])[0])["text"] != "/status" {
							t.Fatalf("wrong acknowledgement: %s", f.Data)
						}
					}
				}
				s = next
			}
			want := 1
			if scenario == "old-run" || scenario == "internal-command" {
				want = 0
			}
			if users != want {
				t.Fatalf("users=%d want=%d", users, want)
			}
		})
	}
}
