package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSubagentCaptures(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			raw, e := os.ReadFile("testdata/" + provider + "-subagents.json")
			if e != nil {
				t.Fatal(e)
			}
			var inputs []json.RawMessage
			json.Unmarshal(raw, &inputs)
			native := "5b24dfdc-d229-42e4-9b43-af1f3de85723"
			if provider == "codex" {
				native = "01a0823d-29fc-79c1-9b0c-d3c577ec57f3"
			}
			const id = "5b24dfdc-d229-42e4-9b43-af1f3de85723"
			s := State{RunID: id, NativeID: native, Configuration: object{"model": "parent"}}
			messages, lanes := 0, 0
			for n, input := range inputs {
				checkpoint, _ := json.Marshal(s)
				s, e = DecodeState(checkpoint)
				if e != nil {
					t.Fatal(e)
				}
				next, frames, e := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: provider, Sequence: int64(n + 1), ReceivedAt: time.Now()}, "stdout", string(input))
				if e != nil {
					t.Fatal(e)
				}
				for _, f := range frames {
					if DataError(f) || f.Kind == "debug/unknown" {
						t.Fatalf("input %d: %s %s", n, f.Kind, f.Data)
					}
					if f.Kind == "message/assistant" {
						if f.LaneID == "" {
							t.Fatal("child escaped lane")
						}
						messages++
					}
					if f.Kind == "subagent/status" {
						lanes++
					}
				}
				s = next
			}
			if messages == 0 || lanes == 0 {
				t.Fatalf("no child content: messages=%d lanes=%d", messages, lanes)
			}
			if s.Configuration["model"] != "parent" || s.Claude.Turn != "" {
				t.Fatal("child overwrote parent", s.Configuration, s.Claude.Turn)
			}
		})
	}
}

func TestClaudeBackgroundLaneWakesNewParentTurn(t *testing.T) {
	const id = "5b24dfdc-d229-42e4-9b43-af1f3de85723"
	s := State{RunID: id, NativeID: id, Turns: map[string]string{"parent": "completed"}, Claude: ClaudeState{Turn: "parent"}, Lanes: map[string]*Lane{"child": {ID: "child", Native: id, Tool: "tool", State: State{RunID: id}}}}
	raws := []string{
		`{"type":"system","subtype":"task_notification","task_id":"child","tool_use_id":"tool","status":"completed","summary":"done","session_id":"` + id + `"}`,
		`{"type":"stream_event","session_id":"` + id + `","event":{"type":"message_start","message":{"id":"response","model":"claude-haiku-4-5","content":[]}}}`,
	}
	for n, raw := range raws {
		next, b, e := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: int64(n + 1), ReceivedAt: time.Now()}, "stdout", raw)
		if e != nil {
			t.Fatal(e)
		}
		for _, f := range b {
			if f.Kind == "debug/unknown" || DataError(f) {
				t.Fatal(string(f.Data))
			}
		}
		s = next
	}
	if s.Claude.Turn != "background/response" || s.Turns["parent"] != "completed" {
		t.Fatal("background reply reused completed parent turn", s.Claude.Turn)
	}
}

func TestCodexNestedLanesAndInterrupt(t *testing.T) {
	const id = "5b24dfdc-d229-42e4-9b43-af1f3de85723"
	s := State{NativeID: "root"}
	var seq int64
	apply := func(m object) []threads.Frame {
		t.Helper()
		seq++
		raw, _ := json.Marshal(m)
		next, b, e := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "codex", Sequence: seq, ReceivedAt: time.Now()}, "stdout", string(raw))
		if e != nil {
			t.Fatal(e)
		}
		for _, f := range b {
			if f.Kind == "debug/unknown" || DataError(f) {
				t.Fatal(string(f.Data))
			}
		}
		s = next
		return b
	}
	start := func(parent, child string) []threads.Frame {
		return apply(object{"method": "item/started", "params": object{"threadId": parent, "turnId": "same-turn", "item": object{"type": "subAgentActivity", "id": "same-tool", "kind": "started", "agentThreadId": child, "agentPath": "/root/" + child}}})
	}
	start("root", "child")
	frames := start("child", "leaf")
	found := false
	for _, f := range frames {
		if f.Kind == "subagent/status" {
			found = true
			if f.LaneID != "child" {
				t.Fatal("nested card in wrong lane")
			}
		}
	}
	if !found {
		t.Fatal("no nested card")
	}
	apply(object{"method": "turn/started", "params": object{"threadId": "leaf", "turn": object{"id": "leaf-turn", "status": "inProgress"}}})
	apply(object{"method": "turn/completed", "params": object{"threadId": "leaf", "turn": object{"id": "leaf-turn", "status": "interrupted", "items": []any{}}}})
	if s.Lanes["leaf"].Status != "interrupted" || s.Lanes["child"].Status != "running" {
		t.Fatal("child terminal state crossed lanes")
	}
}

