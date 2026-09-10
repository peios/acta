package hyperharness

import (
	"acta2/internal/harnesspipe"
	"acta2/internal/threads"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestModelCatalogueAndSettingsControl(t *testing.T) {
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
	run := func(q threads.Control) threads.Result {
		t.Helper()
		if err := c.Control(q); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			for _, r := range c.Results() {
				if r.ID == q.ID && !controllerPending(c, id) {
					return r
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("missing result", q)
		return threads.Result{}
	}
	run(threads.Control{ID: id, ThreadID: id, Provider: "codex", CWD: root, Action: "start"})
	catalogue := run(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "models"})
	if catalogue.Outcome != "accepted" || len(catalogue.Models) != 2 || !catalogue.Models[0].FastMode || catalogue.Models[1].FastMode {
		t.Fatal(catalogue)
	}
	wanted := threads.ModelSettings{Model: "test-model", Effort: "high", FastMode: true}
	q := threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "configure", Settings: wanted}
	if result := run(q); result.Outcome != "accepted" {
		t.Fatal(result)
	}
	if err := c.Control(q); err != nil {
		t.Fatal("duplicate", err)
	}
	c.mu.Lock()
	saved := *c.records[id].Configuration
	c.records[id].Commands[q.ID] = threads.Result{ID: q.ID, ThreadID: id, Outcome: "uncertain"} // Late captured reply must repair an expired waiter.
	c.mu.Unlock()
	if saved != wanted {
		t.Fatal(saved)
	}
	frames, err := c.Frames(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, frame := range frames {
		if frame.Kind == "thread/configuration" {
			var settings threads.ModelSettings
			json.Unmarshal(frame.Data, &settings)
			if settings == wanted {
				found = true
			}
		}
	}
	c.mu.Lock()
	accepted := c.records[id].Commands[q.ID].Outcome
	c.mu.Unlock()
	if accepted != "accepted" {
		t.Fatal("late acknowledgement was not reconciled", accepted)
	}
	if !found {
		t.Fatal("configuration notification was not mapped")
	}
	for _, settings := range []threads.ModelSettings{{Model: "missing", Effort: "low"}, {Model: "test-model", Effort: "ultra"}, {Model: "small-model", Effort: "low", FastMode: true}} {
		result := run(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "configure", Settings: settings})
		if result.Outcome != "rejected" {
			t.Fatal("accepted unsupported combination", result)
		}
	}
	raw, err := pipe.Read(t.Context(), harnesspipe.ReadRequest{ThreadID: id})
	if err != nil {
		t.Fatal(err)
	}
	changes := 0
	catalogueReads := 0
	for _, frame := range raw {
		var response struct{ ID string }
		json.Unmarshal(frame.Data, &response)
		if strings.Contains(response.ID, "/model/list/") {
			catalogueReads++
		}
		if response.ID == id+"/thread/settings/update/"+q.ID {
			changes++
		}
	}
	if catalogueReads != 1 {
		t.Fatal("model changes reloaded cached catalogue", catalogueReads)
	}
	if changes != 1 {
		t.Fatal("settings update repeated", changes)
	}
	c.mu.Lock()
	saved = *c.records[id].Configuration
	c.mu.Unlock()
	if saved != wanted {
		t.Fatal("rejection replaced settings", saved)
	}
	inventory, err := c.Inventory(t.Context())
	if err != nil || inventory[0].State != "running" {
		t.Fatal("settings killed provider", inventory, err)
	}
	mustAck := frames[len(frames)-1].Sequence
	if err := c.Acknowledge(id, mustAck); err != nil {
		t.Fatal(err)
	}
	run(threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "kill"})
	resume := threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "resume"}
	if result := run(resume); result.Error != "" {
		t.Fatal(result)
	}
	raw, err = pipe.Read(t.Context(), harnesspipe.ReadRequest{ThreadID: id})
	if err != nil {
		t.Fatal(err)
	}
	restored := false
	for _, frame := range raw {
		if frame.RunID != resume.ID {
			continue
		}
		var notice struct {
			Method string `json:"method"`
			Params struct {
				Settings struct {
					Model  string `json:"model"`
					Effort string `json:"effort"`
					Tier   string `json:"serviceTier"`
				} `json:"threadSettings"`
			} `json:"params"`
		}
		json.Unmarshal(frame.Data, &notice)
		if notice.Method == "thread/settings/updated" && notice.Params.Settings.Model == wanted.Model && notice.Params.Settings.Effort == wanted.Effort && notice.Params.Settings.Tier == "priority" {
			restored = true
		}
	}
	reloaded := run(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: resume.ID, Action: "models"})
	if reloaded.Outcome != "accepted" {
		t.Fatal(reloaded)
	}
	raw, err = pipe.Read(t.Context(), harnesspipe.ReadRequest{ThreadID: id})
	if err != nil {
		t.Fatal(err)
	}
	fetchedForRun := false
	for _, frame := range raw {
		if frame.RunID == resume.ID && strings.Contains(frame.Text(), "/model/list/") {
			fetchedForRun = true
		}
	}
	if !fetchedForRun {
		t.Fatal("resumed provider reused prior run catalogue")
	}
	if !restored {
		t.Fatal("resume did not restore the confirmed model, effort and fast mode")
	}
}
