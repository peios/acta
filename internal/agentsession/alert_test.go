package agentsession

import (
	"testing"

	"github.com/peios/acta/internal/agentsession/model"
)

func TestAlertFor(t *testing.T) {
	ev := func(t string, d map[string]any) model.Event { return model.Event{T: t, Data: d} }
	cases := []struct {
		name          string
		ev            model.Event
		verb, summary string
		ok            bool
	}{
		{"bash permission", ev(model.ApprovalRequest, map[string]any{"kind": "tool", "tool": "Bash", "input": map[string]any{"command": "git push\norigin"}}), "permission", "needs permission for Bash: git push", true},
		{"write permission", ev(model.ApprovalRequest, map[string]any{"kind": "tool", "tool": "Write", "input": map[string]any{"file_path": "/home/x/proj/note.txt"}}), "permission", "needs permission for Write: note.txt", true},
		{"codex patch", ev(model.ApprovalRequest, map[string]any{"kind": "tool", "tool": "apply_patch", "display": "Edit files", "description": "outside the sandbox", "input": map[string]any{"reason": "outside the sandbox"}}), "permission", "needs permission for Edit files: outside the sandbox", true},
		{"auto review", ev(model.ApprovalRequest, map[string]any{"kind": "tool", "tool": "apply_patch", "auto": true}), "", "", false},
		{"question", ev(model.ApprovalRequest, map[string]any{"kind": "question", "questions": []any{map[string]any{"question": "Which one?"}}}), "question", "has a question: Which one?", true},
		{"plan", ev(model.ApprovalRequest, map[string]any{"kind": "plan"}), "plan", "wants approval for a plan", true},
		{"elicitation", ev(model.ApprovalRequest, map[string]any{"kind": "elicitation", "server": "elic", "message": "A few details"}), "elicitation", "needs input for elic: A few details", true},
		{"other request", ev(model.ApprovalRequest, map[string]any{"kind": "other"}), "", "", false},
		{"turn ended", ev(model.TurnEnd, map[string]any{"ok": true, "result": "Done.\nMore"}), "turn_ended", "finished a turn: Done.", true},
		{"turn failed", ev(model.TurnEnd, map[string]any{"ok": false, "error": "max turns reached"}), "failed", "stopped on an error: max turns reached", true},
		{"interrupted", ev(model.TurnEnd, map[string]any{"ok": false, "interrupted": true}), "", "", false},
		{"clean exit", ev(model.SessionExit, map[string]any{"code": 0}), "", "", false},
		{"crash", ev(model.SessionExit, map[string]any{"code": 1}), "exited", "exited with code 1", true},
		{"resume failed", ev(model.SessionResumeFail, map[string]any{}), "failed", "couldn't resume the conversation and started fresh", true},
		{"assistant", ev(model.Assistant, map[string]any{}), "", "", false},
	}
	for _, c := range cases {
		verb, summary, ok := alertFor(c.ev)
		if ok != c.ok || verb != c.verb || summary != c.summary {
			t.Errorf("%s: got (%q, %q, %v), want (%q, %q, %v)", c.name, verb, summary, ok, c.verb, c.summary, c.ok)
		}
	}
}
