package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/peios/acta/internal/agentsession/model"
)

// TestPruneKeepsShape checks each category replaces only its own values,
// leaves the record readable by the projector, and reports no change when
// nothing in the frame belongs to a chosen category.
func TestPruneKeepsShape(t *testing.T) {
	big := strings.Repeat("x", 1000)
	user := `{"type":"user","uuid":"u1","toolUseResult":{"stdout":"` + big + `"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"` + big + `"},{"type":"text","text":"and a note"}]}}`
	asst := `{"type":"assistant","uuid":"a1","message":{"role":"assistant","content":[{"type":"thinking","thinking":"` + big + `","signature":"sig"},{"type":"tool_use","id":"t1","name":"Write","input":{"file_path":"/x","content":"` + big + `"}},{"type":"text","text":"done"}]}}`
	img := `{"type":"user","uuid":"u2","message":{"content":[{"type":"image","source":{"type":"base64","data":"` + big + `"}},{"type":"text","text":"see"}]}}`
	input := `{"text":"look","images":[{"media_type":"image/png","data":"` + big + `"}]}`

	only := func(c string) map[string]bool { return map[string]bool{c: true} }

	out, changed := Prune("user", json.RawMessage(user), only(model.PruneToolOutput))
	if !changed || len(out) > 400 {
		t.Fatalf("tool output not pruned: %d bytes", len(out))
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	if _, ok := m["toolUseResult"]; ok {
		t.Error("toolUseResult should go with tool output")
	}
	blocks := m["message"].(map[string]any)["content"].([]any)
	if c := blocks[0].(map[string]any)["content"].(string); !strings.HasPrefix(c, "[output pruned") {
		t.Errorf("tool_result content = %q", c)
	}
	if blocks[1].(map[string]any)["text"] != "and a note" {
		t.Error("the text block should be untouched")
	}
	if _, changed := Prune("user", json.RawMessage(user), only(model.PruneThinking)); changed {
		t.Error("a user record has no thinking to prune")
	}

	out, changed = Prune("assistant", json.RawMessage(asst), only(model.PruneThinking))
	_ = json.Unmarshal(out, &m)
	blocks = m["message"].(map[string]any)["content"].([]any)
	th := blocks[0].(map[string]any)
	if !changed || !strings.HasPrefix(th["thinking"].(string), "[thinking pruned") || th["signature"] != nil {
		t.Errorf("thinking block after prune: %v", th)
	}
	if in := blocks[1].(map[string]any)["input"].(map[string]any); len(in["content"].(string)) != 1000 {
		t.Error("tool input should be untouched when only thinking is chosen")
	}
	out, _ = Prune("assistant", json.RawMessage(asst), only(model.PruneToolInput))
	_ = json.Unmarshal(out, &m)
	in := m["message"].(map[string]any)["content"].([]any)[1].(map[string]any)["input"].(map[string]any)
	if in["file_path"] != "/x" || !strings.HasPrefix(in["content"].(string), "[input pruned") {
		t.Errorf("tool input after prune: %v", in)
	}

	out, changed = Prune(TranscriptKind, json.RawMessage(img), only(model.PruneImages))
	_ = json.Unmarshal(out, &m)
	b0 := m["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if !changed || b0["type"] != "text" || !strings.HasPrefix(b0["text"].(string), "[image pruned") {
		t.Errorf("image block after prune: %v", b0)
	}
	out, changed = Prune("input", json.RawMessage(input), only(model.PruneImages))
	_ = json.Unmarshal(out, &m)
	if !changed || m["images"] != nil || m["images_pruned"] != float64(1) || m["text"] != "look" {
		t.Errorf("input frame after prune: %s", out)
	}
	if _, changed := Prune("state", json.RawMessage(`{"state":"exit"}`), only(model.PruneToolOutput)); changed {
		t.Error("a state frame is never pruned")
	}
}
