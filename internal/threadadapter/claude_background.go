package threadadapter

import (
	"acta2/internal/threads"
	"encoding/xml"
	"strings"
)

// Prior runs retain only enough identity to render a repeated notice. They are
// deliberately not restored into Tools: a resume must not recreate old tool cards
// or mark an old process as currently running.
type BackgroundRecord struct {
	Tool     string
	Turn     string
	Label    string
	Notified bool
	Echoed   bool
}

func (s *State) backgroundHistory() map[string]*BackgroundRecord {
	history := s.BackgroundHistory
	if history == nil {
		history = map[string]*BackgroundRecord{}
	}
	for _, t := range s.Tools {
		if t.Name == "Bash" && t.BackgroundID != "" {
			history[t.BackgroundID] = &BackgroundRecord{Tool: t.ID, Turn: t.Turn, Label: t.Label}
		}
	}
	for _, r := range history {
		r.Notified = false
		r.Echoed = false
	}
	return history
}
func (s *State) resumedBackgroundNotice(id, tool, status, summary string, echo bool, emit func(string, object)) bool {
	r := s.BackgroundHistory[id]
	if r == nil || r.Tool != tool || status == "" || summary == "" {
		return false
	}
	if r.Echoed || r.Notified && !echo {
		return true
	}
	r.Notified = true
	r.Echoed = echo
	emit("tool/notification", object{"task_id": id, "tool_id": r.Tool, "turn_id": r.Turn, "label": r.Label, "status": status, "summary": summary, "context_entry": echo})
	return true
}

// A background command outlives its launching turn. Its provider task identity
// belongs to the tool checkpoint, not the current message/turn cursor.
func (s *State) backgroundTool(id string) *ToolCall {
	if id == "" {
		return nil
	}
	for _, t := range s.Tools {
		if t.BackgroundID == id {
			return t
		}
	}
	return nil
}

