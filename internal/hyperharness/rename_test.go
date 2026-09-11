package hyperharness

import (
	"acta/internal/harnesspipe"
	"acta/internal/threads"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRenameSurvivesRestartAndStaleReplay(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			pipe, err := harnesspipe.Open(filepath.Join(root, "pipe"))
			if err != nil {
				t.Fatal(err)
			}
			defer pipe.Close()
			open := func() *Controller {
				c, err := NewController(t.Context(), filepath.Join(root, "threads"), pipe)
				if err != nil {
					t.Fatal(err)
				}
				return c
			}
			c := open()
			defer func() { c.Close() }()
			id := uuid.NewString()
			r := &localThread{Thread: threads.Descriptor{ID: id, Provider: provider, CWD: root, State: "error", Error: "prior provider error", Revision: 1}, Discovered: true, Commands: map[string]threads.Result{}}
			c.records[id] = r
			if err := c.save(r); err != nil {
				t.Fatal(err)
			}
			wait := func(name string) {
				t.Helper()
				until := time.Now().Add(3 * time.Second)
				for time.Now().Before(until) {
					list, err := c.Inventory(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					if len(list) == 1 && list[0].Name == name && !controllerPending(c, id) {
						if list[0].State != "error" || list[0].Error != "prior provider error" {
							t.Fatal("rename changed lifecycle")
						}
						return
					}
					time.Sleep(time.Millisecond)
				}
				t.Fatal("rename did not finish")
			}
			first := threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "rename", Name: "First name"}
			if err := c.Control(first); err != nil {
				t.Fatal(err)
			}
			wait(first.Name)
			second := first
			second.ID, second.Name = uuid.NewString(), "Second name"
			if err := c.Control(second); err != nil {
				t.Fatal(err)
			}
			wait(second.Name)
			c.Close()
			c = open()
			c.Recover()
			if err := c.Control(first); err != nil {
				t.Fatal(err)
			}
			wait(second.Name)
			first.Name = "Changed replay"
			if c.Control(first) == nil {
				t.Fatal("accepted conflicting command replay")
			}
			processes, err := pipe.Processes(t.Context())
			if err != nil || len(processes) != 0 {
				t.Fatal("rename started a provider", processes, err)
			}
		})
	}
}

func TestNativeRenameAndDeferredResume(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			id := uuid.NewString()
			t.Setenv("ACTA_CODEX_HELPER", "1")
			t.Setenv("ACTA_CLAUDE_HELPER", "1")
			t.Setenv("ACTA_CLAUDE_SESSION", id)
			t.Setenv("ACTA_CODEX_ROLLOUT", filepath.Join(root, "rollout"))
			t.Setenv("GORACE", "atexit_sleep_ms=0")
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
			exe, _ := os.Executable()
			c.spawnSpec = func(d threads.Descriptor) (harnesspipe.Spec, error) {
				helper := "TestCodexLifecycleHelper"
				if provider == "claude" {
					helper = "TestClaudeLifecycleHelper"
				}
				return harnesspipe.Spec{ThreadID: d.ID, RunID: d.RunID, CWD: root, Executable: exe, Args: []string{"-test.run=^" + helper + "$"}}, nil
			}
			run := func(q threads.Control) {
				t.Helper()
				if err := c.Control(q); err != nil {
					t.Fatal(err)
				}
				for until := time.Now().Add(5 * time.Second); time.Now().Before(until); time.Sleep(time.Millisecond) {
					for _, r := range c.Results() {
						if r.ID == q.ID && !controllerPending(c, id) {
							if r.Error != "" && q.Name != "Rejected" {
								t.Fatal(r.Error)
							}
							return
						}
					}
				}
				t.Fatal("command timeout", q.Action)
			}
			run(threads.Control{ID: id, ThreadID: id, Action: "start", Provider: provider, CWD: root})
			run(threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "rename", Name: "Native one"})
			list, err := c.Inventory(t.Context())
			if err != nil || list[0].NameSyncPending {
				t.Fatal("running name not synchronized", list, err)
			}
			run(threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "kill"})
			run(threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "rename", Name: "Native two"})
			list, err = c.Inventory(t.Context())
			if err != nil || !list[0].NameSyncPending {
				t.Fatal("stopped name not pending", list, err)
			}
			run(threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "resume"})
			list, err = c.Inventory(t.Context())
			if err != nil || list[0].Name != "Native two" || list[0].NameSyncPending {
				t.Fatal("resume lost name", list, err)
			}
			captures, err := pipe.Read(t.Context(), harnesspipe.ReadRequest{ThreadID: id})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, f := range captures {
				if strings.Contains(string(f.Data), "Native two") {
					found = true
				}
			}
			if !found {
				t.Fatal("provider never received deferred name")
			}
			run(threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "rename", Name: "Rejected"})
			list, err = c.Inventory(t.Context())
			if err != nil || list[0].State != "running" || list[0].Name != "Rejected" || !list[0].NameSyncPending || !strings.Contains(list[0].NameSyncError, "rename rejected") {
				t.Fatal("native failure lost local name or damaged lifecycle", list, err)
			}
			run(threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "rename", Name: "Recovered"})
			list, err = c.Inventory(t.Context())
			if err != nil || list[0].NameSyncPending || list[0].NameSyncError != "" {
				t.Fatal("native retry failed", list, err)
			}
			run(threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "kill"})
		})
	}
}