func TestClaudeChildBackgroundAndInterruptionMarker(t *testing.T) {
	const id = "5b24dfdc-d229-42e4-9b43-af1f3de85723"
	s := State{RunID: id, NativeID: id, Lanes: map[string]*Lane{"child": {ID: "child", Native: id, Tool: "agent-tool", State: State{RunID: id, NativeID: id, Tools: map[string]*ToolCall{"bash": {ID: "bash", Name: "Bash", Turn: "turn", Status: "running", Category: "command", Label: "Background command", Arguments: object{}}}}}}}
	raws := []string{
		`{"type":"system","subtype":"task_started","task_type":"local_bash","task_id":"background","tool_use_id":"bash","owned_by_subagent":true,"is_backgrounded":true}`,
		`{"type":"system","subtype":"task_notification","task_id":"background","tool_use_id":"bash","status":"completed","summary":"done"}`,
		`{"type":"user","uuid":"marker","parent_tool_use_id":"agent-tool","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]}}`,
	}
	for n, raw := range raws {
		var frame object
		if err := json.Unmarshal([]byte(raw), &frame); err != nil {
			t.Fatal(err)
		}
		frame["session_id"] = id
		encoded, _ := json.Marshal(frame)
		raw = string(encoded)
		next, b, e := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: int64(n + 1), ReceivedAt: time.Now()}, "stdout", raw)
		if e != nil {
			t.Fatal(e)
		}
		for _, f := range b {
			if f.Kind == "debug/unknown" || DataError(f) || f.Kind == "message/user" {
				t.Fatal(f.Kind, string(f.Data))
			}
			if f.Kind == "tool/call" && f.LaneID != "child" {
				t.Fatal("background tool escaped child")
			}
		}
		s = next
	}
	if s.Lanes["child"].State.Tools["bash"].Status != "completed" {
		t.Fatal("child background not completed")
	}
}

func TestClaudeChildBackgroundWakeStartsDistinctTurn(t *testing.T) {
	const id = "5b24dfdc-d229-42e4-9b43-af1f3de85723"
	s := State{RunID: id, NativeID: id, Lanes: map[string]*Lane{"child": {ID: "child", Native: id, Tool: "agent-tool", Name: "Worker", Prompt: "Original task", Status: "completed", ActivityID: "first", State: State{RunID: id, NativeID: id, Turns: map[string]string{"first": "completed"}, Claude: ClaudeState{Turn: "first"}}}}}
	raw := `{"type":"system","subtype":"task_started","task_type":"local_agent","task_id":"child","description":"Worker","prompt":"<task-notification><task-id>background</task-id></task-notification>","uuid":"wake","session_id":"` + id + `"}`
	next, b, e := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}, "stdout", raw)
	if e != nil {
		t.Fatal(e)
	}
	started := 0
	for _, f := range b {
		if f.Kind == "debug/unknown" || DataError(f) {
			t.Fatal(string(f.Data))
		}
		if f.Kind == "turn/started" {
			started++
			if f.LaneID != "child" {
				t.Fatal("wake escaped lane")
			}
		}
	}
	l := next.Lanes["child"]
	if started != 1 || l.State.Claude.Turn == "first" || l.ActivityID == "first" || l.Prompt != "Original task" {
		t.Fatalf("wake reused old turn or replaced task: %+v", l)
	}
	// A replay of the same start event must not manufacture another turn.
	again, frames, e := Map(next, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 2, ReceivedAt: time.Now()}, "stdout", raw)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range frames {
		if f.Kind == "turn/started" {
			t.Fatal("replayed wake duplicated turn")
		}
	}
	if again.Lanes["child"].ActivityID != l.ActivityID {
		t.Fatal("unstable wake identity")
	}
}

