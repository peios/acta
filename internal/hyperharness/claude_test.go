package hyperharness

import (
	"acta2/internal/harnesspipe"
	"acta2/internal/threads"
	"bufio"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeLifecycleHelper(t *testing.T) {
	if os.Getenv("ACTA_CLAUDE_HELPER") != "1" {
		return
	}
	id := os.Getenv("ACTA_CLAUDE_SESSION")
	model, effort := "native-small", ""
	fast := false
	send := func(v any) { raw, _ := json.Marshal(v); os.Stdout.Write(append(raw, '\n')) }
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var m map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &m)
		if m["type"] == "control_request" {
			q := m["request"].(map[string]any)
			body := map[string]any{}
			switch q["subtype"] {
			case "rename_session":
				if q["title"] == "Rejected" {
					send(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": m["request_id"], "error": "rename rejected"}})
					continue
				}
				body = q
			case "initialize":
				state := "off"
				if fast {
					state = "on"
				}
				body = map[string]any{"session_state": "idle", "fast_mode_state": state, "current_permission_mode": "default"}
			case "get_binary_version":
				body["version"] = "test"
			case "get_settings":
				body["applied"] = map[string]any{"model": model, "effort": effort}
			case "apply_flag_settings":
				settings := q["settings"].(map[string]any)
				model = settings["model"].(string)
				if model == "small" {
					model = "native-small"
				}
				effort, _ = settings["effortLevel"].(string)
				fast, _ = settings["fastMode"].(bool)
			case "list_models":
				body["models"] = []any{map[string]any{"value": "small", "resolvedModel": "native-small", "displayName": "Small"}}
			case "mcp_status":
				body["mcpServers"] = []any{}
			}
			send(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": m["request_id"], "response": body}})
		} else if m["type"] == "user" {
			command := m["uuid"]
			if command == id {
				// Creation must never inject a bootstrap user message.
				os.Exit(4)
			}
			send(map[string]any{"type": "command_lifecycle", "session_id": id, "command_uuid": command, "state": "started"})
			m["isReplay"] = true
			send(m)
			send(map[string]any{"type": "assistant", "session_id": id, "message": map[string]any{"id": command.(string) + "-reply", "content": []any{map[string]any{"type": "text", "text": "Message received."}}}})
			send(map[string]any{"type": "result", "session_id": id, "user_message_uuid": command, "subtype": "success", "is_error": false, "duration_ms": 1, "queued_turn_count": 0})
		}
	}
	os.Exit(0)
}
func TestClaudeControlAndReplay(t *testing.T) {
	t.Setenv("ACTA_CLAUDE_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	root := t.TempDir()
	id := uuid.NewString()
	t.Setenv("ACTA_CLAUDE_SESSION", id)
	pipe, err := harnesspipe.Open(filepath.Join(root, "pipe"))
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()
	c, err := NewController(t.Context(), filepath.Join(root, "controller"), pipe)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { c.Close() }()
	executable, _ := os.Executable()
	spec := func(d threads.Descriptor) (harnesspipe.Spec, error) {
		return harnesspipe.Spec{ThreadID: d.ID, RunID: d.RunID, CWD: d.CWD, Executable: executable, Args: []string{"-test.run=^TestClaudeLifecycleHelper$"}}, nil
	}
	c.spawnSpec = spec
	run := func(q threads.Control) threads.Result {
		t.Helper()
		if err := c.Control(q); err != nil {
			t.Fatal(err)
		}
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(time.Millisecond * 5) {
			for _, r := range c.Results() {
				if r.ID == q.ID && !controllerPending(c, id) {
					return r
				}
			}
		}
		t.Fatal("no control result", q)
		return threads.Result{}
	}
	if r := run(threads.Control{ID: id, ThreadID: id, Action: "start", Provider: "claude", CWD: root}); r.Error != "" {
		t.Fatal(r)
	}
	assertEmptyThread(t, c, id)
	initialFrames, _ := c.Frames(t.Context(), id)
	if len(initialFrames) > 0 {
		if err := c.Acknowledge(id, initialFrames[len(initialFrames)-1].Sequence); err != nil {
			t.Fatal(err)
		}
	}
	if r := run(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "models"}); r.Outcome != "accepted" || len(r.Models) != 1 || len(r.Models[0].Efforts) != 0 {
		t.Fatal(r)
	}
	configure := threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "configure", Settings: threads.ModelSettings{Model: "small"}}
	if r := run(configure); r.Outcome != "accepted" || r.Settings == nil || r.Settings.Model != "native-small" {
		t.Fatal(r)
	}
	send := threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "send", Text: "Hello"}
	if r := run(send); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	c.Close()
	c, err = NewController(t.Context(), filepath.Join(root, "controller"), pipe)
	if err != nil {
		t.Fatal(err)
	}
	c.spawnSpec = spec
	c.Recover()
	if r := run(send); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	frames, err := c.Frames(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, f := range frames {
		if f.Kind == "message/user" && strings.Contains(string(f.Data), send.ID) {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("repeated provider message after reattachment: %d", users)
	}
	if r := run(threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "kill"}); r.Error != "" {
		t.Fatal(r)
	}
}
func TestClaudeSavedHistory(t *testing.T) {
	root := t.TempDir()
	id := uuid.NewString()
	directory := filepath.Join(root, "projects", "test")
	os.MkdirAll(directory, 0700)
	path := filepath.Join(directory, id+".jsonl")
	for _, raw := range []string{"", `{"type":"summary"}`, `{"type":"user","sessionId":"wrong","message":{"role":"user","content":"hello"}}`, `{"type":"user","sessionId":"` + id + `","message":{"role":"user","content":""}}`} {
		os.WriteFile(path, []byte(raw), 0600)
		if claudeSavedHistory(root, id) {
			t.Fatal("false commitment", raw)
		}
	}
	os.WriteFile(path, []byte(`{"type":"user","sessionId":"`+id+`","message":{"role":"user","content":"hello"}}`), 0600)
	if !claudeSavedHistory(root, id) {
		t.Fatal("stored history not recognized")
	}
}
