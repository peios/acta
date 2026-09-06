package claude

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/peios/acta/internal/agentsession/model"
)

// frames builds a stored transcript from (kind, payload) pairs.
func frames(pairs ...string) []model.Frame {
	var out []model.Frame
	at := time.Date(2026, 9, 5, 21, 0, 0, 0, time.UTC)
	for i := 0; i+1 < len(pairs); i += 2 {
		// stamp each payload with its seq so identical fixtures stay distinct
		payload := strings.TrimSpace(pairs[i+1])
		payload = "{\"_seq\":" + fmt.Sprint(i/2+1) + "," + payload[1:]
		out = append(out, model.Frame{Seq: int64(i/2 + 1), Kind: pairs[i], Payload: json.RawMessage(payload), At: at.Add(time.Duration(i) * time.Second), Stored: true})
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

// nothingLost checks the verbatim rule: every stored frame appears exactly
// once among the events' raw payloads.
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
		if !f.Stored {
			continue
		}
		if count[f.Seq] != 1 {
			t.Errorf("frame %d (%s) appears %d times in raw panels, want 1", f.Seq, f.Kind, count[f.Seq])
		}
	}
}

func TestPlainTurn(t *testing.T) {
	fs := frames(
		"state", `{"state":"spawned","resumed":false,"styles":[{"name":"default"}]}`,
		"input", `{"text":"hi"}`,
		"system", `{"type":"system","subtype":"init","model":"claude-haiku-4-5","permissionMode":"auto","cwd":"/tmp","session_id":"abc"}`,
		"user", `{"type":"user","isReplay":true,"uuid":"u1","message":{"role":"user","content":"hi"}}`,
		"assistant", `{"type":"assistant","message":{"model":"claude-haiku-4-5","usage":{"input_tokens":10,"cache_read_input_tokens":90},"content":[{"type":"text","text":"Hello"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`,
		"user", `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"a\nb"}]}}`,
		"assistant", `{"type":"assistant","message":{"model":"claude-haiku-4-5","content":[{"type":"text","text":"Done"}]}}`,
		"result", `{"type":"result","subtype":"success","num_turns":2,"duration_ms":1200,"usage":{"input_tokens":5,"output_tokens":7},"modelUsage":{"claude-haiku-4-5":{"contextWindow":200000}},"result":"Done"}`,
		"rate_limit_event", `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0.1,"resetsAt":1},"seven_day":{"utilization":0.5,"resetsAt":2}}}}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "session.spawned turn.idle input session.init→session:1 user.message assistant usage.context tool.result→tool:t1 assistant turn.end turn.idle usage.limits"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	// the tool block is classified and the result carries the call's name
	var res model.Event
	for _, e := range evs {
		if e.T == model.ToolResult {
			res = e
		}
	}
	if res.Data["name"] != "Bash" || res.Data["text"] != "a\nb" {
		t.Errorf("tool.result data: %v", res.Data)
	}
	// context accounting saw the prompt size, and the turn carries the window
	for _, e := range evs {
		if e.T == model.UsageContext && e.Data["used"] != int64(100) {
			t.Errorf("usage.context used = %v", e.Data["used"])
		}
		if e.T == model.TurnEnd && (e.Data["context_window"] != int64(200000) || e.Data["calls"] != 2 || e.Data["ok"] != true) {
			t.Errorf("turn.end data: %v", e.Data)
		}
		if e.T == model.UsageLimits {
			w := e.Data["windows"].(map[string]any)
			if w["weekly"] == nil || w["5h"] == nil {
				t.Errorf("usage.limits windows: %v", w)
			}
		}
	}
}

func TestLocalCommandAndReport(t *testing.T) {
	fs := frames(
		"input", `{"text":"/context"}`,
		"assistant", `{"type":"assistant","message":{"model":"<synthetic>","content":[{"type":"text","text":"## Context Usage\n| a | b |"}]}}`,
		"result", `{"type":"result","subtype":"success","num_turns":0}`,
		"input", `{"text":"/goal all tests pass"}`,
		"assistant", `{"type":"assistant","message":{"model":"<synthetic>","content":[{"type":"text","text":"Goal set: all tests pass"}]}}`,
		"result", `{"type":"result","subtype":"success","num_turns":0}`,
		"input", `{"text":"/goal clear"}`,
		"assistant", `{"type":"assistant","message":{"model":"<synthetic>","content":[{"type":"text","text":"Goal cleared: all tests pass"}]}}`,
		"result", `{"type":"result","subtype":"success","num_turns":0}`,
		"input", `{"text":"/effort low"}`,
		"assistant", `{"type":"assistant","message":{"model":"<synthetic>","content":[{"type":"text","text":"Set effort level to low"}]}}`,
		"result", `{"type":"result","subtype":"success","num_turns":0}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "fold fold→cmd:1 report fold→cmd:1 turn.idle input cmd.reply→cmd:4 goal fold→cmd:4 turn.idle input cmd.reply→cmd:7 goal fold→cmd:7 turn.idle input effort→cmd:10 effort→cmd:10 fold→cmd:10 turn.idle"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	for _, e := range evs {
		if e.T == model.Report && (e.Data["key"] != "context" || !strings.Contains(e.Data["text"].(string), "Context Usage")) {
			t.Errorf("report: %v", e.Data)
		}
		if e.T == model.Goal && e.Seq == 5 && (e.Data["state"] != "active" || e.Data["cond"] != "all tests pass") {
			t.Errorf("goal: %v", e.Data)
		}
		if e.T == model.Goal && e.Seq == 8 && e.Data["state"] != "cleared" {
			t.Errorf("goal after clear: %v", e.Data)
		}
	}
}

func TestReportInitLandsOnHost(t *testing.T) {
	// the init that follows a silent report command lands where the report
	// folded (its cmd ref), never on a divider of its own
	fs := frames(
		"input", `{"text":"hi"}`,
		"assistant", `{"type":"assistant","message":{"model":"m","content":[{"type":"text","text":"yo"}]}}`,
		"result", `{"type":"result","subtype":"success","num_turns":1}`,
		"input", `{"text":"/usage"}`,
		"system", `{"type":"system","subtype":"init","model":"m"}`,
		"assistant", `{"type":"assistant","message":{"model":"<synthetic>","content":[{"type":"text","text":"You are using your subscription"}]}}`,
		"result", `{"type":"result","subtype":"success","num_turns":0}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "input assistant turn.end turn.idle fold session.init→cmd:4 fold→cmd:4 report fold→cmd:4 turn.idle"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	if evs[4].Ref != "cmd:4" {
		t.Errorf("silent report fold ref = %q, want cmd:4", evs[4].Ref)
	}
}

func TestApproval(t *testing.T) {
	fs := frames(
		"assistant", `{"type":"assistant","message":{"model":"m","content":[{"type":"tool_use","id":"t9","name":"Write","input":{"file_path":"/x/y.txt","content":"hi"}}]}}`,
		"control_request", `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Write","tool_use_id":"t9","input":{"file_path":"/x/y.txt"},"permission_suggestions":[{"type":"addRules"}]}}`,
		"control", `{"type":"control_response","response":{"subtype":"success","request_id":"r1","response":{"behavior":"allow","updatedInput":{}}}}`,
		"control_response", `{"type":"control_response","response":{"subtype":"success","request_id":"r1","response":{"behavior":"allow","updatedInput":{}}}}`,
		"user", `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t9","content":"ok"}]},"tool_use_result":{"type":"create","filePath":"/x/y.txt","content":"hi\n"}}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "assistant approval.request→tool:t9 approval.answer→approval:r1 fold→approval:r1 tool.result→tool:t9"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	if evs[2].Data["outcome"] != "allowed" || evs[2].Data["mine"] != true {
		t.Errorf("answer: %v", evs[2].Data)
	}
	d, _ := evs[4].Data["diff"].(map[string]any)
	if d == nil || d["kind"] != "create" {
		t.Errorf("diff: %v", evs[4].Data["diff"])
	}
}

func TestQuestionAndPlan(t *testing.T) {
	fs := frames(
		"assistant", `{"type":"assistant","message":{"model":"m","content":[{"type":"tool_use","id":"q1","name":"AskUserQuestion","input":{"questions":[{"question":"Which?","options":[{"label":"A"}]}]}}]}}`,
		"control_request", `{"type":"control_request","request_id":"r2","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","tool_use_id":"q1","input":{"questions":[{"question":"Which?"}]}}}`,
		"control", `{"type":"control_response","response":{"subtype":"success","request_id":"r2","response":{"behavior":"allow","updatedInput":{"answers":{"Which?":"A"}}}}}`,
		"user", `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"q1","content":"\"Which?\"=\"A\""}]}}`,
		"assistant", `{"type":"assistant","message":{"model":"m","content":[{"type":"tool_use","id":"w1","name":"Write","input":{"file_path":"/home/x/.claude/plans/p.md","content":"# Plan\n1. do"}}]}}`,
		"user", `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"w1","content":"written"}]}}`,
		"assistant", `{"type":"assistant","message":{"model":"m","content":[{"type":"tool_use","id":"e1","name":"ExitPlanMode","input":{"plan":"# Plan\n1. do","planFilePath":"/home/x/.claude/plans/p.md"}}]}}`,
		"control_request", `{"type":"control_request","request_id":"r3","request":{"subtype":"can_use_tool","tool_name":"ExitPlanMode","tool_use_id":"e1","input":{"plan":"# Plan\n1. do","planFilePath":"/home/x/.claude/plans/p.md"}}}`,
		"control", `{"type":"control_response","response":{"subtype":"success","request_id":"r3","response":{"behavior":"deny","message":"more tests"}}}`,
		"user", `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"e1","is_error":true,"content":"Plan rejected: more tests"}]}}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "assistant approval.request→tool:q1 approval.answer→approval:r2 question.answer→tool:q1 question.answer→tool:q1 assistant plan.update fold→plan:/home/x/.claude/plans/p.md plan.submit→plan:/home/x/.claude/plans/p.md approval.request→plan:/home/x/.claude/plans/p.md approval.answer→approval:r3 plan.verdict→plan:/home/x/.claude/plans/p.md plan.verdict→plan:/home/x/.claude/plans/p.md"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	// the ExitPlanMode call folded into the existing plan card, so its
	// assistant frame rides the plan.submit event
	for _, e := range evs {
		if e.T == model.PlanSubmit && len(e.Raw) == 0 {
			t.Errorf("plan.submit lost its frame")
		}
		if e.T == model.PlanVerdict && e.Data["state"] != "rejected" {
			t.Errorf("verdict: %v", e.Data)
		}
	}
}