func TestChildQuestionRoutingAndResolution(t *testing.T) {
	const run = "5b24dfdc-d229-42e4-9b43-af1f3de85723"
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			s := State{RunID: run, NativeID: "parent", Lanes: map[string]*Lane{"child": {ID: "child", Native: "child", Tool: "agent-tool", Status: "running", State: State{RunID: run, NativeID: "child", Claude: ClaudeState{Turn: "child-turn"}}}}}
			request := `{"id":42,"method":"item/tool/requestUserInput","params":{"threadId":"child","turnId":"child-turn","itemId":"tool","isBlocking":true,"questions":[{"id":"colour","header":"Colour","question":"Colour?","isOther":true,"options":[{"label":"Blue","description":"Blue"}]}]}}`
			cancel := `{"method":"serverRequest/resolved","params":{"threadId":"child","requestId":42}}`
			if provider == "claude" {
				s.Lanes["child"].Native = "parent"
				s.Lanes["child"].State.NativeID = "parent"
				request = `{"type":"control_request","request_id":"request","request":{"agent_id":"child","subtype":"can_use_tool","tool_name":"AskUserQuestion","tool_use_id":"tool","input":{"questions":[{"question":"Colour?","header":"Colour","options":[{"label":"Blue","description":"Blue"}],"multiSelect":false}]}}}`
				cancel = `{"type":"control_cancel_request","request_id":"request"}`
			}
			for n, raw := range []string{request, cancel} {
				next, frames, e := Map(s, threads.ProviderFrame{ThreadID: run, RunID: run, Provider: provider, Sequence: int64(n + 1), ReceivedAt: time.Now()}, "stdout", raw)
				if e != nil {
					t.Fatal(e)
				}
				found := false
				expected := []string{"question/request", "question/resolved"}[n]
				for _, f := range frames {
					if f.Kind == "debug/unknown" || DataError(f) {
						t.Fatal(string(f.Data))
					}
					if f.Kind == expected {
						found = true
						if f.LaneID != "child" {
							t.Fatal("question escaped lane")
						}
					}
				}
				if !found {
					t.Fatal("missing", expected)
				}
				data, _ := json.Marshal(next)
				s, e = DecodeState(data)
				if e != nil {
					t.Fatal(e)
				}
			}
			if s.Lanes["child"].Status != "running" {
				t.Fatal("resolved question left lane waiting")
			}
		})
	}
}

func TestCodexFollowupReceiptDoesNotRestartCompletedChild(t *testing.T) {
	const run = "5b24dfdc-d229-42e4-9b43-af1f3de85723"
	s := State{RunID: run, NativeID: "parent", Lanes: map[string]*Lane{"child": {ID: "child", Native: "child", Status: "completed", State: State{RunID: run}}}}
	raw := `{"method":"item/completed","params":{"threadId":"parent","turnId":"followup","item":{"type":"subAgentActivity","id":"call-followup","kind":"interacted","agentThreadId":"child","agentPath":"/root/child"}}}`
	next, b, e := Map(s, threads.ProviderFrame{ThreadID: run, RunID: run, Provider: "codex", Sequence: 1, ReceivedAt: time.Now()}, "stdout", raw)
	if e != nil || len(b) != 1 || b[0].Kind != "debug/local" || DataError(b[0]) {
		t.Fatal(e, b)
	}
	if next.Lanes["child"].Status != "completed" {
		t.Fatal("receipt changed lifecycle")
	}
}

func TestRequestLaneRejectsPriorRunChildIdentity(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			s := State{NativeID: "parent", Lanes: map[string]*Lane{"child": {ID: "child", Native: "child", State: State{RunID: "old"}}}}
			raw := json.RawMessage(`{"params":{"threadId":"child"}}`)
			if provider == "claude" {
				raw = json.RawMessage(`{"type":"control_request","request":{"agent_id":"child"}}`)
			}
			if _, _, ok := s.RequestLane(provider, "current", raw); ok {
				t.Fatal("prior-run child accepted")
			}
			s.Lanes["child"].State.RunID = "current"
			if _, lane, ok := s.RequestLane(provider, "current", raw); !ok || lane != "child" {
				t.Fatal("current child rejected")
			}
		})
	}
}

func TestClaudeQueuedBackgroundRepliesHaveDistinctTurns(t *testing.T) {
	const id = "5b24dfdc-d229-42e4-9b43-af1f3de85723"
	s := State{RunID: id, NativeID: id, Turns: map[string]string{"main": "completed"}, Claude: ClaudeState{Turn: "main"}, Lanes: map[string]*Lane{"child": {ID: "child", Native: id, Tool: "tool", State: State{RunID: id}}}}
	inputs := []string{
		`{"type":"system","subtype":"task_notification","task_id":"child","tool_use_id":"tool","status":"completed","summary":"first"}`,
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"first","content":[]}}}`,
		`{"type":"system","subtype":"task_notification","task_id":"child","tool_use_id":"tool","status":"completed","summary":"second"}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"first reply"}`,
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"second","content":[]}}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"second reply"}`,
	}
	turns := map[string]int{}
	for n, input := range inputs {
		var m object
		json.Unmarshal([]byte(input), &m)
		m["session_id"] = id
		raw, _ := json.Marshal(m)
		next, frames, e := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: int64(n + 1), ReceivedAt: time.Now()}, "stdout", string(raw))
		if e != nil {
			t.Fatal(e)
		}
		for _, f := range frames {
			if f.Kind == "debug/unknown" || DataError(f) {
				t.Fatal(string(f.Data))
			}
			if f.Kind == "turn/completed" {
				var d object
				json.Unmarshal(f.Data, &d)
				turns[str(d["turn_id"])]++
			}
		}
		s = next
	}
	if turns["background/first"] != 1 || turns["background/second"] != 1 {
		t.Fatalf("queued replies collapsed into old turn: %v", turns)
	}
}

