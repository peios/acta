package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/peios/acta/internal/agentsession/model"
)

// TestPruneBothItemShapes checks the rollout's core items and the
// app-server's v2 items are both pruned by category, keeping their shape.
func TestPruneBothItemShapes(t *testing.T) {
	big := strings.Repeat("y", 1000)
	core := `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"c1","command":["ls"],"aggregated_output":"` + big + `","stdout":"` + big + `"}}}`
	v2 := `{"method":"item/completed","params":{"item":{"type":"commandExecution","id":"c1","command":"ls","aggregatedOutput":"` + big + `"}}}`
	reason := `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"Reasoning","id":"r1","summary_text":["` + big + `"],"raw_content":[]}}}`
	change := `{"method":"item/completed","params":{"item":{"type":"fileChange","id":"f1","changes":[{"path":"a.go","diff":"` + big + `"}]}}}`
	only := func(c string) map[string]bool { return map[string]bool{c: true} }
	var m map[string]any

	out, changed := Prune(TranscriptKind, json.RawMessage(core), only(model.PruneToolOutput))
	_ = json.Unmarshal(out, &m)
	it := m["payload"].(map[string]any)["item"].(map[string]any)
	if !changed || !strings.HasPrefix(it["aggregated_output"].(string), "[output pruned") || !strings.HasPrefix(it["stdout"].(string), "[output pruned") {
		t.Errorf("core command after prune: %v", it)
	}
	if it["command"].([]any)[0] != "ls" {
		t.Error("the command itself should stay")
	}
	out, changed = Prune("item/completed", json.RawMessage(v2), only(model.PruneToolOutput))
	_ = json.Unmarshal(out, &m)
	it = m["params"].(map[string]any)["item"].(map[string]any)
	if !changed || !strings.HasPrefix(it["aggregatedOutput"].(string), "[output pruned") {
		t.Errorf("v2 command after prune: %v", it)
	}
	if _, changed := Prune(TranscriptKind, json.RawMessage(core), only(model.PruneThinking)); changed {
		t.Error("a command has no thinking")
	}
	out, changed = Prune(TranscriptKind, json.RawMessage(reason), only(model.PruneThinking))
	_ = json.Unmarshal(out, &m)
	it = m["payload"].(map[string]any)["item"].(map[string]any)
	if !changed || !strings.HasPrefix(it["summary_text"].([]any)[0].(string), "[thinking pruned") {
		t.Errorf("reasoning after prune: %v", it)
	}
	out, changed = Prune("item/completed", json.RawMessage(change), only(model.PruneToolInput))
	_ = json.Unmarshal(out, &m)
	ch := m["params"].(map[string]any)["item"].(map[string]any)["changes"].([]any)[0].(map[string]any)
	if !changed || ch["path"] != "a.go" || !strings.HasPrefix(ch["diff"].(string), "[diff pruned") {
		t.Errorf("file change after prune: %v", ch)
	}
	if _, changed := Prune(TranscriptKind, json.RawMessage(`{"type":"turn_context","payload":{}}`), only(model.PruneToolOutput)); changed {
		t.Error("turn context is never pruned")
	}
}
