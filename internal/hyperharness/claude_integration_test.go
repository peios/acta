package hyperharness

import (
	"acta/internal/harnesspipe"
	"acta/internal/threads"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Explicit opt-in: uses a locally authenticated Claude installation and two
// small Haiku turns. No credentials or native account data enter test output.
func TestClaudeNativeLifecycle(t *testing.T) {
	if os.Getenv("ACTA_CLAUDE_INTEGRATION") != "1" {
		t.Skip("requires local Claude and explicit model-usage opt-in")
	}
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
		spec, err := claudeSpec(d)
		spec.Args = append(spec.Args, "--model", "haiku", "--tools", "", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`)
		return spec, err
	}
	id := uuid.NewString()
	runID := id
	run := func(q threads.Control) threads.Result {
		t.Helper()
		if err := c.Control(q); err != nil {
			t.Fatal(err)
		}
		for end := time.Now().Add(40 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			for _, r := range c.Results() {
				if r.ID == q.ID && !controllerPending(c, id) {
					if r.Error != "" {
						t.Fatal(q.Action, r.Error)
					}
					return r
				}
			}
		}
		t.Fatal("native command timed out", q.Action)
		return threads.Result{}
	}
	run(threads.Control{ID: id, ThreadID: id, Action: "start", Provider: "claude", CWD: root})
	t.Log("Native start acknowledged")
	waitTurn := func(turn string) {
		t.Helper()
		for end := time.Now().Add(90 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
			frames, err := c.Frames(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			done := false
			for _, f := range frames {
				if f.Kind == "debug/local" {
					var data struct {
						Error any `json:"processing_error"`
					}
					_ = json.Unmarshal(f.Data, &data)
					if data.Error != nil {
						t.Fatal("normalization failure", data.Error)
					}
				}
				if f.Kind == "turn/completed" {
					var data struct {
						Turn    string `json:"turn_id"`
						Outcome string `json:"outcome"`
					}
					_ = json.Unmarshal(f.Data, &data)
					if data.Turn == turn {
						if data.Outcome != "completed" {
							t.Fatal("turn failed", data.Outcome)
						}
						done = true
					}
				}
			}
			if len(frames) > 0 {
				if err = c.Acknowledge(id, frames[len(frames)-1].Sequence); err != nil {
					t.Fatal(err)
				}
			}
			if done {
				return
			}
		}
		t.Fatal("native turn timed out")
	}
	assertEmptyThread(t, c, id)
	initialFrames, _ := c.Frames(t.Context(), id)
	if len(initialFrames) > 0 {
		if err := c.Acknowledge(id, initialFrames[len(initialFrames)-1].Sequence); err != nil {
			t.Fatal(err)
		}
	}
	initialSend := uuid.NewString()
	run(threads.Control{ID: initialSend, ThreadID: id, RunID: id, Action: "send", Text: "Reply OK only. Do not use tools."})
	waitTurn(initialSend)
	t.Log("Initial text turn completed")
	committed := false
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		inventory, err := c.Inventory(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(inventory) == 1 && inventory[0].Committed {
			committed = true
			break
		}
	}
	if !committed {
		t.Fatal("saved user history did not establish commitment")
	}
	t.Log("Stored user history verified")
	catalog := run(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: runID, Action: "models"})
	if len(catalog.Models) == 0 {
		t.Fatal("no native catalogue")
	}
	settings := run(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: runID, Action: "configure", Settings: threads.ModelSettings{Model: "haiku"}})
	if settings.Settings == nil || settings.Settings.Effort != "" {
		t.Fatal("no-effort model readback failed")
	}
	t.Log("Model catalogue and effective settings verified")
	run(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: runID, Action: "kill"})
	resume := uuid.NewString()
	run(threads.Control{ID: resume, ThreadID: id, RunID: runID, Action: "resume"})
	runID = resume
	t.Log("Killed provider resumed from disk")
	sendID := uuid.NewString()
	run(threads.Control{ID: sendID, ThreadID: id, RunID: runID, Action: "send", Text: "Reply only: Resume works. Do not use tools."})
	waitTurn(sendID)
	t.Log("Message after resume completed")
	run(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: runID, Action: "kill"})
}
