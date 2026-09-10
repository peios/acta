package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"encoding/xml"
	"github.com/google/uuid"
	"strings"
	"time"
)

// RequestLane checks attribution against provider-discovered children before
// routing an interaction. It never accepts a browser-supplied native thread ID.
func (s *State) RequestLane(provider, run string, raw json.RawMessage) (string, string, bool) {
	var m object
	if json.Unmarshal(raw, &m) != nil {
		return "", "", false
	}
	if provider == "codex" {
		native := str(obj(m["params"])["threadId"])
		if native == "" {
			native = s.NativeID
		}
		lane, known := s.nativeLane(native)
		if lane != "" && s.Lanes[lane].State.RunID != run {
			return "", "", false
		}
		return native, lane, known
	}
	id := str(obj(m["request"])["agent_id"])
	if id == "" {
		if l := s.laneForTool(str(m["parent_tool_use_id"])); l != nil {
			id = l.ID
		}
	}
	if id == "" {
		id = s.InteractionLanes[resolvedApproval(provider, run, s.NativeID, m)]
	}
	if id != "" {
		l := s.Lanes[id]
		if l == nil || l.State.RunID != run {
			return "", "", false
		}
		return s.NativeID, id, true
	}
	return s.NativeID, "", true
}

// Lane owns provider parsing state; a child cannot change its parent's turn,
// streamed block indexes, tools or configuration. The checkpoint survives reconnect.
type Lane struct {
	StartEvent   string
	ActivityID   string
	MetadataRead bool
	ID           string
	Parent       string
	Native       string
	Tool         string
	Turn         string
	Name         string
	Prompt       string
	Result       string
	Status       string
	Started      any
	Completed    any
	State        State
}

func (s *State) lane(id, native, parent string, p threads.ProviderFrame) *Lane {
	if s.Lanes == nil {
		s.Lanes = map[string]*Lane{}
	}
	l := s.Lanes[id]
	if l == nil {
		l = &Lane{ID: id, Native: native, Parent: parent, Status: "running", Started: p.ReceivedAt}
		s.Lanes[id] = l
	}
	if l.State.RunID != p.RunID {
		l.MetadataRead = false
		l.State = State{RunID: p.RunID, NativeID: native, Turns: map[string]string{}, Messages: map[string]Message{}}
	}
	return l
}

