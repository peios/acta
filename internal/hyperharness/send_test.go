package hyperharness

import (
	"acta2/internal/harnesspipe"
	"acta2/internal/threads"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSendCorrelationRetryAndFailureIsolation(t *testing.T) {
	t.Setenv("ACTA_CODEX_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
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
	executable, _ := os.Executable()
	c.spawnSpec = func(d threads.Descriptor) (harnesspipe.Spec, error) {
		return harnesspipe.Spec{ThreadID: d.ID, RunID: d.RunID, CWD: d.CWD, Executable: executable, Args: []string{"-test.run=^TestCodexLifecycleHelper$"}}, nil
	}
	id := uuid.NewString()
	if err = c.Control(threads.Control{ID: id, ThreadID: id, Action: "start", Provider: "codex", CWD: root}); err != nil {
		t.Fatal(err)
	}
	wait := func(command string) threads.Result {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
		defer cancel()
		for {
			for _, r := range c.Results() {
				if r.ID == command && !controllerPending(c, id) {
					return r
				}
			}
			select {
			case <-ctx.Done():
				t.Fatal("missing result", command)
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	wait(id)
	inventory, _ := c.Inventory(t.Context())
	run := inventory[0].RunID
	processes, _ := pipe.Processes(t.Context())
	pid := processes[0].PID
	for _, text := range []string{"hello", "hello", "reject", "uncertain"} {
		q := threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "send", RunID: run, Text: text}
		if err = c.Control(q); err != nil {
			t.Fatal(err)
		}
		result := wait(q.ID)
		want := "accepted"
		if text == "reject" {
			want = "rejected"
		}
		if text == "uncertain" {
			want = "uncertain"
		}
		if result.Outcome != want {
			t.Fatalf("%s: %+v", text, result)
		}
		if err = c.Control(q); err != nil {
			t.Fatal("redelivery", err)
		}
		changed := q
		changed.Text += "other"
		if c.Control(changed) == nil {
			t.Fatal("accepted changed payload")
		}
		if text == "hello" {
			deadline := time.Now().Add(time.Second)
			found := false
			for time.Now().Before(deadline) && !found {
				frames, err := c.Frames(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range frames {
					if item.Kind == "message/user" {
						var data map[string]any
						json.Unmarshal(item.Data, &data)
						if data["submission_id"] == q.ID {
							found = true
						}
					}
				}
				if len(frames) > 0 {
					if err := c.Acknowledge(id, frames[len(frames)-1].Sequence); err != nil {
						t.Fatal(err)
					}
				}
				if !found {
					time.Sleep(5 * time.Millisecond)
				}
			}
			if !found {
				t.Fatal("no correlated provider echo")
			}
		}
	}
	stale := threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "send", RunID: uuid.NewString(), Text: "stale"}
	if c.Control(stale) == nil {
		t.Fatal("stale run accepted")
	}
	processes, _ = pipe.Processes(t.Context())
	if len(processes) != 1 || processes[0].PID != pid {
		t.Fatal("send failure restarted provider")
	}
}

func TestSendRecoveryAfterProviderWrite(t *testing.T) {
	t.Setenv("ACTA_CODEX_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	root := t.TempDir()
	pipe, err := harnesspipe.Open(filepath.Join(root, "pipe"))
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()
	submission := uuid.NewString()
	wrapped := &lostStartReply{API: pipe, accepted: make(chan struct{}), method: "turn/start/" + submission}
	c, err := NewController(t.Context(), filepath.Join(root, "controller"), wrapped)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { c.Close() }()
	executable, _ := os.Executable()
	c.spawnSpec = func(d threads.Descriptor) (harnesspipe.Spec, error) {
		return harnesspipe.Spec{ThreadID: d.ID, RunID: d.RunID, CWD: d.CWD, Executable: executable, Args: []string{"-test.run=^TestCodexLifecycleHelper$"}}, nil
	}
	id := uuid.NewString()
	if err = c.Control(threads.Control{ID: id, ThreadID: id, Action: "start", Provider: "codex", CWD: root}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for controllerPending(c, id) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	inventory, err := c.Inventory(t.Context())
	if err != nil || len(inventory) != 1 {
		t.Fatal(inventory, err)
	}
	q := threads.Control{ID: submission, ThreadID: id, RunID: inventory[0].RunID, Action: "send", Text: "survive restart"}
	if err = c.Control(q); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wrapped.accepted:
	case <-time.After(4 * time.Second):
		t.Fatal("write did not reach provider")
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = NewController(t.Context(), filepath.Join(root, "controller"), pipe)
	if err != nil {
		t.Fatal(err)
	}
	c.Recover()
	deadline = time.Now().Add(4 * time.Second)
	accepted := false
	for time.Now().Before(deadline) && !accepted {
		for _, r := range c.Results() {
			if r.ID == submission && r.Outcome == "accepted" {
				accepted = true
			}
		}
		if !accepted {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if !accepted {
		t.Fatal("did not recover accepted command")
	}
	frames, err := pipe.Read(t.Context(), harnesspipe.ReadRequest{ThreadID: id})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, f := range frames {
		var response struct{ ID string }
		json.Unmarshal(f.Data, &response)
		if response.ID == q.RunID+"/turn/start/"+submission {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("provider received %d copies of the send", count)
	}
}
