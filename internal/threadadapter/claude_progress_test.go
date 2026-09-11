package threadadapter

import (
	"acta/internal/threads"
	"encoding/json"
	"testing"
	"time"
)

func TestClaudeHeartbeatUpdatesOriginalTool(t *testing.T) {
	const id = "eb60110d-e1ad-468f-a874-7d1c1635de90"
	const tool = "toolu_018MEBCwa5s6q2PgRcWMMo1j"
	for _, lane := range []string{"", "child"} {
		t.Run(lane, func(t *testing.T) {
			s := State{RunID: id, NativeID: id}
			owner := &s
			if lane != "" {
				s.Lanes = map[string]*Lane{lane: {ID: lane, Tool: "agent-tool", State: State{RunID: id, NativeID: id}}}
				owner = &s.Lanes[lane].State
			}
			owner.Tools = map[string]*ToolCall{tool: {ID: tool, Turn: "turn", Name: "Bash", Category: "command", Label: "Wait for build", Status: "pending", Arguments: object{}}}
			raw := `{"type":"tool_progress","tool_use_id":"` + tool + `-heartbeat-0","tool_name":"Bash","parent_tool_use_id":"` + tool + `","elapsed_time_seconds":30,"heartbeat":true,"session_id":"` + id + `"}`
			now := time.Now().UTC()
			for seq := int64(1); seq <= 3; seq++ {
				checkpoint, _ := json.Marshal(s)
				var err error
				s, err = DecodeState(checkpoint)
				if err != nil {
					t.Fatal(err)
				}
				next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: seq, ReceivedAt: now}, "stdout", raw)
				if err != nil {
					t.Fatal(err)
				}
				s = next
				for _, f := range frames {
					if f.Kind == "debug/unknown" || DataError(f) || f.LaneID != lane {
						t.Fatalf("bad heartbeat: %+v %s", f, f.Data)
					}
				}
				call := s.Tools[tool]
				if lane != "" {
					call = s.Lanes[lane].State.Tools[tool]
				}
				if seq == 1 {
					if len(frames) != 2 || frames[1].Kind != "tool/call" || call.Status != "running" || !call.Started.(time.Time).Equal(now.Add(-30*time.Second)) {
						t.Fatalf("not updated: %+v", call)
					}
				} else if len(frames) != 1 {
					t.Fatal("duplicate emitted a tool card", frames)
				}
				if seq == 2 {
					call.Status = "completed"
				}
				if seq == 3 && call.Status != "completed" {
					t.Fatal("late heartbeat resurrected tool")
				}
			}
		})
	}
}

func TestClaudeHeartbeatRejectsInvalidIdentityAndElapsed(t *testing.T) {
	const id = "eb60110d-e1ad-468f-a874-7d1c1635de90"
	for _, change := range []object{{"parent_tool_use_id": "missing"}, {"tool_name": "Write"}, {"elapsed_time_seconds": -1}, {"elapsed_time_seconds": "30"}, {"elapsed_time_seconds": 1e30}, {"tool_use_id": "tool-heartbeat-no"}, {"heartbeat": false}, {"session_id": "wrong"}} {
		s := State{RunID: id, NativeID: id, Tools: map[string]*ToolCall{"tool": {ID: "tool", Name: "Bash", Status: "pending"}}}
		m := object{"type": "tool_progress", "tool_use_id": "tool-heartbeat-0", "parent_tool_use_id": "tool", "tool_name": "Bash", "elapsed_time_seconds": 30, "heartbeat": true, "session_id": id}
		for k, v := range change {
			m[k] = v
		}
		raw, _ := json.Marshal(m)
		next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}, "stdout", string(raw))
		if err != nil || len(frames) != 1 || frames[0].Kind != "debug/unknown" || next.Tools["tool"].Status != "pending" {
			t.Fatal("accepted", change, err, frames)
		}
	}
	// Retained lanes from previous provider runs cannot own a heartbeat.
	s := State{RunID: id, NativeID: id, Lanes: map[string]*Lane{"child": {ID: "child", Tool: "parent", State: State{RunID: "old", Tools: map[string]*ToolCall{"tool": {ID: "tool", Name: "Bash", Status: "pending"}}}}}}
	raw := `{"type":"tool_progress","tool_use_id":"tool-heartbeat-0","parent_tool_use_id":"tool","tool_name":"Bash","elapsed_time_seconds":30,"heartbeat":true}`
	_, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}, "stdout", raw)
	if err != nil || frames[0].Kind != "debug/unknown" {
		t.Fatal("accepted stale lane", err, frames)
	}
}

func TestClaudeGitPush(t *testing.T) {
	const id = "eb60110d-e1ad-468f-a874-7d1c1635de90"
	for _, kind := range []string{"push", "unknown"} {
		raw, _ := json.Marshal(object{"type": "system", "subtype": "vcs_state_changed", "kind": kind, "branch": "main", "cwd": "/home/jack/projects/peios/librsi", "session_id": id})
		_, frames, err := Map(State{RunID: id, NativeID: id, Claude: ClaudeState{Turn: "turn"}}, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}, "stdout", string(raw))
		if err != nil {
			t.Fatal(err)
		}
		if kind != "push" {
			if len(frames) != 1 || frames[0].Kind != "debug/unknown" {
				t.Fatal(frames)
			}
			continue
		}
		if len(frames) != 2 || frames[1].Kind != "vcs/push" || DataError(frames[0]) {
			t.Fatal(frames)
		}
		var d object
		json.Unmarshal(frames[1].Data, &d)
		if d["branch"] != "main" || d["cwd"] != "/home/jack/projects/peios/librsi" || d["turn_id"] != "turn" {
			t.Fatal(d)
		}
	}
}
