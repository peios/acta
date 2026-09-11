package threadadapter

import (
	"acta/internal/threads"
	"encoding/json"
	"testing"
	"time"
)

// Captured child Bash lifecycle: task_started -> timeout promotion -> launch
// receipt without tool_use_result -> killed hours after its subagent finished.
func TestClaudeTaskOnlyPromotionAndKill(t *testing.T) {
	const id = "eb60110d-e1ad-468f-a874-7d1c1635de90"
	const task = "bmlyuzu7j"
	const tool = "toolu_01D1gT7pkYHz3EXKkW17CqU1"
	const child = "ac5bb1f0ac5316bd6"
	const parent = "toolu_01EPALKqxez9gZDv5jxo2Npk"
	for _, lane := range []string{"", child} {
		for _, receiptFirst := range []bool{false, true} {
			t.Run(lane+map[bool]string{true: "/receipt-first", false: "/promotion-first"}[receiptFirst], func(t *testing.T) {
				s := State{RunID: id, NativeID: id, Claude: ClaudeState{Turn: "main"}, Turns: map[string]string{"main": "started"}}
				owner := &s
				if lane != "" {
					s.Lanes = map[string]*Lane{child: {ID: child, Native: id, Tool: parent, Status: "running", State: State{RunID: id, NativeID: id, Claude: ClaudeState{Turn: "child-turn"}, Turns: map[string]string{"child-turn": "started"}}}}
					owner = &s.Lanes[child].State
				}
				owner.Tools = map[string]*ToolCall{tool: {ID: tool, Name: "Bash", Turn: owner.Claude.Turn, Status: "pending", Category: "command", Label: "Command", Arguments: object{}}}
				seq := int64(0)
				notices := 0
				apply := func(m object) []threads.Frame {
					t.Helper()
					checkpoint, _ := json.Marshal(s)
					var err error
					s, err = DecodeState(checkpoint)
					if err != nil {
						t.Fatal(err)
					}
					m["session_id"] = id
					raw, _ := json.Marshal(m)
					seq++
					next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: seq, ReceivedAt: time.Now()}, "stdout", string(raw))
					if err != nil {
						t.Fatal(err)
					}
					s = next
					for _, f := range frames {
						if f.Kind == "debug/unknown" || DataError(f) || f.LaneID != lane {
							t.Fatalf("wrong mapping: %s lane=%s %s", f.Kind, f.LaneID, f.Data)
						}
						if f.Kind == "turn/completed" {
							t.Fatal("command killed the turn")
						}
						if f.Kind == "tool/notification" {
							notices++
						}
					}
					return frames
				}
				call := func() *ToolCall {
					if lane != "" {
						return s.Lanes[child].State.Tools[tool]
					}
					return s.Tools[tool]
				}
				apply(object{"type": "system", "subtype": "task_started", "task_id": task, "tool_use_id": tool, "task_type": "local_bash", "is_backgrounded": false})
				promote := object{"type": "system", "subtype": "task_updated", "task_id": task, "patch": object{"is_backgrounded": true}}
				receipt := object{"type": "user", "uuid": "receipt", "message": object{"role": "user", "content": []any{object{"type": "tool_result", "tool_use_id": tool, "content": "Command moved to the background", "is_error": false}}}}
				if lane != "" {
					receipt["parent_tool_use_id"] = parent
				}
				if receiptFirst {
					apply(receipt)
					apply(promote)
				} else {
					apply(promote)
					apply(receipt)
				}
				if call().Status != "background" || call().Completed != nil || call().Output == nil {
					t.Fatalf("launch receipt completed background work: %+v", call())
				}
				apply(promote)
				if lane != "" {
					s.Lanes[child].Status = "completed"
					s.Lanes[child].State.Claude.Turn = ""
				} else {
					s.Claude.Turn = "later"
				}
				kill := object{"type": "system", "subtype": "task_updated", "task_id": task, "patch": object{"status": "killed", "end_time": int64(1788973589840)}}
				apply(kill)
				when, _ := call().Completed.(time.Time)
				if call().Status != "interrupted" || !when.Equal(time.UnixMilli(1788973589840)) {
					t.Fatalf("kill lost outcome/time: %+v", call())
				}
				apply(promote) // Late/duplicate promotion must not resurrect it.
				apply(kill)
				notice := object{"type": "system", "subtype": "task_notification", "task_id": task, "tool_use_id": tool, "status": "stopped", "summary": "Command stopped"}
				apply(notice)
				apply(notice)
				if notices != 1 || call().Status != "interrupted" || s.Turns["main"] != "started" {
					t.Fatalf("duplicate notice or wrong state: %d %+v", notices, s)
				}
			})
		}
	}
}

func TestClaudePromotionRejectsUnreviewedPatches(t *testing.T) {
	s := State{Tools: map[string]*ToolCall{"t": {ID: "t", Name: "Bash", ForegroundTaskID: "task", Status: "running"}}}
	for _, patch := range []object{{"is_backgrounded": false}, {"is_backgrounded": "true"}, {"is_backgrounded": true, "unexpected": 1}, {"status": "killed", "unexpected": 1}, {"status": "invented"}} {
		if s.claudeBackground(threads.ProviderFrame{}, object{"type": "system", "subtype": "task_updated", "task_id": "task", "patch": patch}, func(string, object) { t.Fatal("unexpected output") }) {
			t.Fatal("accepted", patch)
		}
	}
	if s.Tools["t"].BackgroundID != "" || s.Tools["t"].Status != "running" {
		t.Fatal("rejected patch mutated task")
	}
}

func TestClaudeTaskOnlyUpdateRequiresCurrentIdentity(t *testing.T) {
	const id = "eb60110d-e1ad-468f-a874-7d1c1635de90"
	for _, run := range []string{id, "previous-run"} {
		for _, task := range []string{"known", "unknown"} {
			if run == id && task == "known" {
				continue
			}
			s := State{RunID: id, NativeID: id, Lanes: map[string]*Lane{"child": {ID: "child", Native: id, Tool: "parent", State: State{RunID: run, NativeID: id, Tools: map[string]*ToolCall{"tool": {ID: "tool", Name: "Bash", ForegroundTaskID: "known", Status: "running"}}}}}}
			raw, _ := json.Marshal(object{"type": "system", "subtype": "task_updated", "task_id": task, "patch": object{"is_backgrounded": true}, "session_id": id})
			next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}, "stdout", string(raw))
			if err != nil || len(frames) != 1 || frames[0].Kind != "debug/unknown" || next.Lanes["child"].State.Tools["tool"].BackgroundID != "" {
				t.Fatalf("uncorrelated task accepted: %s %s %v %+v", run, task, err, frames)
			}
		}
	}
}
