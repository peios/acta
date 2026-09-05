package agentsession

import (
	"encoding/json"
	"testing"
)

func TestAlertFor(t *testing.T) {
	cases := []struct {
		name, kind, payload, verb, summary string
		ok                                 bool
	}{
		{"bash permission", "control_request", `{"request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"git push\norigin"}}}`, "permission", "needs permission for Bash: git push", true},
		{"write permission", "control_request", `{"request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":"/home/x/proj/note.txt"}}}`, "permission", "needs permission for Write: note.txt", true},
		{"question", "control_request", `{"request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"question":"Which one?"}]}}}`, "question", "has a question: Which one?", true},
		{"plan", "control_request", `{"request":{"subtype":"can_use_tool","tool_name":"ExitPlanMode","input":{"plan":"# Plan"}}}`, "plan", "wants approval for a plan", true},
		{"elicitation", "control_request", `{"request":{"subtype":"elicitation","mcp_server_name":"elic","message":"A few details"}}`, "elicitation", "needs input for elic: A few details", true},
		{"other control", "control_request", `{"request":{"subtype":"interrupt"}}`, "", "", false},
		{"turn ended", "result", `{"subtype":"success","result":"Done.\nMore"}`, "turn_ended", "finished a turn: Done.", true},
		{"turn failed", "result", `{"subtype":"error_max_turns","is_error":true}`, "failed", "stopped on an error (max turns)", true},
		{"clean exit", "state", `{"state":"exit","code":0}`, "", "", false},
		{"crash", "state", `{"state":"exit","code":1}`, "exited", "exited with code 1", true},
		{"resume failed", "state", `{"state":"resume_failed"}`, "failed", "couldn't resume the conversation and started fresh", true},
		{"assistant", "assistant", `{"message":{}}`, "", "", false},
	}
	for _, c := range cases {
		verb, summary, ok := alertFor(c.kind, json.RawMessage(c.payload))
		if ok != c.ok || verb != c.verb || summary != c.summary {
			t.Errorf("%s: got (%q, %q, %v), want (%q, %q, %v)", c.name, verb, summary, ok, c.verb, c.summary, c.ok)
		}
	}
}
