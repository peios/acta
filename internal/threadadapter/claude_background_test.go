package threadadapter

import (
	"acta/internal/threads"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestClaudeBackgroundLifecycle(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-background.json")
	if err != nil {
		t.Fatal(err)
	}
	var captures []object
	if err = json.Unmarshal(raw, &captures); err != nil {
		t.Fatal(err)
	}
	const id = "db799357-e3f3-4f43-9960-df8e8e3bd330"
	const tool = "toolu_01HM5Hxx8DkXb86E7NjnE7Yz"
	for _, order := range [][]int{{0, 1, 2, 3, 4, 5, 5}, {2, 0, 1, 3, 4, 5}, {0, 1, 3, 4, 2, 5}} {
		s := State{RunID: id, NativeID: id, Claude: ClaudeState{Turn: "turn"}, Tools: map[string]*ToolCall{tool: {ID: tool, Turn: "turn", Name: "Bash", Label: "Background QA task", Category: "command", Status: "pending", Arguments: object{}}}}
		notices := 0
		for seq, index := range order {
			// Every frame crosses a serialized checkpoint, as on supervisor recovery.
			checkpoint, _ := json.Marshal(s)
			if err := json.Unmarshal(checkpoint, &s); err != nil {
				t.Fatal(err)
			}
			if index >= 3 {
				s.Claude.Turn = ""
			} // completion after launching turn ended
			text, _ := json.Marshal(captures[index])
			next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: int64(seq + 1), ReceivedAt: time.Now()}, "stdout", string(text))
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range frames {
				if f.Kind == "debug/unknown" || DataError(f) {
					t.Fatalf("unmapped %d: %s", index, f.Data)
				}
				if f.Kind == "tool/notification" {
					notices++
				}
			}
			// A late launch result is accepted even after the turn has moved on.
			s = next
			if index == 1 && seq < 3 && s.Tools[tool].Status != "background" {
				t.Fatal("not background")
			}
		}
		if notices != 2 || s.Tools[tool].Status != "completed" {
			t.Fatalf("notices=%d tool=%+v", notices, s.Tools[tool])
		}
	}
}
func DataError(f threads.Frame) bool {
	var d object
	_ = json.Unmarshal(f.Data, &d)
	return d["processing_error"] != nil
}
func TestBackgroundUnknownIdentity(t *testing.T) {
	s := State{NativeID: "native", Tools: map[string]*ToolCall{"t": {ID: "t", Name: "Bash"}}}
	emit := func(string, object) { t.Fatal("unexpected output") }
	p := threads.ProviderFrame{}
	for _, m := range []object{
		{"type": "system", "subtype": "task_started", "task_id": "x", "tool_use_id": "t", "task_type": "agent", "is_backgrounded": true},
		{"type": "system", "subtype": "task_notification", "task_id": "missing", "status": "completed"},
		{"type": "user", "isReplay": true, "message": object{"role": "user", "content": "<task-notification><task-id>missing</task-id><status>completed</status></task-notification>"}},
	} {
		if s.claudeBackground(p, m, emit) {
			t.Fatalf("accepted %+v", m)
		}
	}
}

func TestClaudeBackgroundAutonomousResponse(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-background-wake.json")
	if err != nil {
		t.Fatal(err)
	}
	var captures []object
	if err = json.Unmarshal(raw, &captures); err != nil {
		t.Fatal(err)
	}
	const id = "db799357-e3f3-4f43-9960-df8e8e3bd330"
	const tool = "toolu_01TKjUMW23HiegBVw4VgeXXJ"
	s := State{RunID: id, NativeID: id, Turns: map[string]string{"old": "completed"}, Claude: ClaudeState{Turn: "old"}, Tools: map[string]*ToolCall{tool: {ID: tool, Turn: "old", Name: "Bash", Label: "Background lifecycle QA", Category: "command", Status: "background", BackgroundID: "bc607vyqy", Arguments: object{}}}}
	notices, starts, ends := 0, 0, 0
	for index, m := range captures {
		text, _ := json.Marshal(m)
		next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: int64(index + 1), ReceivedAt: time.Now()}, "stdout", string(text))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range frames {
			if f.Kind == "debug/unknown" || DataError(f) {
				t.Fatalf("unmapped %d: %s", index, f.Data)
			}
			switch f.Kind {
			case "tool/notification":
				notices++
			case "turn/started":
				starts++
			case "turn/completed":
				ends++
			}
		}
		s = next
	}
	if notices != 1 || starts != 1 || ends != 1 || s.Claude.Turn == "old" || s.Turns[s.Claude.Turn] != "completed" {
		t.Fatalf("notices=%d starts=%d ends=%d turn=%s", notices, starts, ends, s.Claude.Turn)
	}
}

