package hyperharness

import (
	"acta2/internal/harnesspipe"
	"acta2/internal/threads"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Opt-in only: publishes one private, harmless HTML artifact and updates it,
// using two Haiku turns. Requires a locally authenticated eligible Claude account.
func TestClaudeNativeArtifact(t *testing.T) {
	if os.Getenv("ACTA_CLAUDE_ARTIFACT_INTEGRATION") != "1" {
		t.Skip("requires explicit private publication and model-usage opt-in")
	}
	root := t.TempDir()
	file := filepath.Join(root, "probe.html")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(file, []byte("<!doctype html><html><body><h1>Acta artifact test</h1><button onclick=\"this.textContent='Works'\">"+text+"</button></body></html>"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("Version one")
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
		if err != nil {
			return spec, err
		}
		// Preserve the production launch settings while disabling user hooks in QA.
		i := slices.Index(spec.Args, "--settings")
		var settings map[string]any
		if err = json.Unmarshal([]byte(spec.Args[i+1]), &settings); err != nil {
			return spec, err
		}
		settings["disableAllHooks"] = true
		raw, _ := json.Marshal(settings)
		spec.Args[i+1] = string(raw)
		spec.Args = append(spec.Args, "--model", "haiku", "--tools", "Artifact,Read", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`)
		return spec, nil
	}
	id := uuid.NewString()
	control := func(q threads.Control) threads.Result {
		t.Helper()
		if err := c.Control(q); err != nil {
			t.Fatal(err)
		}
		for until := time.Now().Add(45 * time.Second); time.Now().Before(until); time.Sleep(50 * time.Millisecond) {
			for _, r := range c.Results() {
				if r.ID == q.ID && !controllerPending(c, id) {
					if r.Error != "" {
						t.Fatal(q.Action, r.Error)
					}
					return r
				}
			}
		}
		t.Fatal("control timeout", q.Action)
		return threads.Result{}
	}
	control(threads.Control{ID: id, ThreadID: id, Action: "start", Provider: "claude", CWD: root})
	control(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "permissions", PermissionMode: "ask"})
	urlPattern := regexp.MustCompile(`https://claude\.ai/code/artifact/[a-zA-Z0-9-]+`)
	url := ""
	for version := 1; version <= 2; version++ {
		if version == 2 {
			write("Version two")
			// Applying model settings must not remove the Artifact opt-in.
			control(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "configure", Settings: threads.ModelSettings{Model: "haiku"}})
		}
		sendID := uuid.NewString()
		prompt := "Use Artifact exactly once to publish " + file + " privately, titled Acta artifact test, with favicon 🧪. Do not share publicly. Then reply with its URL. Do not use other tools."
		if version == 2 {
			prompt = "Use Artifact exactly once to republish " + file + " to " + url + " with the updated file. Keep it private. Then reply with its URL. Do not use other tools."
		}
		control(threads.Control{ID: sendID, ThreadID: id, RunID: id, Action: "send", Text: prompt})
		completed, toolDone, revisionConfirmed := false, false, false
		approvals := map[string]bool{}
		for until := time.Now().Add(90 * time.Second); time.Now().Before(until) && !completed; time.Sleep(100 * time.Millisecond) {
			frames, err := c.Frames(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range frames {
				var d map[string]any
				if err = json.Unmarshal(f.Data, &d); err != nil {
					t.Fatal(err)
				}
				if f.Kind == "approval/request" {
					aid, _ := d["approval_id"].(string)
					if !approvals[aid] {
						details, _ := d["details"].(map[string]any)
						input, _ := details["input"].(map[string]any)
						if d["title"] != "Artifact" || input["file_path"] != file {
							t.Fatalf("unexpected approval: %s", f.Data)
						}
						control(threads.Control{ID: aid, ApprovalID: aid, ThreadID: id, RunID: id, Action: "approval", Decision: "approve"})
						approvals[aid] = true
					}
				}
				if f.Kind == "tool/call" && d["turn_id"] == sendID && d["status"] == "completed" {
					toolDone = true
					output, _ := d["output"].(string)
					revisionConfirmed = revisionConfirmed || strings.Contains(output, fmt.Sprintf("Version %d", version))
				}
				if f.Kind == "tool/call" || f.Kind == "tool/output/delta" {
					found := urlPattern.FindString(string(f.Data))
					if found != "" {
						if url != "" && url != found {
							t.Fatal("created a duplicate artifact", found)
						}
						url = found
					}
				}
				if f.Kind == "debug/unknown" {
					t.Fatalf("unexpected frame: %s", f.Data)
				}
				if f.Kind == "turn/completed" && d["turn_id"] == sendID {
					if d["outcome"] != "completed" {
						t.Fatalf("failed turn: %s", f.Data)
					}
					completed = true
				}
			}
			if len(frames) > 0 {
				if err = c.Acknowledge(id, frames[len(frames)-1].Sequence); err != nil {
					t.Fatal(err)
				}
			}
		}
		if !completed || !toolDone || !revisionConfirmed || url == "" {
			t.Fatalf("version %d: completed=%v tool=%v revision=%v url=%s", version, completed, toolDone, revisionConfirmed, url)
		}
		t.Logf("Version %d published through controller: %s", version, url)
	}
	control(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "kill"})
}