// Native task usage can be cumulative across background wake-ups. A turn's
// duration belongs to this activity, using the same clock as its start frame.
func (l *Lane) duration(end time.Time) any {
	start, ok := l.Started.(time.Time)
	if !ok {
		start, ok = iso(l.Started).(time.Time)
	}
	if !ok {
		return nil
	}
	return max(int64(0), end.Sub(start).Milliseconds())
}
func (l *Lane) data() object {
	return object{"lane_id": l.ID, "parent_lane_id": l.Parent, "tool_id": nullable(l.Tool), "turn_id": nullable(l.Turn), "name": l.Name, "status": l.Status, "prompt": nullable(l.Prompt), "result": nullable(l.Result), "started_at": l.Started, "completed_at": l.Completed, "can_send": false, "can_interrupt": true}
}
func (s *State) laneForTool(tool string) *Lane {
	if tool == "" {
		return nil
	}
	for _, l := range s.Lanes {
		if l.Tool == tool {
			return l
		}
	}
	return nil
}
func (s *State) nativeLane(native string) (string, bool) {
	if native == s.NativeID {
		return "", true
	}
	if l := s.Lanes[native]; l != nil {
		return l.ID, true
	}
	return "", false
}
func (s *State) convertLanes(p threads.ProviderFrame, m object, laneID *string, emit func(string, object)) (bool, string, string) {
	handled := func() (bool, string, string) { return true, "debug/local", "Handled provider subagent lane." }
	emitLane := func(l *Lane) {
		saved := *laneID
		*laneID = l.Parent
		emit("subagent/status", l.data())
		*laneID = saved
	}
	notify := func(l *Lane, echo bool) {
		saved := *laneID
		*laneID = l.Parent
		d := l.data()
		d["context_entry"] = echo
		d["completion_id"] = nullable(l.ActivityID)
		emit("subagent/notification", d)
		*laneID = saved
	}
	if p.Provider == "claude" {
		if session := str(m["session_id"]); session != "" && session != s.NativeID {
			return false, "", ""
		}
		if m["type"] == "tool_progress" && m["heartbeat"] == true {
			// Heartbeats use parent_tool_use_id for the actual command, not
			// the subagent. Resolve that command before ordinary lane routing.
			owner := s
			lane := ""
			id := str(m["parent_tool_use_id"])
			for _, l := range s.Lanes {
				if l.State.RunID == p.RunID && l.State.Tools[id] != nil {
					if owner.Tools[id] != nil {
						return false, "", ""
					}
					owner = &l.State
					lane = l.ID
				}
			}
			if owner.claudeHeartbeat(p, m, func(kind string, d object) { *laneID = lane; emit(kind, d) }) {
				*laneID = lane
				return true, "debug/local", "Handled tool execution heartbeat."
			}
			return true, "debug/unknown", "Unrecognized tool heartbeat identity or shape."
		}
		tool := str(m["parent_tool_use_id"])
		// Native task lifecycle events can omit parent_tool_use_id. Attribute a
		// child's command through its already observed tool/task identity.
		if tool == "" && m["type"] == "system" {
			for _, candidate := range s.Lanes {
				if candidate.State.RunID != p.RunID {
					continue
				}
				direct := str(m["tool_use_id"])
				task := str(m["task_id"])
				call := candidate.State.Tools[direct]
				foreground := task != "" && call != nil && call.ForegroundTaskID == task
				taskOnly := m["subtype"] == "task_updated" && direct == "" && candidate.State.foregroundTool(task) != nil
				if (m["task_type"] == "local_bash" && direct != "" && call != nil) || foreground || taskOnly || candidate.State.backgroundTool(task) != nil {
					tool = candidate.Tool
					break
				}
			}
		}
		if l := s.Lanes[str(obj(m["request"])["agent_id"])]; l != nil && m["type"] == "control_request" {
			tool = l.Tool
			l.Status = "waiting"
			emitLane(l)
			if s.InteractionLanes == nil {
				s.InteractionLanes = map[string]string{}
			}
			s.InteractionLanes[approvalID(p.RunID, m["request_id"])] = l.ID
		}
		if resolved := resolvedApproval("claude", p.RunID, s.NativeID, m); resolved != "" {
			if l := s.Lanes[s.InteractionLanes[resolved]]; l != nil {
				l.Status = "running"
				emitLane(l)
				tool = l.Tool
			}
		}

		parent := ""
		if l := s.laneForTool(tool); l != nil {
			parent = l.ID
		}
		subtype := str(m["subtype"])
		if m["type"] == "system" && subtype == "task_started" && m["task_type"] == "local_agent" {
			id := str(m["task_id"])
			t := str(m["tool_use_id"])
			previous := s.Lanes[id]
			continuation := t == "" && previous != nil && previous.State.RunID == p.RunID
			if continuation {
				if str(m["uuid"]) == "" {
					return false, "", ""
				}
				t = previous.Tool
				parent = previous.Parent
			}
			if id == "" || t == "" || tool != "" && parent == "" {
				return false, "", ""
			}
			for _, candidate := range s.Lanes {
				if candidate.State.Tools[t] != nil {
					parent = candidate.ID
					break
				}
			}
			if m["owned_by_subagent"] == true && parent == "" {
				return false, "", ""
			}
			l := s.lane(id, s.NativeID, parent, p)
			event := str(m["uuid"])
			if event != "" && l.StartEvent == event {
				return handled()
			}
			l.StartEvent = event
			l.Tool = t
			l.Name = str(m["description"])
			if !continuation {
				l.Prompt = str(m["prompt"])
			}
			owner := s
			if parent != "" {
				owner = &s.Lanes[parent].State
			}
			l.Turn = owner.Claude.Turn
			l.Status = "running"
			l.Completed = nil
			if continuation {
				l.Started = p.ReceivedAt
			}
			emitLane(l)
			*laneID = l.ID
			if l.State.Claude.Turn == "" || continuation {
				turn := id + "/" + t
				if continuation {
					turn = id + "/wake/" + event
					l.Started = p.ReceivedAt
				}
				if l.State.Turns == nil {
					l.State.Turns = map[string]string{}
				}
				l.State.Claude.Turn = turn
				l.ActivityID = turn
				l.State.Turns[turn] = "started"
				emit("thread/status", object{"status": "active", "waiting_for": []string{}})
				emit("turn/started", object{"turn_id": turn, "started_at": p.ReceivedAt})
				if !continuation && l.Prompt != "" {
					emit("message/user", object{"message_id": turn + "/prompt", "turn_id": turn, "state": "completed", "started_at": p.ReceivedAt, "completed_at": p.ReceivedAt, "content": []any{object{"type": "text", "text": l.Prompt}}})
				}
			}
			return handled()
		}
		if m["type"] == "system" && s.Lanes[str(m["task_id"])] != nil && (subtype == "task_updated" || subtype == "task_notification" || subtype == "task_progress") {
			l := s.Lanes[str(m["task_id"])]
			patch := obj(m["patch"])
			status := str(patch["status"])
			if subtype == "task_notification" {
				status = str(m["status"])
				l.Result = str(m["summary"])
			}
			switch status {
			case "running", "completed", "failed":
				l.Status = status
			case "stopped", "killed":
				l.Status = "interrupted"
			case "":
			default:
				return false, "", ""
			}
			if (l.Status == "completed" || l.Status == "failed" || l.Status == "interrupted") && l.Completed == nil {
				l.Completed = p.ReceivedAt
			}
			emitLane(l)
			if subtype == "task_notification" {
				notify(l, false)
				*laneID = l.ID
				emit("thread/status", object{"status": "idle", "waiting_for": []string{}})
				if turn := l.State.Claude.Turn; turn != "" {
					if l.State.Turns == nil {
						l.State.Turns = map[string]string{}
					}
					l.State.Turns[turn] = "completed"
					outcome := l.Status
					emit("turn/completed", object{"turn_id": turn, "outcome": outcome, "error": nil, "started_at": l.Started, "completed_at": l.Completed, "duration_ms": l.duration(p.ReceivedAt)})
				}
			}
			return handled()
		}
		if m["type"] == "system" && subtype == "background_tasks_changed" {
			tasks, ok := m["tasks"].([]any)
			if !ok {
				return false, "", ""
			}
			for _, v := range tasks {
				t := obj(v)
				if t["task_type"] != "local_bash" && (t["task_type"] != "local_agent" || str(t["task_id"]) == "") {
					return false, "", ""
				}
			}
			return handled()
		}
		if m["type"] == "user" && m["isReplay"] == true {
			if text, ok := obj(m["message"])["content"].(string); ok && strings.HasPrefix(text, "<task-notification>") {
				var n struct {
					Task    string `xml:"task-id"`
					Tool    string `xml:"tool-use-id"`
					Summary string `xml:"summary"`
				}
				if xml.Unmarshal([]byte(text), &n) == nil {
					if l := s.Lanes[n.Task]; l != nil && l.Tool == n.Tool {
						l.Result = n.Summary
						notify(l, true)
						return handled()
					}
				}
			}
		}
		if tool != "" {
			l := s.laneForTool(tool)
			if l == nil {
				return false, "", ""
			}
			l = s.lane(l.ID, l.Native, l.Parent, p)
			*laneID = l.ID
			clean := object{}
			for k, v := range m {
				clean[k] = v
			}
			clean["parent_tool_use_id"] = nil
			if m["type"] == "user" && str(m["uuid"]) != "" {
				parts := list(obj(m["message"])["content"])
				if len(parts) == 1 && obj(parts[0])["type"] == "text" && obj(parts[0])["text"] == l.Prompt {
					return handled()
				}
				if len(parts) == 1 && obj(parts[0])["type"] == "text" && m["isReplay"] != true {
					marker := str(obj(parts[0])["text"])
					if marker == "[Request interrupted by user]" || marker == "[Request interrupted by user for tool use]" {
						return true, "debug/dropped", "Provider interruption marker; the lane's turn completion supplies its summary."
					}
				}

				// The provider explicitly attributes this prompt to the child, unlike a
				// parent-level local command receipt. No Acta submission is manufactured.
				clean["isReplay"] = true
				l.State.Turns[str(m["uuid"])] = "started"
			}
			// Forwarding emits completed content blocks, without the parent's
			// content_block_start/delta stream. Attribute each block by UUID.
			if m["type"] == "assistant" {
				message := object{}
				for k, v := range obj(m["message"]) {
					message[k] = v
				}
				parts := []any{}
				for _, v := range list(message["content"]) {
					part := obj(v)
					if part["type"] == "thinking" {
						id := str(message["id"]) + "/thinking/" + str(m["uuid"])
						t := l.State.thinking(id, l.State.Claude.Turn, nil)
						t.Content["0"] = str(part["thinking"])
						t.Done = true
						t.Completed = p.ReceivedAt
						emitThinking(id, "thinking/completed", t, emit)
					} else {
						parts = append(parts, v)
					}
				}
				if len(parts) == 0 {
					return handled()
				}
				message["content"] = parts
				if len(parts) == 1 && obj(parts[0])["type"] == "text" {
					message["id"] = str(message["id"]) + "/block/" + str(m["uuid"])
				}
				clean["message"] = message
			}
			if model := str(obj(m["message"])["model"]); model != "" {
				l.State.claudeConfig(object{"model": model}, emit)
			}
			k, reason := l.State.convertClaude(p, clean, emit)
			if k != "debug/unknown" && m["type"] == "assistant" {
				l.State.claudeAssistant(str(obj(clean["message"])["id"]), true, emit)
			}
			return true, k, reason
		}
	} else if p.Provider == "codex" {
		if call := str(m["id"]); strings.HasPrefix(call, p.RunID+"/thread/read/lane/") {
			id := strings.TrimPrefix(call, p.RunID+"/thread/read/lane/")
			l := s.Lanes[id]
			if l == nil || l.State.RunID != p.RunID {
				return false, "", ""
			}
			if m["error"] != nil {
				l.MetadataRead = true
				return handled()
			}
			metadata := obj(obj(m["result"])["thread"])
			if metadata["id"] != l.Native {
				return false, "", ""
			}
			l.MetadataRead = true
			*laneID = l.ID
			l.State.config(metadata, emit)
			return handled()
		}
		params := obj(m["params"])
		native := str(params["threadId"])
		parent, known := s.nativeLane(native)
		item := obj(params["item"])
		method := str(m["method"])
		if known && (method == "item/started" || method == "item/completed") && item["type"] == "subAgentActivity" {
			id := str(item["agentThreadId"])
			if id == "" || id == s.NativeID {
				return false, "", ""
			}
			// A follow-up receipt is not a turn lifecycle event. The child emits
			// its own turn/started and turn/completed notifications separately.
			if item["kind"] == "interacted" && s.Lanes[id] != nil {
				return handled()
			}
			status := map[string]string{"started": "running", "completed": "completed", "interrupted": "interrupted"}[str(item["kind"])]
			if status == "" {
				return false, "", ""
			}
			l := s.lane(id, id, parent, p)
			if l.Tool == "" {
				l.Tool = str(item["id"])
				l.Turn = str(params["turnId"])
			}
			l.Name = strings.TrimPrefix(str(item["agentPath"]), "/root/")
			if l.Status == "completed" && status == "interrupted" {
				return handled()
			}
			if l.Status != status {
				l.Status = status
				if status != "running" {
					l.Completed = p.ReceivedAt
				} else {
					l.Completed = nil
				}
			}
			emitLane(l)
			if status == "completed" || status == "failed" {
				notify(l, false)
			}
			return handled()
		}
		if known && item["type"] == "collabAgentToolCall" && (method == "item/started" || method == "item/completed") {
			// Coordination calls have no independent transcript content. Reviewed state
			// snapshots update the existing child cards; unknown operations stay visible.
			switch str(item["tool"]) {
			case "spawnAgent", "wait", "closeAgent", "sendInput", "resumeAgent", "sendMessage", "followupTask", "interruptAgent", "listAgents":
			default:
				return false, "", ""
			}
			for id, v := range obj(item["agentsStates"]) {
				st := obj(v)
				l := s.Lanes[id]
				if l == nil {
					l = s.lane(id, id, parent, p)
					l.Tool = str(item["id"])
					l.Turn = str(params["turnId"])
					l.Name = id
					l.Prompt = str(item["prompt"])
				}
				status := map[string]string{"pendingInit": "running", "running": "running", "completed": "completed", "errored": "failed", "shutdown": "interrupted", "interrupted": "interrupted", "notFound": "unavailable"}[str(st["status"])]
				if status == "" {
					return false, "", ""
				}
				l.Status = status
				l.Result = str(st["message"])
				if status != "running" {
					l.Completed = p.ReceivedAt
				}
				emitLane(l)
			}
			return handled()
		}
		if known && parent != "" {
			l := s.Lanes[parent]
			l = s.lane(l.ID, l.Native, l.Parent, p)
			*laneID = l.ID
			if method == "item/completed" && item["type"] == "agentMessage" && item["phase"] == "final_answer" {
				l.Result = str(item["text"])
			}
			if method == "turn/started" {
				l.ActivityID = str(obj(params["turn"])["id"])
				l.Status = "running"
				l.Started = p.ReceivedAt
				l.Completed = nil
				emitLane(l)
			}
			k, reason := l.State.convertCodex(p, m, emit)
			if k != "debug/unknown" {
				if method == "turn/completed" {
					turn := obj(params["turn"])
					l.ActivityID = str(turn["id"])
					outcome := map[string]string{"completed": "completed", "interrupted": "interrupted", "failed": "failed"}[str(turn["status"])]
					if outcome != "" {
						l.Status = outcome
						l.Completed = p.ReceivedAt
						emitLane(l)
						notify(l, false)
					}
				}
				encoded, _ := json.Marshal(m)
				if parsed := ParseApproval("codex", p.RunID, l.Native, encoded); parsed != nil {
					l.Status = "waiting"
					emitLane(l)
				}
				if resolvedApproval("codex", p.RunID, l.Native, m) != "" && l.Status == "waiting" {
					l.Status = "running"
					emitLane(l)
				}
			}
			return true, k, reason
		}
		// Codex emits the new child's initial idle notification immediately before
		// its attributed creation event. It has no user-visible information yet.
		if _, err := uuid.Parse(native); err == nil && !known && native != "" && method == "thread/status/changed" && obj(params["status"])["type"] == "idle" {
			return true, "debug/local", "Idle notification before subagent attribution."
		}
	}
	return false, "", ""
}
