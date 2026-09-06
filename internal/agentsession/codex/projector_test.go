package codex

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/peios/acta/internal/agentsession/model"
)

// frames builds a stored transcript from (kind, payload) pairs; kinds are
// what Kind() would label the line, or Acta's own input/control/state.
func frames(pairs ...string) []model.Frame {
	var out []model.Frame
	at := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	for i := 0; i+1 < len(pairs); i += 2 {
		payload := strings.TrimSpace(pairs[i+1])
		payload = "{\"_seq\":" + fmt.Sprint(i/2+1) + "," + payload[1:]
		kind := pairs[i]
		if kind == "" {
			kind = Kind(json.RawMessage(payload))
		}
		out = append(out, model.Frame{Seq: int64(i/2 + 1), Kind: kind, Payload: json.RawMessage(payload), At: at.Add(time.Duration(i) * time.Second), Stored: true})
	}
	return out
}

func run(t *testing.T, fs []model.Frame) []model.Event {
	t.Helper()
	p := New()
	var out []model.Event
	for _, f := range fs {
		if !json.Valid(f.Payload) {
			t.Fatalf("frame %d: invalid JSON fixture: %s", f.Seq, f.Payload)
		}
		out = append(out, p.Project(f)...)
	}
	return out
}

func kinds(evs []model.Event) string {
	var ks []string
	for _, e := range evs {
		k := e.T
		if e.To != "" {
			k += "→" + e.To
		}
		ks = append(ks, k)
	}
	return strings.Join(ks, " ")
}

func nothingLost(t *testing.T, fs []model.Frame, evs []model.Event) {
	t.Helper()
	count := map[int64]int{}
	for _, e := range evs {
		for _, r := range e.Raw {
			if r.Payload == nil {
				continue
			}
			for _, f := range fs {
				if string(f.Payload) == string(r.Payload) && f.Kind == r.Kind {
					count[f.Seq]++
				}
			}
		}
	}
	for _, f := range fs {
		if count[f.Seq] != 1 {
			t.Errorf("frame %d (%s) appears %d times in raw panels, want 1", f.Seq, f.Kind, count[f.Seq])
		}
	}
}

const thread = "01a073cd-4315-71b1-aca6-7d8e72ef6ed2"

