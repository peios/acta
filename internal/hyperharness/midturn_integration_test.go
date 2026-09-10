package hyperharness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"acta2/internal/harnesspipe"
	"acta2/internal/threads"
	"github.com/google/uuid"
)

// Opt-in, isolated native sessions: no user workspace or existing provider is
// touched. Uses Haiku and a selectable small Codex model, with short text only.
func TestNativeMidTurnInput(t *testing.T) {
	if os.Getenv("ACTA_MIDTURN_INTEGRATION") != "1" {
		t.Skip("requires authenticated local providers and model usage")
	}
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			pipe, err := harnesspipe.Open(filepath.Join(root, "pipe"))
			if err != nil {
				t.Fatal(err)
			}
			defer pipe.Close()
			c, err := NewController(t.Context(), filepath.Join(root, "controller"), pipe)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.spawnSpec = func(d threads.Descriptor) (harnesspipe.Spec, error) {
				if provider == "codex" {
					return codexSpec(d)
				}
				spec, err := claudeSpec(d)
				spec.Args = append(spec.Args, "--model", "haiku", "--tools", "", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--settings", `{"disableAllHooks":true}`)
				return spec, err
			}
			id := uuid.NewString()
			run := func(q threads.Control) threads.Result {
				t.Helper()
				if err := c.Control(q); err != nil {
					t.Fatal(err)
				}
				for end := time.Now().Add(70 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
					for _, r := range c.Results() {
						if r.ID == q.ID && !controllerPending(c, id) {
							if r.Error != "" {
								t.Fatal(q.Action, r.Error)
							}
							return r
						}
					}
				}
				t.Fatal("command timed out", q.Action)
				return threads.Result{}
			}
			run(threads.Control{ID: id, ThreadID: id, RunID: id, Action: "start", Provider: provider, CWD: root})
			if provider == "codex" {
				catalogue := run(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "models"})
				var model threads.ModelOption
				for _, part := range []string{"nano", "mini", "luna", "spark"} {
					for _, m := range catalogue.Models {
						if model.ID == "" && strings.Contains(m.ID, part) {
							model = m
						}
					}
				}
				if model.ID == "" {
					t.Fatal("no small Codex model available")
				}
				effort := model.DefaultEffort
				for _, e := range model.Efforts {
					if e.ID == "low" {
						effort = e.ID
					}
				}
				t.Log("Using", model.ID)
				run(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "configure", Settings: threads.ModelSettings{Model: model.ID, Effort: effort}})
			}
			first := uuid.NewString()
			run(threads.Control{ID: first, ThreadID: id, RunID: id, Action: "send", Text: "Without tools, write 150 numbered lines, each saying 'This is a short counting test'. Start immediately."})
			var seen []threads.Frame
			read := func() {
				f, err := c.Frames(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				seen = append(seen, f...)
				if len(f) > 0 {
					if err = c.Acknowledge(id, f[len(f)-1].Sequence); err != nil {
						t.Fatal(err)
					}
				}
			}
			started := false
			for end := time.Now().Add(45 * time.Second); time.Now().Before(end) && !started; time.Sleep(20 * time.Millisecond) {
				read()
				for _, f := range seen {
					if f.Kind == "message/assistant/delta" {
						started = true
					}
				}
			}
			if !started {
				t.Fatal("no streaming response")
			}
			second := uuid.NewString()
			run(threads.Control{ID: second, ThreadID: id, RunID: id, Action: "send", Text: "Stop counting. Reply only ACTA_MIDTURN_OK."})
			done := false
			for end := time.Now().Add(90 * time.Second); time.Now().Before(end) && !done; time.Sleep(100 * time.Millisecond) {
				read()
				idle := false
				answer := false
				echo := false
				for _, f := range seen {
					var d map[string]any
					_ = json.Unmarshal(f.Data, &d)
					if f.Kind == "thread/status" {
						idle = d["status"] == "idle"
					}
					if f.Kind == "message/assistant" && strings.Contains(string(f.Data), "ACTA_MIDTURN_OK") {
						answer = true
					}
					if f.Kind == "message/user" && d["submission_id"] == second {
						echo = true
					}
				}
				done = idle && answer && echo
			}
			if dir := os.Getenv("ACTA_MIDTURN_CAPTURE"); dir != "" {
				os.MkdirAll(dir, 0700)
				raw, _ := json.MarshalIndent(seen, "", "  ")
				os.WriteFile(filepath.Join(dir, provider+"-frames.json"), raw, 0600)
				rawFrames, _ := pipe.Read(t.Context(), harnesspipe.ReadRequest{ThreadID: id})
				raw, _ = json.MarshalIndent(rawFrames, "", "  ")
				os.WriteFile(filepath.Join(dir, provider+"-raw.json"), raw, 0600)
			}
			if !done {
				t.Fatal("mid-turn message was not correlated and answered before idle")
			}
			t.Log("Mid-turn submission acknowledged, echoed and answered")
		})
	}
}