func TestClaudeBackgroundKilled(t *testing.T) {
	const id = "db799357-e3f3-4f43-9960-df8e8e3bd330"
	s := State{RunID: id, NativeID: id, Tools: map[string]*ToolCall{"tool": {ID: "tool", Turn: "turn", Name: "Bash", Category: "command", Label: "Cancelled background QA", Status: "background", BackgroundID: "bfpwuoxte", Arguments: object{}}}}
	text := `{"type":"system","subtype":"task_updated","task_id":"bfpwuoxte","patch":{"status":"killed","end_time":1788899224287},"session_id":"db799357-e3f3-4f43-9960-df8e8e3bd330"}`
	next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}, "stdout", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || frames[1].Kind != "tool/call" || next.Tools["tool"].Status != "interrupted" {
		t.Fatalf("%+v", frames)
	}
}

func TestClaudeEmptyTaskReceiptDoesNotCompleteActiveTurn(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-empty-task-receipt.json")
	if err != nil {
		t.Fatal(err)
	}
	const id = "db799357-e3f3-4f43-9960-df8e8e3bd330"
	for _, turn := range []string{"", "active"} {
		state := State{RunID: id, NativeID: id, Claude: ClaudeState{Turn: turn}, Turns: map[string]string{"active": "started"}}
		next, frames, err := Map(state, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}, "stdout", string(raw))
		if err != nil || len(frames) != 1 || frames[0].Kind != "debug/dropped" || next.Turns["active"] != "started" {
			t.Fatalf("%v %+v %+v", err, frames, next.Turns)
		}
	}
	var m object
	_ = json.Unmarshal(raw, &m)
	m["is_error"] = true
	m["subtype"] = "error_during_execution"
	changed, _ := json.Marshal(m)
	_, frames, err := Map(State{RunID: id, NativeID: id}, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}, "stdout", string(changed))
	if err != nil || frames[0].Kind == "debug/dropped" {
		t.Fatal("silenced an error", err, frames)
	}
}

func TestResumedBackgroundNoticeOnlyAndCheckpoint(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-resumed-background.json")
	if err != nil {
		t.Fatal(err)
	}
	const id = "db799357-e3f3-4f43-9960-df8e8e3bd330"
	const run = "061bd414-8ff7-440e-ab16-1f63fa8144ac"
	const tool = "toolu_01CaYrbpdCGxA2etWgBR4Qw8"
	state := State{RunID: id, NativeID: id, Tools: map[string]*ToolCall{tool: {ID: tool, Turn: "old-turn", Name: "Bash", Label: "Recovery command", BackgroundID: "btn099ydt", Status: "interrupted"}}}
	seq := int64(0)
	apply := func(run string, raw []byte) []threads.Frame {
		seq++
		next, frames, e := Map(state, threads.ProviderFrame{ThreadID: id, RunID: run, Provider: "claude", Sequence: seq, ReceivedAt: time.Now()}, "stdout", string(raw))
		if e != nil {
			t.Fatal(e)
		}
		checkpoint, _ := json.Marshal(next)
		state, e = DecodeState(checkpoint)
		if e != nil {
			t.Fatal(e)
		}
		for _, f := range frames {
			if f.Kind == "tool/call" || f.Kind == "turn/started" || f.Kind == "debug/unknown" || DataError(f) {
				t.Fatalf("unexpected frame %s: %s", f.Kind, f.Data)
			}
		}
		return frames
	}
	f := apply(run, raw)
	if len(f) != 2 || f[1].Kind != "tool/notification" || len(state.Tools) != 0 {
		t.Fatal(f, state.Tools)
	}
	if f = apply(run, raw); len(f) != 1 {
		t.Fatal("duplicate notice", f)
	}
	echo := []byte(`{"type":"user","isReplay":true,"session_id":"` + id + `","message":{"role":"user","content":"<task-notification><task-id>btn099ydt</task-id><tool-use-id>` + tool + `</tool-use-id><status>stopped</status><summary>Stopped after previous session</summary></task-notification>"}}`)
	if f = apply(run, echo); len(f) != 2 {
		t.Fatal("missing context relocation", f)
	}
	if f = apply(run, echo); len(f) != 1 {
		t.Fatal("repeated echo", f)
	}
	if f = apply(id, raw); len(f) != 2 {
		t.Fatal("subsequent run needs own notice", f)
	}
	if state.BackgroundHistory["btn099ydt"].Label != "Recovery command" {
		t.Fatal("lost original identity")
	}
	// Unknown unrelated identities must not be guessed from notice text.
	var m object
	_ = json.Unmarshal(raw, &m)
	m["tool_use_id"] = "different"
	other, _ := json.Marshal(m)
	_, f, err = Map(state, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 99, ReceivedAt: time.Now()}, "stdout", string(other))
	if err != nil || f[0].Kind != "debug/unknown" {
		t.Fatal(err, f)
	}
}