func TestChildTurnDurationExcludesPreviousActivities(t *testing.T) {
	const id = "5b24dfdc-d229-42e4-9b43-af1f3de85723"
	now := time.Now().UTC()
	s := State{RunID: id, NativeID: id, Lanes: map[string]*Lane{"child": {ID: "child", Native: id, Tool: "tool", Started: now.Add(-2 * time.Second), State: State{RunID: id, Claude: ClaudeState{Turn: "wake"}}}}}
	raw := `{"type":"system","subtype":"task_notification","task_id":"child","tool_use_id":"tool","status":"completed","usage":{"duration_ms":99999},"session_id":"` + id + `"}`
	_, frames, e := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: now}, "stdout", raw)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range frames {
		if DataError(f) || f.Kind == "debug/unknown" {
			t.Fatal(string(f.Data))
		}
		if f.Kind == "turn/completed" {
			found = true
			var d object
			dec := json.NewDecoder(strings.NewReader(string(f.Data)))
			dec.UseNumber()
			dec.Decode(&d)
			if numeric(d["duration_ms"]) != 2000 {
				t.Fatal("cumulative task duration used for turn", d)
			}
		}
	}
	if !found {
		t.Fatal("missing completion")
	}
}

func TestClaudeChildForegroundNotificationWithoutParent(t *testing.T) {
	const id = "eb60110d-e1ad-468f-a874-7d1c1635de90"
	const tool = "toolu_016TW7nyk5XDLxnbc5XVTJop"
	s := State{RunID: id, NativeID: id, Lanes: map[string]*Lane{"child": {ID: "child", Native: id, Tool: "toolu_01YSN7AG6SLCVAGuxkSy2DDN", State: State{RunID: id, NativeID: id, Claude: ClaudeState{Turn: "turn"}, Turns: map[string]string{"turn": "started"}, Tools: map[string]*ToolCall{tool: {ID: tool, Name: "Bash", Turn: "turn", Status: "pending", Category: "command", Label: "cargo check pu_cut", Arguments: object{}}}}}}}
	apply := func(m object) []threads.Frame {
		t.Helper()
		m["session_id"] = id
		raw, _ := json.Marshal(m)
		next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}, "stdout", string(raw))
		if err != nil {
			t.Fatal(err)
		}
		s = next
		return frames
	}
	start := object{"type": "system", "subtype": "task_started", "task_id": "bnmkg6nbm", "owned_by_subagent": true, "tool_use_id": tool, "description": "cargo check pu_cut", "is_backgrounded": false, "task_type": "local_bash"}
	for _, f := range apply(start) {
		if f.Kind != "debug/local" || f.LaneID != "child" || DataError(f) {
			t.Fatalf("start: %s %s", f.Kind, f.Data)
		}
	}
	checkpoint, _ := json.Marshal(s)
	var err error
	s, err = DecodeState(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	notice := object{"type": "system", "subtype": "task_notification", "task_id": "bnmkg6nbm", "tool_use_id": tool, "status": "completed", "output_file": "", "summary": "cargo check pu_cut"}
	for range 2 {
		for _, f := range apply(notice) {
			if f.Kind != "debug/local" || f.LaneID != "child" || DataError(f) {
				t.Fatalf("notification: lane %s %s %s", f.LaneID, f.Kind, f.Data)
			}
		}
	}
	// The ordinary result must still carry the output and complete only the child tool.
	result := object{"type": "user", "uuid": "result", "parent_tool_use_id": "toolu_01YSN7AG6SLCVAGuxkSy2DDN", "message": object{"role": "user", "content": []any{object{"type": "tool_result", "tool_use_id": tool, "content": "Finished check", "is_error": false}}}}
	for _, f := range apply(result) {
		if f.Kind == "debug/unknown" || DataError(f) {
			t.Fatal(string(f.Data))
		}
	}
	call := s.Lanes["child"].State.Tools[tool]
	if call.Status != "completed" || call.Output == nil || *call.Output != "Finished check" || s.Tools[tool] != nil {
		t.Fatal("result crossed lanes or lost output")
	}
	notice["task_id"] = "unrelated"
	frames := apply(notice)
	if frames[0].Kind != "debug/unknown" {
		t.Fatal("uncorrelated task accepted")
	}
	notice["task_id"] = "bnmkg6nbm"
	s.Lanes["child"].State.RunID = "previous-run"
	frames = apply(notice)
	if frames[0].Kind != "debug/unknown" {
		t.Fatal("stale lane accepted")
	}
}