// Task-only updates omit both tool_use_id and parent_tool_use_id. Retain the
// foreground identity so a timeout can promote the same command in its lane.
func (s *State) foregroundTool(id string) *ToolCall {
	if id == "" {
		return nil
	}
	for _, t := range s.Tools {
		if t.Name == "Bash" && t.ForegroundTaskID == id {
			return t
		}
	}
	return nil
}
func (s *State) startBackground(t *ToolCall, id string, p threads.ProviderFrame) bool {
	if id == "" || (t.BackgroundID != "" && t.BackgroundID != id) || (t.ForegroundTaskID != "" && t.ForegroundTaskID != id) {
		return false
	}
	for _, other := range s.Tools {
		if (other.BackgroundID == id || other.ForegroundTaskID == id) && other.ID != t.ID {
			return false
		}
	}
	if t.BackgroundID == id {
		return true
	}
	t.BackgroundID = id
	t.Status = "background"
	t.Completed = nil
	if t.Started == nil {
		t.Started = p.ReceivedAt
	}
	return true
}
func backgroundOutcome(status string) string {
	switch status {
	case "completed":
		return "completed"
	case "failed":
		return "failed"
	case "stopped", "killed":
		return "interrupted"
	}
	return ""
}
func finishBackground(t *ToolCall, status string, when any) {
	if terminalTool(t.Status) {
		return
	}
	t.Status = status
	t.Completed = when
}
func (s *State) claudeBackground(p threads.ProviderFrame, m object, emit func(string, object)) bool {
	if m["parent_tool_use_id"] != nil {
		return false
	}
	if m["type"] == "system" {
		id := str(m["task_id"])
		switch m["subtype"] {
		case "background_tasks_changed":
			tasks, ok := m["tasks"].([]any)
			if !ok {
				return false
			}
			// Inventory has no tool identity. Lifecycle frames establish the mapping.
			for _, v := range tasks {
				t := obj(v)
				if str(t["task_id"]) == "" || t["task_type"] != "local_bash" {
					return false
				}
			}
			return true
		case "task_started":
			t := s.tool(str(m["tool_use_id"]))
			background, known := m["is_backgrounded"].(bool)
			if t == nil || t.Name != "Bash" || m["task_type"] != "local_bash" || !known || id == "" {
				return false
			}
			if !background {
				if t.ForegroundTaskID != "" && t.ForegroundTaskID != id || t.BackgroundID != "" && t.BackgroundID != id {
					return false
				}
				for _, other := range s.Tools {
					if (other.ForegroundTaskID == id || other.BackgroundID == id) && other.ID != t.ID {
						return false
					}
				}
				// Foreground lifecycle notices accompany the ordinary tool result.
				// Track identity without changing status or creating a background card.
				t.ForegroundTaskID = id
				if t.Started == nil {
					t.Started = p.ReceivedAt
				}
				return true
			}
			if !s.startBackground(t, id, p) {
				return false
			}
			emitTool(t, emit)
			return true
		case "task_updated":
			t := s.backgroundTool(id)
			patch := obj(m["patch"])
			if t == nil {
				t = s.foregroundTool(id)
			}
			if t == nil {
				return false
			}
			if len(patch) == 1 && patch["is_backgrounded"] == true {
				if !s.startBackground(t, id, p) {
					return false
				}
				emitTool(t, emit)
				return true
			}
			status := backgroundOutcome(str(patch["status"]))
			if status == "" {
				return false
			}
			for key := range patch {
				if key != "status" && key != "end_time" {
					return false
				}
			}
			if t.BackgroundID == "" {
				// A foreground task's ordinary tool result remains authoritative.
				return true
			}
			when := stamp(patch["end_time"], true)
			if when == nil {
				when = p.ReceivedAt
			}
			finishBackground(t, status, when)
			emitTool(t, emit)
			return true
		case "task_notification":
			if t := s.tool(str(m["tool_use_id"])); t != nil && t.Name == "Bash" && id != "" && t.ForegroundTaskID == id && t.BackgroundID == "" {
				// Do not mark terminal here: it can precede the full tool result,
				// which carries stdout, stderr and the authoritative failure state.
				return backgroundOutcome(str(m["status"])) != ""
			}
			t := s.backgroundTool(id)
			status := backgroundOutcome(str(m["status"]))
			if t == nil {
				return s.resumedBackgroundNotice(id, str(m["tool_use_id"]), status, str(m["summary"]), false, emit)
			}
			if t.ID != str(m["tool_use_id"]) || status == "" {
				return false
			}
			finishBackground(t, status, p.ReceivedAt)
			if summary := str(m["summary"]); summary != "" {
				t.Output = &summary
			}
			emitTool(t, emit)
			if !t.BackgroundNotified {
				t.BackgroundNotified = true
				emit("tool/notification", object{"task_id": id, "tool_id": t.ID, "turn_id": t.Turn, "label": t.Label, "status": status, "summary": str(m["summary"]), "context_entry": false})
			}
			return true
		}
	}
	if m["type"] != "user" || m["isReplay"] != true || obj(m["message"])["role"] != "user" {
		return false
	}
	text, ok := obj(m["message"])["content"].(string)
	if !ok || !strings.HasPrefix(text, "<task-notification>") {
		return false
	}
	var n struct {
		XMLName xml.Name   `xml:"task-notification"`
		Task    string     `xml:"task-id"`
		Tool    string     `xml:"tool-use-id"`
		Status  string     `xml:"status"`
		Summary string     `xml:"summary"`
		Output  string     `xml:"output-file"`
		Other   []struct{} `xml:",any"`
	}
	if xml.Unmarshal([]byte(text), &n) != nil || len(n.Other) != 0 {
		return false
	}
	t := s.backgroundTool(n.Task)
	status := backgroundOutcome(n.Status)
	if t == nil {
		return s.resumedBackgroundNotice(n.Task, n.Tool, status, n.Summary, true, emit)
	}
	if t.ID != n.Tool || status == "" || n.Summary == "" {
		return false
	}
	if t.BackgroundEchoed {
		return true
	}
	finishBackground(t, status, p.ReceivedAt)
	t.BackgroundNotified = true
	t.BackgroundEchoed = true
	emitTool(t, emit)
	emit("tool/notification", object{"task_id": n.Task, "tool_id": t.ID, "turn_id": t.Turn, "label": t.Label, "status": status, "summary": n.Summary, "context_entry": true})
	return true
}