func TestClaudeForegroundTaskLifecycle(t *testing.T) {
	const session = "eb60110d-e1ad-468f-a874-7d1c1635de90"
	const tool = "toolu_01MxdKMRKwa1L5G2CWzupHWP"
	start := object{"type": "system", "subtype": "task_started", "task_id": "blu2vrzxk", "tool_use_id": tool, "description": "Build learn to validate the new citation anchor", "is_backgrounded": false, "task_type": "local_bash", "uuid": "4175fba0-8126-40c4-bcf1-83cb43ebc8dc", "session_id": session}
	notice := object{"type": "system", "subtype": "task_notification", "task_id": "blu2vrzxk", "tool_use_id": tool, "status": "completed", "output_file": "", "summary": "Build learn to validate the new citation anchor", "uuid": "121d99f1-60db-462c-929d-2cc5e37bd9da", "session_id": session}
	result := object{"type": "user", "session_id": session, "message": object{"content": []any{object{"type": "tool_result", "tool_use_id": tool, "content": "Build output", "is_error": false}}}, "tool_use_result": object{"stdout": "Build output", "stderr": ""}}
	for _, early := range []bool{false, true} {
		t.Run(map[bool]string{false: "notice after result", true: "notice before result"}[early], func(t *testing.T) {
			s := State{RunID: session, NativeID: session, Claude: ClaudeState{Turn: "turn"}, Tools: map[string]*ToolCall{tool: {ID: tool, Turn: "turn", Name: "Bash", Category: "command", Label: "Build learn", Status: "running", Arguments: object{}}}}
			captures := []object{start, result, notice, notice}
			if early {
				captures = []object{start, notice, result, notice}
			}
			for i, m := range captures {
				raw, _ := json.Marshal(m)
				next, frames, err := Map(s, threads.ProviderFrame{ThreadID: session, RunID: session, Provider: "claude", Sequence: int64(i + 1), ReceivedAt: time.Now()}, "stdout", string(raw))
				if err != nil {
					t.Fatal(err)
				}
				for _, f := range frames {
					if f.Kind == "debug/unknown" || f.Kind == "tool/notification" || DataError(f) {
						t.Fatalf("unexpected frame: %s %s", f.Kind, f.Data)
					}
					if m["type"] == "system" && f.Kind != "debug/local" {
						t.Fatalf("foreground bookkeeping emitted %s", f.Kind)
					}
				}
				// Exercise the checkpoint path between start and notification.
				checkpoint, _ := json.Marshal(next)
				s, err = DecodeState(checkpoint)
				if err != nil {
					t.Fatal(err)
				}
			}
			actual := s.Tools[tool]
			if actual.Status != "completed" || !actual.ResultReceived || actual.Output == nil || *actual.Output != "Build output" || actual.BackgroundID != "" {
				t.Fatalf("lost ordinary tool result: %+v", actual)
			}
		})
	}
}

func TestClaudeForegroundIdentityAndPromotion(t *testing.T) {
	s := State{Tools: map[string]*ToolCall{"a": {ID: "a", Name: "Bash", Status: "running"}, "b": {ID: "b", Name: "Bash", Status: "running"}}}
	p := threads.ProviderFrame{ReceivedAt: time.Now()}
	emit := func(string, object) {}
	start := func(tool, id string, background any) object {
		return object{"type": "system", "subtype": "task_started", "tool_use_id": tool, "task_id": id, "task_type": "local_bash", "is_backgrounded": background}
	}
	if !s.claudeBackground(p, start("a", "task", false), emit) {
		t.Fatal("foreground rejected")
	}
	for _, m := range []object{start("b", "task", false), start("b", "task", true), start("a", "different", false), start("a", "different", true), start("a", "task", nil), start("a", "task", "false"), {"type": "system", "subtype": "task_notification", "tool_use_id": "b", "task_id": "task", "status": "completed"}} {
		if s.claudeBackground(p, m, emit) {
			t.Fatalf("accepted conflicting shape: %+v", m)
		}
	}
	if !s.claudeBackground(p, start("a", "task", true), emit) || s.Tools["a"].Status != "background" {
		t.Fatal("foreground could not transition to background")
	}
	notices := 0
	if !s.claudeBackground(p, object{"type": "system", "subtype": "task_notification", "tool_use_id": "a", "task_id": "task", "status": "completed", "summary": "Done"}, func(kind string, _ object) {
		if kind == "tool/notification" {
			notices++
		}
	}) || notices != 1 {
		t.Fatal("promoted background lost completion notice")
	}
}