func TestTurnWithCommandAndPatch(t *testing.T) {
	fs := frames(
		"state", `{"state":"spawned","resumed":false}`,
		"", `{"id":"acta-init","result":{"userAgent":"acta/0.153.4"}}`,
		"", `{"id":"acta-thread-start","result":{"thread":{"id":"`+thread+`","model":"gpt-5.4-mini","cwd":"/w"},"model":"gpt-5.4-mini","approvalPolicy":"on-request","approvalsReviewer":"user","sandbox":{"type":"workspaceWrite"},"reasoningEffort":"low","cwd":"/w"}}`,
		"", `{"method":"thread/started","params":{"thread":{"id":"`+thread+`","model":"gpt-5.4-mini"}}}`,
		"input", `{"text":"list files then write notes.txt"}`,
		"", `{"id":"acta-turn-1","result":{"turn":{"id":"T1","status":"inProgress"}}}`,
		"", `{"method":"turn/started","params":{"threadId":"`+thread+`","turn":{"id":"T1","status":"inProgress"}}}`,
		"", `{"method":"item/started","params":{"item":{"type":"userMessage","id":"u1","content":[{"type":"text","text":"list files then write notes.txt"}]},"threadId":"`+thread+`","turnId":"T1"}}`,
		"", `{"method":"item/completed","params":{"item":{"type":"userMessage","id":"u1","content":[{"type":"text","text":"list files then write notes.txt"}]},"threadId":"`+thread+`","turnId":"T1"}}`,
		"", `{"method":"item/started","params":{"item":{"type":"reasoning","id":"r1","summary":[],"content":[]},"turnId":"T1"}}`,
		"", `{"method":"item/completed","params":{"item":{"type":"reasoning","id":"r1","summary":["Listing first."],"content":[]},"turnId":"T1"}}`,
		"", `{"method":"item/started","params":{"item":{"type":"agentMessage","id":"m1","text":"","phase":"commentary"},"turnId":"T1"}}`,
		"", `{"method":"item/completed","params":{"item":{"type":"agentMessage","id":"m1","text":"I’ll list the files.","phase":"commentary"},"turnId":"T1"}}`,
		"", `{"method":"item/started","params":{"item":{"type":"commandExecution","id":"c1","command":"/usr/bin/bash -lc ls","cwd":"/w","status":"inProgress","commandActions":[{"type":"listFiles","command":"ls","path":null}],"aggregatedOutput":null},"turnId":"T1"}}`,
		"", `{"method":"item/completed","params":{"item":{"type":"commandExecution","id":"c1","command":"/usr/bin/bash -lc ls","cwd":"/w","status":"completed","commandActions":[{"type":"listFiles","command":"ls","path":null}],"aggregatedOutput":"README.md\n","exitCode":0,"durationMs":3},"turnId":"T1"}}`,
		"", `{"method":"thread/tokenUsage/updated","params":{"threadId":"`+thread+`","turnId":"T1","tokenUsage":{"total":{"totalTokens":16618,"inputTokens":16352,"outputTokens":266},"last":{"totalTokens":16618,"inputTokens":16352,"outputTokens":266},"modelContextWindow":258400}}}`,
		"", `{"method":"account/rateLimits/updated","params":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":14,"windowDurationMins":10080,"resetsAt":1789239448},"secondary":null,"planType":"prolite"}}}`,
		"", `{"method":"item/started","params":{"item":{"type":"fileChange","id":"f1","changes":[{"path":"/w/notes.txt","kind":{"type":"add"},"diff":"hello\n"}],"status":"inProgress"},"turnId":"T1"}}`,
		"", `{"id":7,"method":"item/fileChange/requestApproval","params":{"itemId":"f1","threadId":"`+thread+`","turnId":"T1","reason":"outside the sandbox","startedAtMs":1}}`,
		"control", `{"op":"answer","id":"7","kind":"item/fileChange/requestApproval","outcome":"allow"}`,
		"", `{"method":"serverRequest/resolved","params":{"requestId":7,"threadId":"`+thread+`"}}`,
		"", `{"method":"item/completed","params":{"item":{"type":"fileChange","id":"f1","changes":[{"path":"/w/notes.txt","kind":{"type":"add"},"diff":"hello\n"}],"status":"completed"},"turnId":"T1"}}`,
		"", `{"method":"turn/diff/updated","params":{"threadId":"`+thread+`","turnId":"T1","diff":"diff --git a/notes.txt b/notes.txt\n+hello\n"}}`,
		"", `{"method":"turn/diff/updated","params":{"threadId":"`+thread+`","turnId":"T1","diff":"diff --git a/notes.txt b/notes.txt\n+hello\n"}}`,
		"", `{"method":"item/started","params":{"item":{"type":"agentMessage","id":"m2","text":"","phase":"final_answer"},"turnId":"T1"}}`,
		"", `{"method":"item/completed","params":{"item":{"type":"agentMessage","id":"m2","text":"Done.","phase":"final_answer"},"turnId":"T1"}}`,
		"", `{"method":"thread/status/changed","params":{"threadId":"`+thread+`","status":{"type":"idle"}}}`,
		"", `{"method":"turn/completed","params":{"threadId":"`+thread+`","turn":{"id":"T1","status":"completed","durationMs":6510,"items":[{"type":"agentMessage","id":"m2","text":"Done."}]}}}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "session.spawned turn.idle fold→session:1 session.init→session:1 fold→session:1 input fold→input:5 fold→input:5 user.message fold fold→input:5 thought fold→input:5 assistant tool.call tool.result→tool:c1 usage.context usage.limits tool.call approval.request→tool:f1 approval.answer→approval:7 fold→approval:7 tool.result→tool:f1 turn.diff fold fold→input:5 assistant fold turn.end turn.idle"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	for _, e := range evs {
		switch e.T {
		case model.SessionInit:
			if e.Data["model"] != "gpt-5.4-mini" || e.Data["permission_mode"] != "acceptEdits" || e.Data["conversation"] != thread {
				t.Errorf("init: %v", e.Data)
			}
		case model.ToolCall:
			if e.Data["name"] == "Bash" && e.Data["input"].(map[string]any)["command"] != "ls" {
				t.Errorf("command not unwrapped: %v", e.Data)
			}
		case model.ToolResult:
			if e.Data["name"] == "Bash" && (e.Data["text"] != "README.md\n" || e.Data["exit_code"] != 0) {
				t.Errorf("bash result: %v", e.Data)
			}
			if e.Data["name"] == "apply_patch" {
				d, _ := e.Data["diff"].(map[string]any)
				if d == nil || d["kind"] != "create" || d["file"] != "/w/notes.txt" {
					t.Errorf("patch result: %v", e.Data)
				}
			}
		case model.ApprovalRequest:
			if e.Data["tool"] != "apply_patch" || e.Data["id"] != "7" || e.Data["subtype"] != "item/fileChange/requestApproval" {
				t.Errorf("approval: %v", e.Data)
			}
		case model.ApprovalAnswer:
			if e.Data["outcome"] != "allowed" {
				t.Errorf("answer: %v", e.Data)
			}
		case model.UsageContext:
			if e.Data["used"] != int64(16618) || e.Data["window"] != int64(258400) {
				t.Errorf("usage.context: %v", e.Data)
			}
		case model.UsageLimits:
			w := e.Data["windows"].(map[string]any)["weekly"].(map[string]any)
			if w["utilization"] != 0.14 {
				t.Errorf("usage.limits: %v", e.Data)
			}
		case model.TurnEnd:
			if e.Data["ok"] != true || e.Data["duration_ms"] != int64(6510) || e.Data["result"] != "Done." {
				t.Errorf("turn.end: %v", e.Data)
			}
		case model.Thought:
			if e.Data["text"] != "Listing first." {
				t.Errorf("thought: %v", e.Data)
			}
		}
	}
}

func TestAutoReviewGoalAndCompaction(t *testing.T) {
	fs := frames(
		"", `{"method":"item/started","params":{"item":{"type":"fileChange","id":"f2","changes":[{"path":"/w/a.txt","kind":{"type":"add"},"diff":"x"}],"status":"inProgress"},"turnId":"T1"}}`,
		"", `{"method":"item/autoApprovalReview/started","params":{"reviewId":"R1","targetItemId":"f2","action":{"type":"applyPatch","cwd":"/w","files":["/w/a.txt"]},"review":{"status":"inProgress"}}}`,
		"", `{"method":"guardianWarning","params":{"message":"Automatic approval review approved (risk: low)"}}`,
		"", `{"method":"item/autoApprovalReview/completed","params":{"reviewId":"R1","targetItemId":"f2","decisionSource":"agent","review":{"status":"approved","riskLevel":"low","rationale":"narrow and reversible"}}}`,
		"input", `{"text":"/goal notes.txt exists"}`,
		"", `{"id":"acta-goal-set-1","result":{"goal":{"objective":"notes.txt exists","status":"active"}}}`,
		"", `{"method":"thread/goal/updated","params":{"goal":{"objective":"notes.txt exists","status":"active"}}}`,
		"input", `{"text":"/goal clear"}`,
		"", `{"id":"acta-goal-clear-2","result":{"cleared":true}}`,
		"", `{"method":"thread/goal/cleared","params":{"threadId":"x"}}`,
		"input", `{"text":"/compact"}`,
		"", `{"id":"acta-compact-3","result":{}}`,
		"", `{"method":"turn/started","params":{"turn":{"id":"T9"}}}`,
		"", `{"method":"item/started","params":{"item":{"type":"contextCompaction","id":"cc1"},"turnId":"T9"}}`,
		"", `{"method":"item/completed","params":{"item":{"type":"contextCompaction","id":"cc1"},"turnId":"T9"}}`,
		"", `{"method":"turn/completed","params":{"turn":{"id":"T9","status":"completed","items":[]}}}`,
		"", `{"method":"turn/plan/updated","params":{"turnId":"T2","explanation":null,"plan":[{"step":"list files","status":"completed"},{"step":"write notes","status":"inProgress"}]}}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "tool.call approval.request→tool:f2 fold→approval:R1 approval.answer→approval:R1 input cmd.reply→cmd:5 goal goal→cmd:5 input cmd.reply→cmd:8 goal goal→cmd:8 input compact.start→cmd:11 fold→compact:12 fold→compact:12 compact.end→compact:12 fold→compact:12 turn.idle tasks→tasks"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	for _, e := range evs {
		if e.T == model.ApprovalRequest && e.Data["auto"] != true {
			t.Errorf("auto review not flagged: %v", e.Data)
		}
		if e.T == model.ApprovalAnswer && (e.Data["outcome"] != "allowed" || e.Data["message"] != "narrow and reversible") {
			t.Errorf("auto answer: %v", e.Data)
		}
		if e.T == model.Tasks && (e.Data["done"] != 1 || e.Data["total"] != 2) {
			t.Errorf("tasks: %v", e.Data)
		}
	}
	// the goal ends cleared
	last := evs[len(evs)-1]
	_ = last
	var goalStates []string
	for _, e := range evs {
		if e.T == model.Goal {
			goalStates = append(goalStates, e.Data["state"].(string))
		}
	}
	if strings.Join(goalStates, ",") != "active,active,cleared,cleared" {
		t.Errorf("goal states: %v", goalStates)
	}
}

func TestWire(t *testing.T) {
	opts := map[string]any{"permission_mode": "auto", "model": "gpt-5.4-mini", "effort": "low"}
	start := StartLines("s1", opts, "/w", false)
	if len(start) != 3 || !strings.Contains(string(start[2]), `"thread/start"`) || !strings.Contains(string(start[2]), `"approvalsReviewer":"auto_review"`) {
		t.Errorf("start lines: %q", start)
	}
	opts["conversation"] = thread
	res := StartLines("s1", opts, "/w", true)
	if !strings.Contains(string(res[2]), `"thread/resume"`) || !strings.Contains(string(res[2]), thread) {
		t.Errorf("resume line: %s", res[2])
	}
	in := string(InputLine(opts, "hi", nil))
	if !strings.Contains(in, `"turn/start"`) || !strings.Contains(in, `"effort":"low"`) || !strings.Contains(in, `"sandboxPolicy":{"type":"workspaceWrite"}`) {
		t.Errorf("input line: %s", in)
	}
	opts["turn"] = "T1"
	if st := string(InputLine(opts, "more", nil)); !strings.Contains(st, `"turn/steer"`) || !strings.Contains(st, `"expectedTurnId":"T1"`) {
		t.Errorf("steer line: %s", st)
	}
	if c := string(InputLine(opts, "/compact", nil)); !strings.Contains(c, `"thread/compact/start"`) {
		t.Errorf("compact line: %s", c)
	}
	ans := ControlLines(opts, Op{Op: "answer", ID: "7", Kind: "item/commandExecution/requestApproval", Outcome: "allow"})
	if len(ans) != 1 || string(ans[0]) != `{"id":7,"jsonrpc":"2.0","result":{"decision":"accept"}}` {
		t.Errorf("answer line: %q", ans)
	}
	q := ControlLines(opts, Op{Op: "answer", ID: "q9", Kind: "item/tool/requestUserInput", Outcome: "allow", Answers: json.RawMessage(`{"q1":"yes"}`)})
	if len(q) != 1 || !strings.Contains(string(q[0]), `"answers":{"q1":{"answers":["yes"]}}`) {
		t.Errorf("question answer: %q", q)
	}
	if k, v, ok := Option("control", json.RawMessage(`{"op":"setting","key":"fast","value":"on"}`)); !ok || k != "service_tier" || v != "priority" {
		t.Errorf("fast option: %s %s %v", k, v, ok)
	}
	if n := Notes("turn/started", json.RawMessage(`{"method":"turn/started","params":{"turn":{"id":"T5"}}}`)); n["turn"] != "T5" {
		t.Errorf("notes: %v", n)
	}
	if Kind(json.RawMessage(`{"id":"x","result":{}}`)) != "response" || Kind(json.RawMessage(`{"method":"turn/started"}`)) != "turn/started" {
		t.Errorf("kind")
	}
	if Stored("item/agentMessage/delta", nil) || !Stored("item/completed", nil) {
		t.Errorf("stored")
	}
}

func TestModelSwitchFollowsThreadSettings(t *testing.T) {
	fs := frames(
		"", `{"id":"acta-thread-start","result":{"thread":{"id":"`+thread+`"},"model":"gpt-6-astra","reasoningEffort":"high","serviceTier":"default","approvalPolicy":"on-request","approvalsReviewer":"auto_review","sandbox":{"type":"workspaceWrite"}}}`,
		"", `{"method":"thread/settings/updated","params":{"threadSettings":{"model":"gpt-6-astra","effort":"high","serviceTier":"default","approvalPolicy":"on-request","approvalsReviewer":"auto_review","sandboxPolicy":{"type":"workspaceWrite"}}}}`,
		"control", `{"id":"model-1","op":"setting","key":"model","value":"gpt-5.6-luna"}`,
		"input", `{"text":"hi"}`,
		"", `{"method":"thread/settings/updated","params":{"threadSettings":{"model":"gpt-5.6-luna","effort":"high","personality":"pragmatic","serviceTier":"default","approvalPolicy":"on-request","approvalsReviewer":"auto_review","sandboxPolicy":{"type":"workspaceWrite"}}}}`,
		"", `{"method":"item/completed","params":{"item":{"type":"agentMessage","id":"m1","text":"hello","phase":"final_answer"},"turnId":"T1"}}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "session.init fold setting input setting→setting:3 setting assistant"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	last := evs[len(evs)-1]
	if last.Data["model"] != "gpt-5.6-luna" {
		t.Errorf("assistant model after switch = %v", last.Data["model"])
	}
	for _, e := range evs {
		if e.T == model.Setting && e.To == "setting:3" && (e.Data["key"] != "model" || e.Data["value"] != "gpt-5.6-luna" || e.Data["requested"] != nil) {
			t.Errorf("confirmation: %v", e.Data)
		}
	}
}

// TestTerminalInteractionFoldsIntoTheCommand checks the notice that the
// model wrote to a running command's stdin lands on that command's call
// rather than as an unknown frame.
func TestTerminalInteractionFoldsIntoTheCommand(t *testing.T) {
	fs := frames(
		"", `{"method":"item/started","params":{"item":{"type":"commandExecution","id":"c9","command":"python","cwd":"/w","status":"inProgress","aggregatedOutput":null},"turnId":"T1"}}`,
		"", `{"method":"item/commandExecution/terminalInteraction","params":{"stdin":"print(1)\n","itemId":"c9","turnId":"T1","threadId":"th","processId":"85117"}}`,
		"", `{"method":"item/completed","params":{"item":{"type":"commandExecution","id":"c9","command":"python","cwd":"/w","status":"completed","aggregatedOutput":"1\n","exitCode":0},"turnId":"T1"}}`,
	)
	evs := run(t, fs)
	for _, e := range evs {
		if e.T == model.Unknown {
			t.Fatalf("terminalInteraction rendered as unknown: %s", kinds(evs))
		}
	}
	if !strings.Contains(kinds(evs), "fold→tool:c9") {
		t.Errorf("expected a fold into the command: %s", kinds(evs))
	}
	nothingLost(t, fs, evs)
}