func TestCompaction(t *testing.T) {
	fs := frames(
		"input", `{"text":"/compact"}`,
		"system", `{"type":"system","subtype":"status","status":"compacting"}`,
		"system", `{"type":"system","subtype":"status","compact_result":"success"}`,
		"system", `{"type":"system","subtype":"init","model":"m"}`,
		"system", `{"type":"system","subtype":"compact_boundary","compact_metadata":{"pre_tokens":50000,"post_tokens":8000,"trigger":"manual"}}`,
		"user", `{"type":"user","message":{"role":"user","content":"Summary of everything"}}`,
		"assistant", `{"type":"assistant","message":{"model":"<synthetic>","content":[{"type":"text","text":"Compacted"}]}}`,
		"result", `{"type":"result","subtype":"success","num_turns":0,"usage":{"input_tokens":0,"output_tokens":0}}`,
		"input", `{"text":"go on"}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "input compact.start→cmd:1 fold→compact:2 fold→compact:2 compact.end→compact:2 usage.context compact.summary→compact:2 fold→compact:2 fold→compact:2 turn.idle input"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
}

func TestPeerAndReset(t *testing.T) {
	fs := frames(
		"command_lifecycle", `{"type":"command_lifecycle","state":"started"}`,
		"user", `{"type":"user","isReplay":true,"uuid":"u2","message":{"content":"Another Claude session sent a message:\n<cross-session-message from=\"uds:/run/x/1.sock\" from-name=\"Test\" from-mode=\"prompting\">hello there</cross-session-message>"}}`,
		"command_lifecycle", `{"type":"command_lifecycle","state":"completed"}`,
		"input", `{"text":"/clear"}`,
		"conversation_reset", `{"type":"conversation_reset","new_conversation_id":"n1"}`,
		"system", `{"type":"system","subtype":"init","model":"m","session_id":"real"}`,
		"result", `{"type":"result","subtype":"success","num_turns":0}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "peer.message fold→peer:2 input session.reset→cmd:4 session.init→session:5 fold→session:5 turn.idle"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	if evs[0].Data["name"] != "Test" || evs[0].Data["text"] != "hello there" || len(evs[0].Raw) != 2 {
		t.Errorf("peer: %v raw=%d", evs[0].Data, len(evs[0].Raw))
	}
}

func TestSubagentLane(t *testing.T) {
	fs := frames(
		"assistant", `{"type":"assistant","message":{"model":"m","content":[{"type":"tool_use","id":"a1","name":"Agent","input":{"subagent_type":"Explore","description":"look","prompt":"find x"}}]}}`,
		"system", `{"type":"system","subtype":"task_started","tool_use_id":"a1","task_id":"tk1","description":"look"}`,
		"assistant", `{"type":"assistant","parent_tool_use_id":"a1","message":{"model":"m","content":[{"type":"text","text":"looking"}]}}`,
		"system", `{"type":"system","subtype":"task_progress","task_id":"tk1","last_tool_name":"Grep"}`,
		"system", `{"type":"system","subtype":"task_notification","task_id":"tk1","status":"completed","summary":"found it"}`,
		"user", `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a1","content":"found it"}]}}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "assistant agent.start→tool:a1 assistant agent.progress→tool:a1 agent.end agent.end→tool:a1"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	if evs[2].Lane != "a1" {
		t.Errorf("subagent frame lane = %q", evs[2].Lane)
	}
	if evs[4].Ref != "lane-done:a1" || evs[4].Data["summary"] != "found it" {
		t.Errorf("agent.end: ref=%q data=%v", evs[4].Ref, evs[4].Data)
	}
}

func TestInterruptAndExit(t *testing.T) {
	fs := frames(
		"user", `{"type":"user","message":{"content":[{"type":"text","text":"[Request interrupted by user]"}]}}`,
		"result", `{"type":"result","subtype":"error_during_execution","is_error":true,"num_turns":1}`,
		"state", `{"state":"exit","code":0}`,
		"input", `{"text":"again"}`,
		"state", `{"state":"undelivered","reason":"no harness connected"}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "turn.end turn.idle session.exit→turn:2 turn.idle input session.undelivered→input:4"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	if evs[0].Data["interrupted"] != true || len(evs[0].Raw) != 2 {
		t.Errorf("interrupted turn: %v raw=%d", evs[0].Data, len(evs[0].Raw))
	}
}

func TestDeterministic(t *testing.T) {
	fs := frames(
		"input", `{"text":"hi"}`,
		"assistant", `{"type":"assistant","message":{"model":"m","content":[{"type":"text","text":"yo"}]}}`,
		"result", `{"type":"result","subtype":"success","num_turns":1}`,
	)
	a, _ := json.Marshal(run(t, fs))
	b, _ := json.Marshal(run(t, fs))
	if string(a) != string(b) {
		t.Errorf("projection differs between runs")
	}
	if !strings.Contains(string(a), `"t":"assistant"`) {
		t.Errorf("unexpected JSON: %s", a)
	}
}

func ExampleProjector() {
	p := New()
	evs := p.Project(model.Frame{Seq: 1, Kind: "input", Payload: json.RawMessage(`{"text":"/compact tidy up"}`), At: time.Unix(0, 0), Stored: true})
	fmt.Println(evs[0].T, evs[0].Ref, evs[0].Data["cmd"].(map[string]any)["args"])
	// Output: input cmd:1 tidy up
}

func TestToolProgressFoldsIntoTheCall(t *testing.T) {
	fs := frames(
		"input", `{"text":"build it"}`,
		"assistant", `{"type":"assistant","message":{"model":"m","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"make"}}]}}`,
		"tool_progress", `{"type":"tool_progress","uuid":"h1","heartbeat":true,"tool_name":"Bash","tool_use_id":"t1-heartbeat-0","parent_tool_use_id":"t1","elapsed_time_seconds":30}`,
		"tool_progress", `{"type":"tool_progress","uuid":"h2","heartbeat":true,"tool_name":"Bash","tool_use_id":"t1-heartbeat-1","parent_tool_use_id":"t1","elapsed_time_seconds":60}`,
		"user", `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`,
	)
	evs := run(t, fs)
	nothingLost(t, fs, evs)
	want := "input assistant fold→tool:t1 fold→tool:t1 tool.result→tool:t1"
	if got := kinds(evs); got != want {
		t.Errorf("kinds:\n got %s\nwant %s", got, want)
	}
	for _, e := range evs {
		if e.T == model.Unknown {
			t.Errorf("tool_progress left unknown")
		}
	}
}
